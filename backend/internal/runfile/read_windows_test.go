//go:build windows

package runfile

import (
	"fmt"
	"testing"
)

func TestRetryableReadErrorRecognizesWindowsFileLocks(t *testing.T) {
	for _, err := range []error{
		errorSharingViolation,
		fmt.Errorf("wrapped: %w", errorLockViolation),
	} {
		if !retryableReadError(err) {
			t.Fatalf("retryableReadError(%v) = false, want true", err)
		}
	}

	if retryableReadError(fmt.Errorf("other error")) {
		t.Fatal("retryableReadError(other error) = true, want false")
	}
}
