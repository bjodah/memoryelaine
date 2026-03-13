package proxy

import (
	"encoding/json"
	"net/http"
)

// redactedHeaders is the set of headers to strip before logging.
var redactedHeaders = map[string]struct{}{
	"Authorization": {},
	"Cookie":        {},
	"Set-Cookie":    {},
}

// RedactHeaders returns a shallow copy of the header map with sensitive
// headers removed. The original is not modified.
// Applied to BOTH request and response headers before DB serialization.
func RedactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		if _, redact := redactedHeaders[http.CanonicalHeaderKey(k)]; redact {
			continue
		}
		out[k] = v
	}
	return out
}

// HeadersToJSON serializes an http.Header to a JSON string.
// Returns "{}" on marshal error.
func HeadersToJSON(h http.Header) string {
	b, err := json.Marshal(h)
	if err != nil {
		return "{}"
	}
	return string(b)
}
