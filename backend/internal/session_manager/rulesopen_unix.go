//go:build unix

package sessionmanager

import (
	"os"
	"syscall"
)

// rulesFileOpenFlag opens a rules file read-only and non-blocking. O_NONBLOCK
// makes open() return immediately even for a FIFO (which would otherwise block
// until a writer appears), so the type check on the resulting handle can reject
// it without a hang — and validating the opened descriptor closes the
// Stat-then-Open race. On a regular file O_NONBLOCK is a no-op.
const rulesFileOpenFlag = os.O_RDONLY | syscall.O_NONBLOCK
