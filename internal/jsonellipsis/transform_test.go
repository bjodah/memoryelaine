package jsonellipsis

import (
	"encoding/json"
	"testing"
)

func TestTransformBasicObject(t *testing.T) {
	src := []byte(`{"prompt":"a very long string that exceeds the limit"}`)
	out, changed, err := Transform(src, 10, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	val := m["prompt"].(string)
	// First 10 runes of "a very long string that exceeds the limit" = "a very lon"
	expected := "a very lon..."
	if val != expected {
		t.Fatalf("expected %q, got %q", expected, val)
	}
}

func TestTransformNestedObject(t *testing.T) {
	src := []byte(`{"outer":{"inner":"a very long string"}}`)

	// minDepth=2: inner is at depth 2 (outer→inner), should be truncated
	out, changed, err := Transform(src, 10, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	inner := m["outer"].(map[string]any)["inner"].(string)
	expected := "a very lon..."
	if inner != expected {
		t.Fatalf("expected %q, got %q", expected, inner)
	}

	// minDepth=3: inner at depth 2 should NOT be truncated
	out2, changed2, err := Transform(src, 10, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatal("expected changed=false at minDepth=3")
	}
	var m2 map[string]any
	if err := json.Unmarshal(out2, &m2); err != nil {
		t.Fatal(err)
	}
	inner2 := m2["outer"].(map[string]any)["inner"].(string)
	if inner2 != "a very long string" {
		t.Fatalf("expected original string, got %q", inner2)
	}
}

func TestTransformArray(t *testing.T) {
	src := []byte(`{"items":["short","a very long string that exceeds"]}`)
	out, changed, err := Transform(src, 10, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	items := m["items"].([]any)
	if items[0].(string) != "short" {
		t.Fatalf("expected short string unchanged, got %q", items[0])
	}
	expected := "a very lon..."
	if items[1].(string) != expected {
		t.Fatalf("expected %q, got %q", expected, items[1])
	}
}

func TestTransformKeyFiltering(t *testing.T) {
	src := []byte(`{"prompt":"long string value here","description":"long string value here"}`)
	out, changed, err := Transform(src, 5, DefaultKeys, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	prompt := m["prompt"].(string)
	if prompt != "long ..." {
		t.Fatalf("expected %q, got %q", "long ...", prompt)
	}
	desc := m["description"].(string)
	if desc != "long string value here" {
		t.Fatalf("expected description unchanged, got %q", desc)
	}
}

func TestTransformMinDepth(t *testing.T) {
	src := []byte(`{"shallow":"long string value here","nested":{"deep":"long string value here"}}`)
	out, changed, err := Transform(src, 5, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	shallow := m["shallow"].(string)
	if shallow != "long string value here" {
		t.Fatalf("expected shallow unchanged, got %q", shallow)
	}
	deep := m["nested"].(map[string]any)["deep"].(string)
	if deep != "long ..." {
		t.Fatalf("expected %q, got %q", "long ...", deep)
	}
}

func TestTransformNoChanges(t *testing.T) {
	src := []byte(`{"key":"short"}`)
	out, changed, err := Transform(src, 100, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var original, result map[string]any
	if err := json.Unmarshal(src, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if original["key"] != result["key"] {
		t.Fatalf("expected same content, got %v vs %v", original["key"], result["key"])
	}
}

func TestTransformNonJSON(t *testing.T) {
	src := []byte(`not valid json {{`)
	_, _, err := Transform(src, 10, nil, 1)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTransformPreservesScalars(t *testing.T) {
	src := []byte(`{"num":42,"bool":true,"null":null,"str":"ok"}`)
	out, changed, err := Transform(src, 100, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	// Number preserved (may be float64 after re-unmarshal without UseNumber)
	num, ok := m["num"]
	if !ok || num == nil {
		t.Fatal("num missing")
	}
	b, ok := m["bool"]
	if !ok || b != true {
		t.Fatalf("expected bool=true, got %v", b)
	}
	if m["null"] != nil {
		t.Fatalf("expected null, got %v", m["null"])
	}
	if m["str"] != "ok" {
		t.Fatalf("expected str=ok, got %v", m["str"])
	}
}

func TestTransformUnicodeStrings(t *testing.T) {
	src := []byte(`{"content":"日本語のテキストがここにあります"}`)
	out, changed, err := Transform(src, 5, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	val := m["content"].(string)
	expected := "日本語のテ..."
	if val != expected {
		t.Fatalf("expected %q, got %q", expected, val)
	}
}

func TestTransformDefaultKeys(t *testing.T) {
	src := []byte(`{"prompt":"a very long prompt string exceeding limit"}`)
	out, changed, err := Transform(src, 10, DefaultKeys, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	val := m["prompt"].(string)
	expected := "a very lon..."
	if val != expected {
		t.Fatalf("expected %q, got %q", expected, val)
	}
}
