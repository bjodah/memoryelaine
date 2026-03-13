package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactHeaders_All(t *testing.T) {
	h := http.Header{
		"Authorization": {"Bearer sk-test123"},
		"Cookie":        {"sid=abc"},
		"Set-Cookie":    {"sid=abc; Path=/"},
	}
	r := RedactHeaders(h)
	if len(r) != 0 {
		t.Errorf("expected empty headers, got %v", r)
	}
}

func TestRedactHeaders_CaseInsensitive(t *testing.T) {
	h := http.Header{}
	h.Set("authorization", "Bearer sk-test")
	h.Set("content-type", "application/json")

	r := RedactHeaders(h)
	if _, ok := r["Authorization"]; ok {
		t.Error("authorization should be redacted (canonical)")
	}
	if r.Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be preserved")
	}
}

func TestHeadersToJSON_Empty(t *testing.T) {
	j := HeadersToJSON(http.Header{})
	if j != "{}" {
		t.Errorf("expected {}, got %s", j)
	}
}

func TestHeadersToJSON_MultiValue(t *testing.T) {
	h := http.Header{
		"Accept": {"text/html", "application/json"},
	}
	j := HeadersToJSON(h)
	if !strings.Contains(j, "text/html") || !strings.Contains(j, "application/json") {
		t.Errorf("expected both values, got %s", j)
	}
}
