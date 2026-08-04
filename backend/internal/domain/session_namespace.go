package domain

import (
	"errors"
	"strings"
)

// MaxSessionNamespaceLabelBytes bounds only the readable decoration on an
// external namespace key. The complete session ID is always appended verbatim.
const MaxSessionNamespaceLabelBytes = 24

// ComposeSessionNamespaceKey combines a safe creation-time work label with the
// complete non-recycling AO session identity. The result is stored once and is
// never parsed to recover identity; SessionRecord.ID remains authoritative.
func ComposeSessionNamespaceKey(displayName string, id SessionID) (string, error) {
	if id == "" {
		return "", errors.New("session namespace key requires a session id")
	}
	label := sessionNamespaceLabel(displayName)
	if label == "" {
		label = "work"
	}
	return label + "--" + string(id), nil
}

func sessionNamespaceLabel(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		var out byte
		switch {
		case r >= 'a' && r <= 'z':
			out = byte(r)
		case r >= 'A' && r <= 'Z':
			out = byte(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			out = byte(r)
		default:
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		if b.Len() >= MaxSessionNamespaceLabelBytes {
			break
		}
		b.WriteByte(out)
		lastDash = false
	}
	return strings.Trim(b.String(), "-")
}
