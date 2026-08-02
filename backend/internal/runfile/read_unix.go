//go:build unix

package runfile

func retryableReadError(error) bool {
	return false
}
