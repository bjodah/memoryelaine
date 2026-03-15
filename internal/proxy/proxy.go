package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"memoryelaine/internal/database"
	"memoryelaine/internal/recording"
)

// NewReverseProxy creates a configured httputil.ReverseProxy for capture paths.
// It sets ModifyResponse to tee the response body into a cappedBuffer.
func NewReverseProxy(upstream *url.URL, timeout time.Duration, maxCapture int) *httputil.ReverseProxy {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = singleJoiningSlash(upstream.Path, req.URL.Path)
			req.Host = upstream.Host
		},
		Transport: &http.Transport{
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			// Only bound time-to-first-byte; active streams run indefinitely
			ResponseHeaderTimeout: timeout,
			TLSHandshakeTimeout:   30 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		FlushInterval: -1, // immediate flush for SSE zero-latency streaming
		ModifyResponse: func(resp *http.Response) error {
			if holder := getHolder(resp.Request); holder != nil {
				tee := newTeeReadCloserWithCallback(resp.Body, maxCapture, holder.onResponseReady)
				resp.Body = tee
				holder.respTee = tee
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("upstream error", "error", err, "path", r.URL.Path)
			if holder := getHolder(r); holder != nil {
				errStr := err.Error()
				holder.upstreamErr = &errStr
			}
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return rp
}

// NewPlainReverseProxy creates a reverse proxy with no capture hooks for non-log paths.
func NewPlainReverseProxy(upstream *url.URL, timeout time.Duration) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = singleJoiningSlash(upstream.Path, req.URL.Path)
			req.Host = upstream.Host
		},
		Transport: &http.Transport{
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: timeout,
			TLSHandshakeTimeout:   30 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		FlushInterval: -1,
	}
}

// Handler returns the top-level HTTP handler for the proxy port.
func Handler(
	rpPlain *httputil.ReverseProxy,
	rpCapture *httputil.ReverseProxy,
	logPathSet map[string]struct{},
	maxCapture int,
	logWriter *database.LogWriter,
	recordingState *recording.State,
	upstream *url.URL,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, shouldLog := logPathSet[r.URL.Path]; !shouldLog {
			rpPlain.ServeHTTP(w, r)
			return
		}
		if !recordingState.Enabled() {
			logWriter.MarkLastBodiesStale()
			rpPlain.ServeHTTP(w, r)
			return
		}

		startTime := time.Now()

		// Derive client_ip from RemoteAddr (direct peer only)
		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)

		// Build upstream URL for logging
		upstreamURL := upstream.Scheme + "://" + upstream.Host + singleJoiningSlash(upstream.Path, r.URL.Path)

		// Capture request body via tee (stream-only; zero added latency)
		reqTee := newTeeReadCloserWithCallback(r.Body, maxCapture, func(buf *cappedBuffer) {
			logWriter.SetLastRequest(string(buf.Bytes()))
		})
		r.Body = reqTee

		// Build holder and attach to request context
		holder := &captureHolder{
			reqTee:    reqTee,
			startTime: startTime,
			onResponseReady: func(buf *cappedBuffer) {
				logWriter.SetLastResponse(string(buf.Bytes()))
			},
		}
		r = setHolder(r, holder)

		// Redact and serialize request headers
		reqHeaders := HeadersToJSON(RedactHeaders(r.Header))

		// Capture status code
		sw := newStatusCapturingWriter(w)
		rpCapture.ServeHTTP(sw, r)

		// After ServeHTTP returns, collect capture results
		endTime := time.Now()
		tsStart := startTime.UnixMilli()
		tsEnd := endTime.UnixMilli()
		durationMs := tsEnd - tsStart

		entry := database.LogEntry{
			TsStart:        tsStart,
			TsEnd:          &tsEnd,
			DurationMs:     &durationMs,
			ClientIP:       clientIP,
			RequestMethod:  r.Method,
			RequestPath:    r.URL.Path,
			UpstreamURL:    upstreamURL,
			ReqHeadersJSON: reqHeaders,
			ReqBody:        string(reqTee.tee.Bytes()),
			ReqTruncated:   reqTee.tee.Truncated(),
			ReqBytes:       reqTee.tee.TotalBytes(),
		}

		// Status code
		code := sw.statusCode
		entry.StatusCode = &code

		// Response capture
		if holder.respTee != nil {
			respBody := string(holder.respTee.tee.Bytes())
			entry.RespBody = &respBody
			entry.RespTruncated = holder.respTee.tee.Truncated()
			entry.RespBytes = holder.respTee.tee.TotalBytes()
		}

		// Redact and serialize response headers
		respHeaders := HeadersToJSON(RedactHeaders(sw.Header()))
		entry.RespHeadersJSON = &respHeaders

		// Upstream error
		entry.Error = holder.upstreamErr

		logWriter.Enqueue(entry)
	})
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
