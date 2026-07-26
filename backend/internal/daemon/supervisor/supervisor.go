// Package supervisor provides a transport-agnostic watchdog that fires a
// callback when the last connected client disconnects and stays gone for a
// configurable grace period. It arms only after the FIRST client completes the
// handshake (see HandshakeToken) so a daemon started with no frontend (e.g. CLI
// "ao daemon") never self-stops, and so a transient probe that merely opens the
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
//   - mu guards liveCount, armed, pendingTimer, and graceGen.
//   - armed flips to true on the first connection that completes the handshake
//     and never resets; it is the "headless-safety" gate that prevents a
//     pre-connect fire.
//   - pendingTimer holds the *time.Timer from time.AfterFunc so it can be
//     stopped on reconnect. A non-nil pendingTimer means a grace countdown is
//     running.
//   - graceGen identifies the current countdown. Every install or cancel bumps
//     it, so a timer whose func had already started when it was stopped sees
//     that it is no longer current and declines to fire. Stop() alone cannot
//     provide this: it does not wait for a func that is already running.
//   - Connections are handled by one goroutine each, so arrivals and departures
//     contend on mu concurrently. Every transition of liveCount is therefore
//     paired with its countdown decision inside a single critical section; a
//     countdown must never be installed from a section that has already
//     released mu, because an arrival can interleave and the timer would then
//     be armed while a client is live.
//   - fireOnce ensures onLastClientGone is called at most once for the entire
//     process lifetime, even if the timer fires concurrently with a reconnect.
//   - onLastClientGone is never invoked while holding mu.
type Supervisor struct {
	grace            time.Duration
	onLastClientGone func()
	log              *slog.Logger

	mu           sync.Mutex
	liveCount    int
	armed        bool        // true once a connection has completed the handshake
	pendingTimer *time.Timer // non-nil while grace countdown is running
	graceGen     uint64      // generation of the current countdown

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
//
// A half-close counts as gone: a client that calls shutdown(SHUT_WR) while
// keeping its fd open reads as EOF here, so the daemon treats it as departed
// even though the process is alive. That is correct for the only client that
// exists — Node's default allowHalfOpen: false never half-closes — but a future
// client must hold the write side open for as long as it wants to count as live.
func drainToEOF(conn net.Conn) {
	// ponytail: 32-byte scratch buffer; we never process the payload.
	scratch := make([]byte, 32)
	// There is exactly one timeout worth surviving: the handshake deadline that
	// handleConn failed to clear. Allowing a single retry makes that recoverable
	// while keeping termination a property of the loop rather than a promise
	// about the transport — a conn that reported a successful clear and then
	// timed out again is malfunctioning, and retrying it forever would hot-spin
	// and mask a dead client.
	retriedTimeout := false
	for {
		_, err := conn.Read(scratch)
		if err == nil {
			continue
		}
		// A timeout is NOT a disconnect. With the handshake deadline still
		// armed, treating its expiry as EOF would report a live client as gone
		// and self-stop the daemon after grace — exactly the bug the handshake
		// exists to prevent.
		var netErr net.Error
		if !retriedTimeout && errors.As(err, &netErr) && netErr.Timeout() {
			retriedTimeout = true
			if clearErr := conn.SetReadDeadline(time.Time{}); clearErr == nil {
				continue
			}
		}
		return
	}
}

// clientArrived records a handshaked client: it arms the watchdog, raises the
// live count, and cancels any grace countdown left by a previous disconnect.
func (s *Supervisor) clientArrived() {
	s.mu.Lock()
	s.armed = true
	s.liveCount++
	// A client is live again, so any countdown is void. Raising the count and
	// cancelling must be atomic together: a countdown installed by a departure
	// that had already released mu would be armed while this client is live.
	s.cancelGraceLocked()
	live := s.liveCount
	s.mu.Unlock()

	s.log.Debug("supervisor: client connected", "liveCount", live)
}

// clientGone records the departure of a handshaked client and starts the grace
// countdown when it was the last one. Only handleConn calls it, and only after a
// matching clientArrived.
//
// The decrement, the was-that-the-last-one test, and the countdown install all
// happen under one uninterrupted hold of mu. Splitting them (the shape this
// replaced) let a clientArrived interleave between the test and the install, so
// the countdown could be armed while a client was live and would then be
// orphaned by the next install and fire early against a transient zero.
func (s *Supervisor) clientGone() {
	s.mu.Lock()
	s.liveCount--
	live := s.liveCount
	if s.armed && live == 0 {
		s.startGraceLocked()
	}
	s.mu.Unlock()

	s.log.Debug("supervisor: client disconnected", "liveCount", live)
}

// startGraceLocked replaces any pending countdown with a fresh one. mu must be
// held. time.AfterFunc runs the callback on its own goroutine, so creating the
// timer under mu cannot deadlock.
func (s *Supervisor) startGraceLocked() {
	s.cancelGraceLocked()
	gen := s.graceGen
	s.pendingTimer = time.AfterFunc(s.grace, func() { s.graceElapsed(gen) })
}

// cancelGraceLocked voids the pending countdown, if any. mu must be held.
//
// The generation bump is unconditional and is the load-bearing half: Stop()
// returns false and has no effect once the timer's func has started running, so
// stopping alone cannot guarantee a superseded countdown stays quiet. Bumping
// makes "only the current countdown may fire" a property of the state rather
// than a race that Stop() has to win.
func (s *Supervisor) cancelGraceLocked() {
	if s.pendingTimer != nil {
		s.pendingTimer.Stop()
		s.pendingTimer = nil
	}
	s.graceGen++
}

// graceElapsed runs on the timer's own goroutine when a countdown completes. It
// fires the callback only if its countdown is still the current one and no
// client has come back.
func (s *Supervisor) graceElapsed(gen uint64) {
	s.mu.Lock()
	current := gen == s.graceGen
	live := s.liveCount
	if current {
		s.pendingTimer = nil
	}
	s.mu.Unlock()

	if !current || live != 0 {
		return
	}

	s.log.Info("supervisor: last client gone; grace elapsed, firing callback")
	s.fireOnce.Do(s.onLastClientGone)
}
