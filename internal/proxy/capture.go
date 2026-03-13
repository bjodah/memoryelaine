package proxy

import (
	"io"
	"net/http"
	"sync"
)

// cappedBuffer captures bytes up to a maximum, but always counts total bytes seen.
type cappedBuffer struct {
	buf       []byte
	maxBytes  int
	written   int64
	truncated bool
}

func newCappedBuffer(maxBytes int) *cappedBuffer {
	initial := maxBytes
	if initial > 64*1024 {
		initial = 64 * 1024
	}
	return &cappedBuffer{
		buf:      make([]byte, 0, initial),
		maxBytes: maxBytes,
	}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	c.written += int64(n)

	if !c.truncated {
		remaining := c.maxBytes - len(c.buf)
		if remaining <= 0 {
			c.truncated = true
		} else if n <= remaining {
			c.buf = append(c.buf, p...)
		} else {
			c.buf = append(c.buf, p[:remaining]...)
			c.truncated = true
		}
	}
	return n, nil
}

func (c *cappedBuffer) Bytes() []byte     { return c.buf }
func (c *cappedBuffer) TotalBytes() int64 { return c.written }
func (c *cappedBuffer) Truncated() bool   { return c.truncated }

// teeReadCloser wraps an io.ReadCloser, teeing every Read into a cappedBuffer.
type teeReadCloser struct {
	source io.ReadCloser
	tee    *cappedBuffer
	onDone func(*cappedBuffer)
	done   sync.Once
}

func newTeeReadCloser(rc io.ReadCloser, maxBytes int) *teeReadCloser {
	return newTeeReadCloserWithCallback(rc, maxBytes, nil)
}

func newTeeReadCloserWithCallback(rc io.ReadCloser, maxBytes int, onDone func(*cappedBuffer)) *teeReadCloser {
	return &teeReadCloser{
		source: rc,
		tee:    newCappedBuffer(maxBytes),
		onDone: onDone,
	}
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.source.Read(p)
	if n > 0 {
		if _, teeErr := t.tee.Write(p[:n]); teeErr != nil && err == nil {
			err = teeErr
		}
	}
	if err == io.EOF {
		t.finish()
	}
	return n, err
}

func (t *teeReadCloser) Close() error {
	err := t.source.Close()
	t.finish()
	return err
}

func (t *teeReadCloser) finish() {
	t.done.Do(func() {
		if t.onDone != nil {
			t.onDone(t.tee)
		}
	})
}

// statusCapturingWriter wraps http.ResponseWriter to capture the status code
// and implement http.Flusher for SSE streaming.
type statusCapturingWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newStatusCapturingWriter(w http.ResponseWriter) *statusCapturingWriter {
	return &statusCapturingWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (s *statusCapturingWriter) WriteHeader(code int) {
	if !s.written {
		s.statusCode = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCapturingWriter) Write(b []byte) (int, error) {
	if !s.written {
		s.written = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusCapturingWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (s *statusCapturingWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
