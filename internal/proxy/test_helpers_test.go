package proxy

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

func mustWrite(t *testing.T, writer io.Writer, data []byte) {
	t.Helper()
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}
