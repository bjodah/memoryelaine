package database

import (
	"io"
	"testing"
)

func mustClose(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close failed: %v", err)
	}
}
