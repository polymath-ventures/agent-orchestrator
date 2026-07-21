//go:build !unix

package sessionmanager

import "os"

// rulesFileOpenFlag opens a rules file read-only. O_NONBLOCK is a POSIX concept
// that does not apply to regular-file opens on non-unix platforms, where open()
// does not block on a regular file; the type check on the opened handle still
// rejects anything that is not a regular file.
const rulesFileOpenFlag = os.O_RDONLY
