package procdiag

import (
	"regexp"
	"strings"
	"sync"
)

// Limit is the maximum retained stderr tail size.
const Limit = 8 << 10

var (
	secretValuePattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)(["']?\s*[:=]\s*["']?|[^\S\r\n]+)([^\s"']+)`) //nolint:gochecknoglobals // compiled once for stderr redaction
	authValuePattern   = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*)["']?(?:bearer|basic)?\s*(\S+)`)                   //nolint:gochecknoglobals // compiled once for stderr redaction
	openAIKeyPattern   = regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)                                                              //nolint:gochecknoglobals // compiled once for stderr redaction
	jwtPattern         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)                      //nolint:gochecknoglobals // compiled once for stderr redaction
)

// Tail drains and retains a bounded, redacted tail of child stderr for startup diagnostics.
type Tail struct {
	mu   sync.Mutex
	buf  []byte
	full bool
}

func (t *Tail) Write(p []byte) (int, error) {
	n := len(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) > Limit {
		p = p[len(p)-Limit:]
		t.buf = append(t.buf[:0], p...)
		t.full = true
		return n, nil
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > Limit {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-Limit:]...)
		t.full = true
	}
	return n, nil
}

func (t *Tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) == 0 {
		return ""
	}
	out := string(t.buf)
	out = secretValuePattern.ReplaceAllString(out, "$1$2[redacted]")
	out = authValuePattern.ReplaceAllString(out, "$1[redacted]")
	out = openAIKeyPattern.ReplaceAllString(out, "[redacted]")
	out = jwtPattern.ReplaceAllString(out, "[redacted]")
	if t.full {
		return "…" + out
	}
	return out
}

// Diagnostics formats a retained stderr tail for appending to an error.
func Diagnostics(label, tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return ""
	}
	return "; " + label + ": " + tail
}
