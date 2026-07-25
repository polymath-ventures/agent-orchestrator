// Package supervisor provides a transport-agnostic watchdog that fires a
// callback when the last connected client disconnects and stays gone for a
// configurable grace period. It arms only after the FIRST client completes the
// handshake (see HandshakeToken) so a daemon started with no frontend (e.g. CLI
// "ao start") never self-stops, and so a transient probe that merely opens the
// socket cannot perturb the daemon's lifecycle.
//
// This package is a leaf: it imports only stdlib.
package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// HandshakeToken is the exact byte sequence a client must write immediately
// after connecting to be counted as a live supervisor client. It is the single
// source of truth for the token; the frontend link
// (frontend/src/main/supervisor-link.ts) writes this same string on connect.
//
// The connection alone is deliberately NOT the credential: without a proof of
// identity, any transient connection (a port scan, a stray curl, a mistaken
// probe) would arm the watchdog and its close would schedule a daemon shutdown.
const HandshakeToken = "ao-supervisor/1\n"

// handshakeTimeout bounds how long a freshly accepted connection has to produce
// HandshakeToken. A real client writes the token from its connect handler, so
// this only ever expires for clients that are not speaking the protocol at all.
// It is deliberately well under the daemon's supervisor grace (5s, see
// backend/internal/daemon/daemon.go) so a rejected client is hung up on
// promptly instead of lingering — the 8-hour leaked probe in #147.
const handshakeTimeout = 2 * time.Second

// acceptRetryBackoff bounds the retry after a transient Accept error so a
// persistent failure cannot hot-spin the accept loop.
const acceptRetryBackoff = 200 * time.Millisecond

// errHandshakeMismatch means the client wrote the right number of bytes but not
// the right bytes (e.g. an HTTP request line).
var errHandshakeMismatch = errors.New("supervisor: handshake token mismatch")

// Supervisor watches connections on a net.Listener and calls onLastClientGone
// exactly once (per process lifetime) when the live-count drops to zero and
// stays zero for the grace period.
//
// Concurrency model:
//   - mu guards liveCount, armed, and pendingTimer.
//   - armed flips to true on the first connection that completes the handshake
//     and never resets; it is the "headless-safety" gate that prevents a
//     pre-connect fire.
//   - pendingTimer holds the *time.Timer from time.AfterFunc so it can be
//     stopped on reconnect. A non-nil pendingTimer means a grace countdown is
//     running.
//   - fireOnce ensures onLastClientGone is called at most once for the entire
//     process lifetime, even if the timer fires concurrently with a reconnect.
type Supervisor struct {
	grace            time.Duration
	onLastClientGone func()
	log              *slog.Logger

	mu           sync.Mutex
	liveCount    int
	armed        bool        // true once a connection has completed the handshake
	pendingTimer *time.Timer // non-nil while grace countdown is running

	fireOnce sync.Once
}

// New creates a Supervisor. grace is the delay before the callback fires after
// the last connection closes. onLastClientGone is called at most once for the
// process lifetime, so it is safe to use it to trigger os.Exit or context
// cancellation.
func New(grace time.Duration, onLastClientGone func(), log *slog.Logger) *Supervisor {
	return &Supervisor{
		grace:            grace,
		onLastClientGone: onLastClientGone,
		log:              log,
	}
}

// Serve runs the accept loop on ln until ctx is cancelled or ln is closed.
// It returns nil on a clean shutdown (context cancelled or listener closed
// normally); it only returns a non-nil error for unexpected Accept failures.
func (s *Supervisor) Serve(ctx context.Context, ln net.Listener) error {
	// Derive a cancellable context so the watcher goroutine always unblocks
	// when Serve returns, even if ctx itself is not cancelled (e.g. listener
	// closed directly).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Close the listener when ctx is cancelled so Accept() unblocks.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// A closed listener or context cancellation is a clean stop.
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			// net.ErrClosed is what real listeners return when closed normally.
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			// A transient Accept error (e.g. EMFILE) must NOT silently kill the
			// watchdog: that would leave the daemon unable to self-stop on
			// frontend death. Back off briefly and keep accepting. A genuinely
			// closed listener returns net.ErrClosed (handled above) or trips
			// ctx.Done during the backoff.
			s.log.Warn("supervisor: accept error, retrying", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(acceptRetryBackoff):
			}
			continue
		}

		// Hand off immediately and touch no state here: the handshake read
		// happens in handleConn so a slow or hostile client can never block
		// Accept().
		go s.handleConn(conn)
	}
}

