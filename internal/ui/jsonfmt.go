package ui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	inlineMaxPairs = 8 // max object keys shown in the inline summary
	inlineArrayN   = 3 // max array elements shown inline
)

// inlineJSON renders a JSON object as a compact, colorized `key=value …`
// summary with all escapes decoded (so "&" shows as "&"). It preserves key
// order. Returns ok=false when raw is not a JSON object, so the caller can fall
// back to its plain-text summary. The returned string is full width; the caller
// truncates to fit.
func inlineJSON(raw string, st styles) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return "", false
	}

	var parts []string
	n := 0
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return "", false
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return "", false
		}
		if n >= inlineMaxPairs {
			parts = append(parts, st.jsonPn.Render("…"))
			// still drain the decoder so it stays well-formed, but stop adding
			break
		}
		parts = append(parts, st.jsonKey.Render(key)+st.jsonPn.Render("=")+compactValue(val, st))
		n++
	}
	if len(parts) == 0 {
		return st.jsonPn.Render("{}"), true
	}
	return strings.Join(parts, " "), true
}

// compactValue renders a single JSON value on one line, decoded and colorized.
func compactValue(raw json.RawMessage, st styles) string {
	t := strings.TrimSpace(string(raw))
	if t == "" {
		return ""
	}
	switch t[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return st.jsonStr.Render(t)
		}
		return st.jsonStr.Render("\"" + oneLine(s) + "\"")
	case '{':
		return st.jsonPn.Render("{…}")
	case '[':
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil || len(elems) == 0 {
			return st.jsonPn.Render("[]")
		}
		shown := make([]string, 0, inlineArrayN)
		for i, e := range elems {
			if i >= inlineArrayN {
				shown = append(shown, st.jsonPn.Render("…"))
				break
			}
			shown = append(shown, compactValue(e, st))
		}
		return st.jsonPn.Render("[") + strings.Join(shown, st.jsonPn.Render(", ")) + st.jsonPn.Render("]")
	default: // number, bool, null
		return st.jsonNum.Render(t)
	}
}

// oneLine flattens whitespace runs to single spaces for inline display.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// prettyJSON pretty-prints raw as indented, colorized JSON wrapped to width.
// Escapes are decoded (& stays &, not &). When raw is not valid JSON it is
// returned as wrapped plain text, so non-JSON payloads (e.g. assistant prose)
// still expand usefully.
func prettyJSON(raw string, st styles, width int) string {
	if width < 8 {
		width = 8
	}
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return wrap(strings.TrimSpace(raw), width)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep &, <, > literal
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return wrap(raw, width)
	}
	return wrap(colorizeJSON(strings.TrimRight(buf.String(), "\n"), st), width)
}

// colorizeJSON applies subtle syntax colors to already-indented JSON. It walks
// characters so it can tell an object key ("x":) from a string value.
func colorizeJSON(s string, st styles) string {
	var b strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == '"':
			// read the whole string literal (handling escapes)
			j := i + 1
			for j < len(runes) {
				if runes[j] == '\\' {
					j += 2
					continue
				}
				if runes[j] == '"' {
					break
				}
				j++
			}
			if j >= len(runes) {
				j = len(runes) - 1
			}
			lit := string(runes[i : j+1])
			// key if the next non-space char is ':'
			k := j + 1
			for k < len(runes) && (runes[k] == ' ' || runes[k] == '\t') {
				k++
			}
			if k < len(runes) && runes[k] == ':' {
				b.WriteString(st.jsonKey.Render(lit))
			} else {
				b.WriteString(st.jsonStr.Render(lit))
			}
			i = j + 1
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',':
			b.WriteString(st.jsonPn.Render(string(c)))
			i++
		case c == '-' || (c >= '0' && c <= '9') || isLetter(c):
			j := i
			for j < len(runes) && (isScalarRune(runes[j])) {
				j++
			}
			b.WriteString(st.jsonNum.Render(string(runes[i:j])))
			i = j
		default: // whitespace and newlines pass through uncolored
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}

func isLetter(c rune) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isScalarRune(c rune) bool {
	return isLetter(c) || (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
}

// wrap soft-wraps a (possibly styled) string to width columns, ANSI-aware.
func wrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
