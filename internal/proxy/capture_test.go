package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCappedBuffer_BelowCap(t *testing.T) {
	cb := newCappedBuffer(100)
	mustWrite(t, cb, []byte("hello"))
	if string(cb.Bytes()) != "hello" {
		t.Errorf("expected 'hello', got %q", cb.Bytes())
	}
	if cb.TotalBytes() != 5 {
		t.Errorf("expected 5, got %d", cb.TotalBytes())
	}
	if cb.Truncated() {
		t.Error("should not be truncated")
	}
}

func TestCappedBuffer_AtCap(t *testing.T) {
	cb := newCappedBuffer(5)
	mustWrite(t, cb, []byte("hello"))
	if string(cb.Bytes()) != "hello" {
		t.Errorf("expected 'hello', got %q", cb.Bytes())
	}
	if cb.Truncated() {
		t.Error("should not be truncated at exact cap")
	}
}

func TestCappedBuffer_AboveCap(t *testing.T) {
	cb := newCappedBuffer(5)
	mustWrite(t, cb, []byte("hello world"))
	if string(cb.Bytes()) != "hello" {
		t.Errorf("expected 'hello', got %q", cb.Bytes())
	}
	if cb.TotalBytes() != 11 {
		t.Errorf("expected 11, got %d", cb.TotalBytes())
	}
	if !cb.Truncated() {
		t.Error("should be truncated")
	}
}

func TestCappedBuffer_MultipleWrites(t *testing.T) {
	cb := newCappedBuffer(8)
	mustWrite(t, cb, []byte("hello"))
	mustWrite(t, cb, []byte(" world"))
	if string(cb.Bytes()) != "hello wo" {
		t.Errorf("expected 'hello wo', got %q", cb.Bytes())
	}
	if cb.TotalBytes() != 11 {
		t.Errorf("expected 11, got %d", cb.TotalBytes())
	}
	if !cb.Truncated() {
		t.Error("should be truncated")
	}
	// Further writes after truncation still count
	mustWrite(t, cb, []byte("more"))
	if cb.TotalBytes() != 15 {
		t.Errorf("expected 15, got %d", cb.TotalBytes())
	}
}

func TestTeeReadCloser(t *testing.T) {
	data := "hello world test data"
	rc := io.NopCloser(strings.NewReader(data))
	trc := newTeeReadCloser(rc, 10)

	buf := make([]byte, 100)
	n, err := trc.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes, got %d", len(data), n)
	}
	// The read should return all data (tee doesn't limit reads)
	if string(buf[:n]) != data {
		t.Errorf("expected %q, got %q", data, string(buf[:n]))
	}
	// But capture should be limited to 10
	if string(trc.tee.Bytes()) != "hello worl" {
		t.Errorf("expected 'hello worl', got %q", trc.tee.Bytes())
	}
	if !trc.tee.Truncated() {
		t.Error("expected truncated")
	}
	if trc.tee.TotalBytes() != int64(len(data)) {
		t.Errorf("expected %d, got %d", len(data), trc.tee.TotalBytes())
	}
}

func TestStatusCapturingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusCapturingWriter(rec)

	sw.WriteHeader(http.StatusNotFound)
	mustWrite(t, sw, []byte("not found"))

	if sw.statusCode != 404 {
		t.Errorf("expected 404, got %d", sw.statusCode)
	}
	if rec.Code != 404 {
		t.Errorf("expected underlying 404, got %d", rec.Code)
	}
}

func TestStatusCapturingWriter_DefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusCapturingWriter(rec)

	mustWrite(t, sw, []byte("ok"))
	if sw.statusCode != 200 {
		t.Errorf("expected default 200, got %d", sw.statusCode)
	}
}

func TestStatusCapturingWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusCapturingWriter(rec)

	// Should not panic even though recorder implements Flusher
	sw.Flush()
	if !rec.Flushed {
		t.Error("expected flush to propagate")
	}
}

func TestRedactHeaders(t *testing.T) {
	h := http.Header{
		"Authorization": {"Bearer sk-test"},
		"Cookie":        {"session=abc"},
		"Set-Cookie":    {"session=abc; Path=/"},
		"Content-Type":  {"application/json"},
		"X-Custom":      {"value"},
	}

	redacted := RedactHeaders(h)

	if _, ok := redacted["Authorization"]; ok {
		t.Error("Authorization should be redacted")
	}
	if _, ok := redacted["Cookie"]; ok {
		t.Error("Cookie should be redacted")
	}
	if _, ok := redacted["Set-Cookie"]; ok {
		t.Error("Set-Cookie should be redacted")
	}
	if redacted.Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be preserved")
	}
	if redacted.Get("X-Custom") != "value" {
		t.Error("X-Custom should be preserved")
	}

	// Original should be untouched
	if h.Get("Authorization") != "Bearer sk-test" {
		t.Error("original Authorization should not be modified")
	}
}

func TestHeadersToJSON(t *testing.T) {
	h := http.Header{
		"Content-Type": {"application/json"},
	}
	j := HeadersToJSON(h)
	if !strings.Contains(j, "application/json") {
		t.Errorf("expected JSON to contain application/json, got %s", j)
	}
}

func TestTeeReadCloser_Close(t *testing.T) {
	data := "test"
	rc := io.NopCloser(bytes.NewReader([]byte(data)))
	trc := newTeeReadCloser(rc, 100)
	if err := trc.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}
