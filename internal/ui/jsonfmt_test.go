package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestInlineJSON(t *testing.T) {
	st := newStyles()

	// object → key=value, ok
	out, ok := inlineJSON(`{"action":"type","text":"hi"}`, st)
	if !ok {
		t.Fatal("object should be ok")
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "action=") || !strings.Contains(plain, `"type"`) {
		t.Errorf("inline object = %q", plain)
	}
	if !strings.Contains(plain, "text=") {
		t.Errorf("missing second key: %q", plain)
	}

	// unicode escapes must be decoded
	out, _ = inlineJSON(`{"cmd":"a&b>c"}`, st)
	if plain := ansi.Strip(out); !strings.Contains(plain, "a&b>c") {
		t.Errorf("escapes not decoded: %q", plain)
	}

	// nested + array compaction
	out, _ = inlineJSON(`{"apps":[{"n":1},{"n":2},{"n":3},{"n":4}],"obj":{"x":1}}`, st)
	plain = ansi.Strip(out)
	if !strings.Contains(plain, "apps=[") || !strings.Contains(plain, "…") {
		t.Errorf("array not compacted: %q", plain)
	}
	if !strings.Contains(plain, "obj={…}") {
		t.Errorf("nested object not compacted: %q", plain)
	}

	// non-objects → not ok
	for _, s := range []string{`[1,2,3]`, `"just a string"`, `42`, ``, `not json`} {
		if _, ok := inlineJSON(s, st); ok {
			t.Errorf("inlineJSON(%q) should be ok=false", s)
		}
	}
}

func TestPrettyJSON(t *testing.T) {
	st := newStyles()

	// valid JSON → multi-line, indented, escapes decoded
	out := prettyJSON(`{"a":1,"b":{"c":"x&y"}}`, st, 40)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "\n") {
		t.Errorf("pretty JSON should be multi-line: %q", plain)
	}
	if !strings.Contains(plain, "x&y") {
		t.Errorf("escapes should be decoded: %q", plain)
	}
	if !strings.Contains(plain, "\"a\"") {
		t.Errorf("key should be present: %q", plain)
	}

	// invalid JSON → returned as (wrapped) plain text
	out = prettyJSON("this is not json, just prose", st, 40)
	if plain := ansi.Strip(out); !strings.Contains(plain, "not json") {
		t.Errorf("non-JSON should pass through: %q", plain)
	}
}
