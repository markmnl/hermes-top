package derive

import (
	"database/sql"
	"testing"

	"github.com/markmnl/hermes-top/internal/db"
)

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// mkMsg builds a db.Message for tests. toolCalls/toolCallID are set only when
// non-empty so NULL handling is exercised realistically.
func mkMsg(role, content, toolCalls, toolCallID, session string, ts float64) db.Message {
	m := db.Message{SessionID: session, Role: role, Content: content, Timestamp: ts, Active: true}
	if toolCalls != "" {
		m.ToolCalls = ns(toolCalls)
	}
	if toolCallID != "" {
		m.ToolCallID = ns(toolCallID)
	}
	return m
}

func TestActionsFromAssistant_RealRow(t *testing.T) {
	// Copied verbatim from ~/.hermes/state.db (message id 644).
	raw := `[{"id": "call_yhtqce2f", "call_id": "call_yhtqce2f", "response_item_id": "fc_yhtqce2f", "type": "function", "function": {"name": "computer_use", "arguments": "{\"action\":\"type\",\"text\":\"hermes computer-use doctor\"}"}}]`
	acts := ActionsFromAssistant(mkMsg("assistant", "", raw, "", "sess1", 1000))
	if len(acts) != 1 {
		t.Fatalf("want 1 action, got %d", len(acts))
	}
	a := acts[0]
	if a.CallID != "call_yhtqce2f" {
		t.Errorf("CallID = %q", a.CallID)
	}
	if a.Name != "computer_use" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.Status != ActionPending {
		t.Errorf("Status = %v, want pending", a.Status)
	}
	if a.ArgsSummary == "" {
		t.Errorf("ArgsSummary empty")
	}
	if a.ArgsRaw != `{"action":"type","text":"hermes computer-use doctor"}` {
		t.Errorf("ArgsRaw should retain full untruncated JSON, got %q", a.ArgsRaw)
	}
	if a.Done() {
		t.Errorf("action should not be Done before result")
	}
}

func TestActionsFromAssistant_MultipleAndMalformed(t *testing.T) {
	multi := `[{"id":"c1","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":"b","arguments":"{}"}}]`
	if got := len(ActionsFromAssistant(mkMsg("assistant", "", multi, "", "s", 1))); got != 2 {
		t.Errorf("multi: want 2, got %d", got)
	}

	bad := `not json at all`
	acts := ActionsFromAssistant(mkMsg("assistant", "", bad, "", "s", 1))
	if len(acts) != 1 {
		t.Fatalf("malformed: want 1 fallback action, got %d", len(acts))
	}
	if acts[0].Name != "tool_calls?" {
		t.Errorf("malformed fallback name = %q", acts[0].Name)
	}

	if ActionsFromAssistant(mkMsg("assistant", "", "", "", "s", 1)) != nil {
		t.Errorf("empty tool_calls should yield nil")
	}
}

func TestApplyToolResult_PairingAndDuration(t *testing.T) {
	raw := `[{"id":"call_x","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"ls\"}"}}]`
	a := ActionsFromAssistant(mkMsg("assistant", "", raw, "", "s", 1000))[0]
	result := mkMsg("tool", `{"ok": true, "output": "file1\nfile2"}`, "", "call_x", "s", 1002.5)
	result.ToolName = ns("bash")
	ApplyToolResult(&a, result)

	if !a.Done() {
		t.Fatal("action should be Done after result")
	}
	if a.Status != ActionOK {
		t.Errorf("Status = %v, want ok", a.Status)
	}
	if d := a.Duration(a.EndedAt); d.Seconds() != 2.5 {
		t.Errorf("Duration = %v, want 2.5s", d)
	}
	if a.ResultRaw != `{"ok": true, "output": "file1\nfile2"}` {
		t.Errorf("ResultRaw should retain full result content, got %q", a.ResultRaw)
	}
}

