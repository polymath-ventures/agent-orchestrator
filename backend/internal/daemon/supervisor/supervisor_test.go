// Package supervisor_test exercises the Supervisor watchdog via in-process
// net.Pipe connections so no real OS sockets are needed.
package supervisor_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/daemon/supervisor"
)

// fakeListener queues pre-made conns and blocks (or returns a closed error)
// once the queue is drained. Close() unblocks any pending Accept().
type fakeListener struct {
	mu     sync.Mutex
	conns  []net.Conn
	closed bool
	ready  chan struct{} // closed when a conn is enqueued or the listener is closed
}

func newFakeListener() *fakeListener {
	return &fakeListener{ready: make(chan struct{}, 1)}
}

// enqueue adds a conn for the next Accept() call.
func (fl *fakeListener) enqueue(c net.Conn) {
	fl.mu.Lock()
	fl.conns = append(fl.conns, c)
	fl.mu.Unlock()
	select {
	case fl.ready <- struct{}{}:
	default:
	}
}

func (fl *fakeListener) Accept() (net.Conn, error) {
	for {
		fl.mu.Lock()
		if fl.closed {
			fl.mu.Unlock()
			return nil, net.ErrClosed // signals Serve to stop
		}
		if len(fl.conns) > 0 {
			c := fl.conns[0]
			fl.conns = fl.conns[1:]
			fl.mu.Unlock()
			return c, nil
		}
		fl.mu.Unlock()
		// drain the ready channel so we can block below
		select {
		case <-fl.ready:
		default:
		}
		// wait for a new conn or a close signal
		<-fl.ready
	}
}

func (fl *fakeListener) Close() error {
	fl.mu.Lock()
	fl.closed = true
	fl.mu.Unlock()
	select {
	case fl.ready <- struct{}{}:
	default:
	}
	return nil
}

func (fl *fakeListener) Addr() net.Addr { return &net.UnixAddr{Name: "fake", Net: "unix"} }

// noopLogger returns a slog.Logger that discards all output.
func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const testGrace = 30 * time.Millisecond

// comfortWait is how long we wait when asserting the callback did NOT fire.
// It must be strictly greater than testGrace so a real timer would have fired.
const comfortWait = testGrace * 5

// TestNeverFiresPreConnect: start Serve with no connections, wait well past
// grace, assert callback was NOT called.
func TestNeverFiresPreConnect(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{})
	cb := func() { close(fired) }

	s := supervisor.New(testGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	// wait comfortably past grace with no connections ever accepted
	time.Sleep(comfortWait)

	select {
	case <-fired:
		t.Fatal("callback fired before any client ever connected")
	default:
	}

	cancel()
	_ = ln.Close()
	<-done
}