// handleConn gates one accepted connection on the handshake before it is
// allowed to affect lifecycle state.
//
// The handshake, the increment, and the drain that observes EOF are all in this
// one goroutine, in this order. That is what makes "only a handshaked
// connection may decrement" true by construction: there is a single code path
// past the handshake, and it owns both halves of the count. Moving the
// increment anywhere else (the accept loop, a helper goroutine) reintroduces
// the races #147 is about.
func (s *Supervisor) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	if err := readHandshake(conn); err != nil {
		// No state was touched, so there is nothing to undo: the connection was
		// never live, never armed the watchdog, and must not decrement.
		s.log.Debug("supervisor: rejected client that did not complete the handshake", "err", err)
		return
	}

	// The token is proven, so the connection counts from here on. Clearing the
	// deadline lets the drain below block indefinitely: a held connection must
	// never time out. A failure here means the connection is already broken (a
	// peer that closed right after handshaking — net.Pipe reports exactly this),
	// which the drain observes immediately, so it must not retroactively
	// un-prove a handshake that succeeded.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		s.log.Debug("supervisor: could not clear handshake read deadline", "err", err)
	}

	s.clientArrived()
	drainToEOF(conn)
	s.clientGone()
}

// readHandshake reads exactly len(HandshakeToken) bytes under a short deadline
// and verifies them. The fixed length bounds the read by construction: a client
// streaming garbage cannot grow a buffer here, and a client that sends nothing
// is cut off by the deadline rather than held forever.
func readHandshake(conn net.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}
	buf := make([]byte, len(HandshakeToken))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if string(buf) != HandshakeToken {
		return errHandshakeMismatch
	}
	return nil
}

// drainToEOF reads and discards from conn purely to detect close. When read
// returns io.EOF or any error, the connection is gone.
func drainToEOF(conn net.Conn) {
	// ponytail: 32-byte scratch buffer; we never process the payload.
	scratch := make([]byte, 32)
	for {
		if _, err := conn.Read(scratch); err != nil {
			return
		}
	}
}

// clientArrived records a handshaked client: it arms the watchdog, raises the
// live count, and cancels any grace countdown left by a previous disconnect.
func (s *Supervisor) clientArrived() {
	s.mu.Lock()
	s.armed = true
	s.liveCount++
	// If a grace timer was pending (reconnect before grace elapsed), cancel it.
	if s.pendingTimer != nil {
		s.pendingTimer.Stop()
		s.pendingTimer = nil
	}
	live := s.liveCount
	s.mu.Unlock()

	s.log.Debug("supervisor: client connected", "liveCount", live)
}

// clientGone records the departure of a handshaked client. Only handleConn
// calls it, and only after a matching clientArrived.
func (s *Supervisor) clientGone() {
	s.mu.Lock()
	s.liveCount--
	live := s.liveCount
	armed := s.armed
	s.mu.Unlock()

	s.log.Debug("supervisor: client disconnected", "liveCount", live)

	if armed && live == 0 {
		s.armGrace()
	}
}

// armGrace starts the grace countdown. If another client handshakes before it
// elapses, clientArrived will Stop() the timer via pendingTimer.
func (s *Supervisor) armGrace() {
	s.mu.Lock()
	s.pendingTimer = time.AfterFunc(s.grace, func() {
		s.mu.Lock()
		live := s.liveCount
		s.pendingTimer = nil
		s.mu.Unlock()

		if live == 0 {
			s.log.Info("supervisor: last client gone; grace elapsed, firing callback")
			s.fireOnce.Do(s.onLastClientGone)
		}
	})
	s.mu.Unlock()
}
