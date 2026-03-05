package proxy

import (
	"context"
	"net/http"
	"time"
)

type contextKey string

const holderKey contextKey = "capture"

// captureHolder is stored in the request context to pass capture state
// between the handler, the Director, and ModifyResponse.
type captureHolder struct {
	reqTee      *teeReadCloser
	respTee     *teeReadCloser
	startTime   time.Time
	upstreamErr *string
}

func setHolder(r *http.Request, h *captureHolder) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), holderKey, h))
}

func getHolder(r *http.Request) *captureHolder {
	if v := r.Context().Value(holderKey); v != nil {
		return v.(*captureHolder)
	}
	return nil
}