func TestMessageEventRaw(t *testing.T) {
	// tool result: Raw is the content
	tr := mkMsg("tool", `{"error":"denied"}`, "", "c", "s", 1)
	tr.ToolName = ns("bash")
	if ev, _ := MessageEvent(tr); ev.Raw != `{"error":"denied"}` {
		t.Errorf("tool result Raw = %q", ev.Raw)
	}
	// single tool call: Raw is the arguments JSON, not the whole array
	tc := mkMsg("assistant", "", `[{"id":"c","function":{"name":"bash","arguments":"{\"cmd\":\"ls\"}"}}]`, "", "s", 1)
	if ev, _ := MessageEvent(tc); ev.Raw != `{"cmd":"ls"}` {
		t.Errorf("single tool call Raw = %q", ev.Raw)
	}
	// plain user text: Raw is the full content
	um := mkMsg("user", "hello world", "", "", "s", 1)
	if ev, _ := MessageEvent(um); ev.Raw != "hello world" {
		t.Errorf("user Raw = %q", ev.Raw)
	}
}

func TestSniffStatus(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    ActionStatus
	}{
		{"json error key", `{"error": "denied by user", "action": "type"}`, ActionError},
		{"json ok false", `{"ok": false, "action": "click", "message": "No active window"}`, ActionError},
		{"json is_error", `{"is_error": true, "msg": "boom"}`, ActionError},
		{"json status error", `{"status": "error"}`, ActionError},
		{"json success", `{"apps": [{"name": "systemd", "pid": 1}]}`, ActionOK},
		{"json ok true", `{"ok": true, "output": "done"}`, ActionOK},
		{"empty", "", ActionOK},
		{"plain error prefix", "Error: command not found", ActionError},
		{"traceback", "Traceback (most recent call last):", ActionError},
		{"plain success text", "42 files processed successfully", ActionOK},
		{"json empty error string", `{"error": ""}`, ActionOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SniffStatus(c.content); got != c.want {
				t.Errorf("SniffStatus(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestTruncateAndCollapse(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("no-trunc = %q", got)
	}
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("trunc = %q", got)
	}
	if got := collapse("a\n\tb   c\n"); got != "a b c" {
		t.Errorf("collapse = %q", got)
	}
	// multibyte safety
	if got := truncate("héllo wörld", 6); len([]rune(got)) != 6 {
		t.Errorf("multibyte trunc rune count = %d", len([]rune(got)))
	}
}

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"20260704_090016_5945c6":            "5945c6",
		"cron_59972e48f818_20260703_083123": "083123",
		"short":                             "short",
		"":                                  "-",
	}
	for in, want := range cases {
		if got := ShortID(in); got != want {
			t.Errorf("ShortID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMessageEvent_Kinds(t *testing.T) {
	tc := mkMsg("assistant", "", `[{"id":"c","type":"function","function":{"name":"bash","arguments":"{}"}}]`, "", "s", 1)
	if ev, _ := MessageEvent(tc); ev.Kind != EvToolCall {
		t.Errorf("tool call kind = %v", ev.Kind)
	}
	tr := mkMsg("tool", `{"error":"x"}`, "", "c", "s", 1)
	tr.ToolName = ns("bash")
	if ev, _ := MessageEvent(tr); ev.Kind != EvToolResult || !ev.Err {
		t.Errorf("tool result kind=%v err=%v", ev.Kind, ev.Err)
	}
	um := mkMsg("user", "hello there", "", "", "s", 1)
	if ev, _ := MessageEvent(um); ev.Kind != EvUser {
		t.Errorf("user kind = %v", ev.Kind)
	}
}

func TestEventMatches(t *testing.T) {
	e := Event{Short: "5945c6", Role: "tool", Summary: "← computer_use: denied by user"}
	if !e.Matches("") {
		t.Error("empty filter should match")
	}
	if !e.Matches("DENIED") {
		t.Error("case-insensitive summary match failed")
	}
	if !e.Matches("5945") {
		t.Error("short id match failed")
	}
	if e.Matches("zzz") {
		t.Error("non-matching filter matched")
	}
}