// TestFiresOnceAfterGrace: connect one client, close it, assert the callback
// fires exactly once within a reasonable window.
func TestFiresOnceAfterGrace(t *testing.T) {
	t.Parallel()

	fireCount := 0
	var mu sync.Mutex
	fired := make(chan struct{})
	cb := func() {
		mu.Lock()
		fireCount++
		mu.Unlock()
		// close is safe even if called once, but use a Once-guarded close via
		// a sync.Once in the real impl; here we just close the channel once
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	s := supervisor.New(testGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	// create a pipe, enqueue the server-side end
	serverConn, clientConn := makePipe()
	ln.enqueue(serverConn)

	// close the client side to signal disconnect
	_ = clientConn.Close()

	// wait for the callback within a bounded window
	select {
	case <-fired:
		// good
	case <-time.After(comfortWait * 2):
		t.Fatal("callback did not fire after client disconnected and grace elapsed")
	}

	// close and wait a bit more to make sure it only fires once
	time.Sleep(comfortWait)
	mu.Lock()
	count := fireCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected callback to fire exactly once, got %d", count)
	}

	cancel()
	_ = ln.Close()
	<-done
}

// TestReconnectWithinGraceCancels: connect, disconnect (arms grace), reconnect
// before grace elapses, wait past grace, assert callback NOT called. Then
// disconnect again and assert it DOES fire.
func TestReconnectWithinGraceCancels(t *testing.T) {
	t.Parallel()

	fireCount := 0
	var mu sync.Mutex
	fired := make(chan struct{}, 1)
	cb := func() {
		mu.Lock()
		fireCount++
		mu.Unlock()
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	s := supervisor.New(testGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	// --- first connection ---
	serverConn1, clientConn1 := makePipe()
	ln.enqueue(serverConn1)
	// small sleep so the server-side accept loop picks up the first conn
	time.Sleep(5 * time.Millisecond)

	// disconnect first client: this arms grace
	_ = clientConn1.Close()

	// reconnect immediately (well within grace period) before grace elapses
	serverConn2, clientConn2 := makePipe()
	ln.enqueue(serverConn2)

	// wait well past grace: grace should have been cancelled by the reconnect
	time.Sleep(comfortWait)

	select {
	case <-fired:
		t.Fatal("callback fired even though a client reconnected before grace elapsed")
	default:
	}

	// now disconnect the second client: grace re-arms, callback should fire
	_ = clientConn2.Close()

	select {
	case <-fired:
		// good
	case <-time.After(comfortWait * 2):
		t.Fatal("callback did not fire after second client disconnected and grace elapsed")
	}

	mu.Lock()
	count := fireCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly one callback fire (process-lifetime once), got %d", count)
	}

	cancel()
	_ = ln.Close()
	<-done
}

// makePipe returns a server-side and client-side net.Conn pair via net.Pipe.
func makePipe() (net.Conn, net.Conn) {
	s, c := net.Pipe()
	return s, c
}

// --- #147: the connection alone must not be the credential -------------------

// httpProbe is what a bare `curl http://.../` puts on the wire: a realistic
// non-protocol client that never speaks the supervisor handshake.
const httpProbe = "GET / HTTP/1.1\r\nHost: local\r\n\r\n"

// closeWindow bounds how long the server may take to hang up on a client that
// is not speaking the supervisor protocol. It is deliberately generous: the
// assertion is that the close *terminates*, not that it is fast.
const closeWindow = 2 * time.Second

// reconnectGrace is used where a test has to complete a full accept +
// handshake round trip inside the grace period. testGrace (30ms) is too tight
// to do that reliably under `-race` on a loaded machine, and a flaky watchdog
// test is worse than a slightly slower one.
const reconnectGrace = 200 * time.Millisecond

// writeHandshake writes the supervisor handshake token from the client side of
// a net.Pipe. The write is done off the test goroutine because net.Pipe is
// unbuffered: it blocks until the server actually reads the token, which is
// exactly the behavior under test. A blocked write is a failure, not a hang.
func writeHandshake(t *testing.T, c net.Conn) {
	t.Helper()

	wrote := make(chan error, 1)
	go func() {
		_, err := c.Write([]byte(supervisor.HandshakeToken))
		wrote <- err
	}()

	select {
	case err := <-wrote:
		if err != nil {
			t.Fatalf("handshake write failed: %v", err)
		}
	case <-time.After(closeWindow):
		t.Fatalf("handshake write blocked for more than %s: server never read the token", closeWindow)
	}
}

// TestTransientNonHandshakeClientDoesNotArm is the restart-churn bug (#147 AC 1).
// A connection that is accepted and then closed WITHOUT ever writing the
// handshake token — a `curl --max-time 2`, a port scan, a mistaken probe —
// must not arm the watchdog, so its disconnect must not fire the callback.
func TestTransientNonHandshakeClientDoesNotArm(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{})
	cb := func() { close(fired) }

	s := supervisor.New(testGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	serverConn, clientConn := makePipe()
	ln.enqueue(serverConn)
	// Let the accept loop pick the conn up, so this is genuinely an accepted
	// connection that then went away rather than one that was never accepted.
	time.Sleep(5 * time.Millisecond)

	// Gone without ever having spoken the protocol.
	_ = clientConn.Close()

	// Wait comfortably past grace: a real timer would have fired by now.
	time.Sleep(comfortWait)

	select {
	case <-fired:
		t.Fatal("callback fired for a client that never completed the supervisor handshake")
	default:
	}

	cancel()
	_ = ln.Close()
	<-done
}

// TestNonProtocolClientClosedPromptly is the leaked-curl bug (#147 AC 2).
// A client that writes bytes which are not the handshake token must be closed
// by the server within a bounded window, instead of being drained forever
// (the 8-hour hung `curl` the reporter observed).
func TestNonProtocolClientClosedPromptly(t *testing.T) {
	t.Parallel()

	s := supervisor.New(testGrace, func() {}, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	serverConn, clientConn := makePipe()
	ln.enqueue(serverConn)
	// Unblocks the goroutine below if the server never closes, so a regression
	// fails this test instead of hanging the suite.
	defer func() { _ = clientConn.Close() }()

	// Both the write and the follow-up read can block indefinitely against a
	// server that drains and never hangs up, so neither runs on the test
	// goroutine.
	result := make(chan error, 1)
	go func() {
		if _, err := clientConn.Write([]byte(httpProbe)); err != nil {
			// The server hung up mid-write; that is a prompt close.
			result <- err
			return
		}
		buf := make([]byte, 64)
		_, err := clientConn.Read(buf)
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected the server to close a non-protocol connection; read returned data instead")
		}
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("expected io.EOF or a closed-connection error, got %v", err)
		}
	case <-time.After(closeWindow):
		t.Fatalf("server held a non-protocol connection open for more than %s instead of closing it", closeWindow)
	}

	cancel()
	_ = ln.Close()
	<-done
}

// TestHandshakedClientFiresAfterGrace guards against a fix that just disables
// the watchdog: a client that DOES write the handshake token counts as live,
// and its disconnect still fires the callback exactly once after grace.
func TestHandshakedClientFiresAfterGrace(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	fireCount := 0
	fired := make(chan struct{}, 1)
	cb := func() {
		mu.Lock()
		fireCount++
		mu.Unlock()
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	s := supervisor.New(testGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	serverConn, clientConn := makePipe()
	ln.enqueue(serverConn)
	writeHandshake(t, clientConn)

	_ = clientConn.Close()

	select {
	case <-fired:
		// good
	case <-time.After(comfortWait * 2):
		t.Fatal("callback did not fire after a handshaked client disconnected and grace elapsed")
	}

	// Give a duplicate fire time to show up.
	time.Sleep(comfortWait)
	mu.Lock()
	count := fireCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected callback to fire exactly once, got %d", count)
	}

	cancel()
	_ = ln.Close()
	<-done
}

// TestHandshakedReconnectWithinGraceCancels is TestReconnectWithinGraceCancels
// with both connections completing the handshake: a handshaked reconnect
// inside the grace period must still cancel the pending shutdown, and the
// second disconnect must still fire the callback exactly once.
func TestHandshakedReconnectWithinGraceCancels(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	fireCount := 0
	fired := make(chan struct{}, 1)
	cb := func() {
		mu.Lock()
		fireCount++
		mu.Unlock()
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	s := supervisor.New(reconnectGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	// --- first handshaked connection ---
	serverConn1, clientConn1 := makePipe()
	ln.enqueue(serverConn1)
	writeHandshake(t, clientConn1)

	// Disconnect: this arms grace.
	_ = clientConn1.Close()

	// --- handshaked reconnect, well inside grace ---
	serverConn2, clientConn2 := makePipe()
	ln.enqueue(serverConn2)
	writeHandshake(t, clientConn2)

	// Wait past grace: the reconnect should have cancelled the countdown.
	time.Sleep(reconnectGrace * 3)

	select {
	case <-fired:
		t.Fatal("callback fired even though a handshaked client reconnected before grace elapsed")
	default:
	}

	// Now drop the second client: grace re-arms and the callback should fire.
	_ = clientConn2.Close()

	select {
	case <-fired:
		// good
	case <-time.After(reconnectGrace * 6):
		t.Fatal("callback did not fire after the second handshaked client disconnected and grace elapsed")
	}

	mu.Lock()
	count := fireCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly one callback fire (process-lifetime once), got %d", count)
	}

	cancel()
	_ = ln.Close()
	<-done
}
