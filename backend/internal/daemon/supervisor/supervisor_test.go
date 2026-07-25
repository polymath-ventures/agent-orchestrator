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
// is not speaking the supervisor protocol. It must stay strictly LARGER than
// the production handshakeTimeout the server is waiting out, or the two race
// and the test flakes under load or -race for no real reason. The assertion is
// that the close *terminates*, not that it is fast.
const closeWindow = 4 * time.Second

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

// --- #147 follow-up: countdown accounting under concurrent arrivals ----------

// stormGrace is long enough that no countdown armed during the storm below can
// legitimately elapse while the storm is still running, so a fire that lands
// early is evidence of a leaked countdown rather than normal operation.
const stormGrace = 500 * time.Millisecond

// stormDuration is how long the arrive/depart storm runs. It is deliberately a
// fraction of stormGrace: a countdown leaked near the start of the storm becomes
// visible as a fire roughly stormGrace after the storm BEGAN, which is about
// stormDuration too early relative to the last departure.
const stormDuration = 250 * time.Millisecond

// stormWorkers is the number of goroutines cycling handshaked connections. The
// bug needs an arrival to acquire the supervisor's mutex in the gap a departure
// leaves between deciding "that was the last client" and installing the
// countdown. Go hands a contended mutex to a waiter on Unlock, so heavy
// contention makes that interleaving common instead of vanishingly rare.
const stormWorkers = 64

// TestConcurrentArriveDepartDoesNotShortenGrace pins countdown accounting under
// the concurrency that per-connection goroutines introduce.
//
// Many handshaked clients arrive and depart at once, then all of them go away.
// From that moment a correct supervisor waits a full grace period before firing,
// because the only legitimate countdown is the one armed by the final departure.
// A countdown armed while a client was still live, or an orphan a later install
// failed to stop, elapses sooner than that and cuts the grace short — which in
// production means the daemon self-stopping while a frontend is reconnecting.
func TestConcurrentArriveDepartDoesNotShortenGrace(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	fireCount := 0
	var fireAt time.Time
	fired := make(chan struct{}, 1)
	cb := func() {
		mu.Lock()
		fireCount++
		if fireCount == 1 {
			fireAt = time.Now()
		}
		mu.Unlock()
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	s := supervisor.New(stormGrace, cb, noopLogger())
	ln := newFakeListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	var wg sync.WaitGroup
	deadline := time.Now().Add(stormDuration)
	for i := 0; i < stormWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				serverConn, clientConn := makePipe()
				ln.enqueue(serverConn)
				// Deliberately not writeHandshake: that calls t.Fatalf, which is
				// illegal off the test goroutine. A write error here means the
				// server hung up on a valid token, which the assertions below
				// surface as a missing or mistimed fire.
				if _, err := clientConn.Write([]byte(supervisor.HandshakeToken)); err != nil {
					_ = clientConn.Close()
					return
				}
				_ = clientConn.Close()
			}
		}()
	}
	wg.Wait()

	// Every client is gone from here on. The server may still be observing the
	// last few EOFs, which can only push the legitimate fire later, never
	// earlier, so this is a safe lower bound to measure from.
	tEnd := time.Now()

	select {
	case <-fired:
	case <-time.After(stormGrace * 4):
		t.Fatal("callback never fired after every handshaked client went away")
	}

	mu.Lock()
	gotAt := fireAt
	mu.Unlock()

	// 80% of grace absorbs scheduling noise while staying well above what a
	// leaked countdown produces (roughly stormGrace - stormDuration, i.e. ~50%).
	elapsed := gotAt.Sub(tEnd)
	wantMin := stormGrace * 4 / 5
	if elapsed < wantMin {
		t.Fatalf("callback fired %s after the last client left, want at least %s (grace %s): a countdown was armed while a client was still live, or an orphaned countdown fired",
			elapsed, wantMin, stormGrace)
	}

	// A leaked countdown can also surface as a second fire; fireOnce must hold.
	time.Sleep(stormGrace)
	mu.Lock()
	count := fireCount
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly one callback fire, got %d", count)
	}

	cancel()
	_ = ln.Close()
	<-done
}

// timeoutErr is a net.Error that reports itself as a timeout, so a test can
// make a Read expire the way an un-cleared deadline would.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// deadlineFlakeConn is a handshaked client whose SetReadDeadline fails exactly
// once — on the clear that follows a successful handshake — and whose next Read
// then expires because the handshake deadline is still armed. It stays open the
// whole time: the client is alive and must keep counting as live.
type deadlineFlakeConn struct {
	net.Conn
	mu        sync.Mutex
	reads     int
	setCalls  int
	blockRead chan struct{}
}

func (c *deadlineFlakeConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setCalls++
	// 1: readHandshake arms it. 2: handleConn's clear — the failure under test.
	// 3+: the drain's retry, which must succeed so the client is kept.
	if c.setCalls == 2 {
		return errors.New("simulated SetReadDeadline failure")
	}
	return nil
}

func (c *deadlineFlakeConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	c.reads++
	n := c.reads
	c.mu.Unlock()

	switch n {
	case 1:
		return copy(b, supervisor.HandshakeToken), nil
	case 2:
		// The un-cleared handshake deadline expiring. NOT a disconnect.
		return 0, timeoutErr{}
	default:
		<-c.blockRead
		return 0, io.EOF
	}
}

func (c *deadlineFlakeConn) Close() error { return nil }

// TestDrainTimeoutIsNotADisconnect: if clearing the handshake deadline fails,
// the drain's read expires. Treating that timeout as EOF would report a live
// client as gone and self-stop the daemon after grace — #147's exact symptom,
// reintroduced through the one path meant to prevent it.
func TestDrainTimeoutIsNotADisconnect(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{}, 1)
	cb := func() {
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

	serverSide, clientSide := makePipe()
	defer func() { _ = clientSide.Close() }()
	conn := &deadlineFlakeConn{Conn: serverSide, blockRead: make(chan struct{})}
	ln.enqueue(conn)

	// Well past grace: a timeout misread as EOF would have fired by now.
	time.Sleep(comfortWait * 3)

	select {
	case <-fired:
		t.Fatal("callback fired for a live client whose read merely timed out; a timeout is not a disconnect")
	default:
	}

	// The client really leaving must still fire, so the fix cannot pass by
	// making the watchdog unreachable.
	close(conn.blockRead)
	select {
	case <-fired:
	case <-time.After(comfortWait * 4):
		t.Fatal("callback did not fire after the client actually disconnected")
	}

	cancel()
	_ = ln.Close()
	<-done
}
