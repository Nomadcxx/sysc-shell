package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// decodeReply parses one reply line into a generic map so a test can check the
// exact keys present.
func decodeReply(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("reply %s: %v", line, err)
	}
	return m
}

func TestErrorsCarryCodes(t *testing.T) {
	s := NewServer("", Handlers{
		Panel:  func(string, string, string) error { return errors.New("panel refused") },
		Status: func() map[string]any { return map[string]any{"version": "test"} },
	})
	cases := []struct {
		name, line, code, msg string
	}{
		{"unknown method", `{"id":1,"method":"nope"}`, CodeUnsupported, "unknown method"},
		{"malformed json", `{"id":`, CodeInvalidParams, "malformed json"},
		{"legacy handler error", `{"id":2,"method":"panel.open","params":{"panel":"clock"}}`, "", "panel refused"},
		{"legacy unset handler", `{"id":3,"method":"theme.get"}`, "", "theme handler unset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeReply(t, s.handleLine(tc.line))
			if m["error"] != tc.msg {
				t.Fatalf("error = %v, want %q", m["error"], tc.msg)
			}
			code, has := m["code"]
			if tc.code == "" && has {
				t.Fatalf("legacy error gained a code: %v", code)
			}
			if tc.code != "" && code != tc.code {
				t.Fatalf("code = %v, want %q", code, tc.code)
			}
		})
	}

	// A legacy success keeps its flat shape, byte for byte.
	got := string(s.handleLine(`{"id":7,"method":"status"}`))
	if want := `{"id":7,"ok":true,"version":"test"}`; got != want {
		t.Fatalf("status = %s, want %s", got, want)
	}
}

func TestAPIMethodStrictParams(t *testing.T) {
	s := NewServer("", Handlers{API: map[string]APIMethod{
		"test.echo": func(raw json.RawMessage) (any, error) {
			var p struct {
				N int `json:"n"`
			}
			if err := DecodeParams(raw, &p); err != nil {
				return nil, err
			}
			return map[string]int{"n": p.N}, nil
		},
		"test.coded": func(json.RawMessage) (any, error) {
			return nil, &Error{Code: CodeBusy, Message: "try later"}
		},
		"test.plain": func(json.RawMessage) (any, error) {
			return nil, errors.New("plain failure")
		},
	}})
	for _, tc := range []struct{ name, params string }{
		{"unknown field", `{"n":1,"extra":2}`},
		{"wrong type", `{"n":"one"}`},
		{"not an object", `[1]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeReply(t, s.handleLine(`{"id":1,"method":"test.echo","params":`+tc.params+`}`))
			if m["code"] != CodeInvalidParams {
				t.Fatalf("reply = %v, want invalid_params", m)
			}
		})
	}
	var p struct{}
	if err := DecodeParams(json.RawMessage(`{} {}`), &p); !isCode(err, CodeInvalidParams) {
		t.Fatalf("trailing data: %v, want invalid_params", err)
	}
	if err := DecodeParams(nil, &p); err != nil {
		t.Fatalf("absent params: %v", err)
	}

	got := string(s.handleLine(`{"id":4,"method":"test.echo","params":{"n":3}}`))
	if want := `{"id":4,"ok":true,"result":{"n":3}}`; got != want {
		t.Fatalf("success = %s, want %s", got, want)
	}
	if m := decodeReply(t, s.handleLine(`{"id":5,"method":"test.coded"}`)); m["code"] != CodeBusy || m["error"] != "try later" {
		t.Fatalf("coded = %v", m)
	}
	if m := decodeReply(t, s.handleLine(`{"id":6,"method":"test.plain"}`)); m["error"] != "plain failure" || m["code"] != nil {
		t.Fatalf("plain = %v", m)
	}
}

func isCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

func TestCapabilitiesMatchDispatch(t *testing.T) {
	noop := Provider{Start: func(publish func(any)) error { publish(1); return nil }, Stop: func() {}}
	s := NewServer("", Handlers{
		API: map[string]APIMethod{
			"test.present": func(json.RawMessage) (any, error) { return nil, nil },
			"test.absent":  nil,
		},
		Topics: map[string]Provider{"fake": noop},
	})
	m := decodeReply(t, s.handleLine(`{"id":1,"method":"ipc.capabilities"}`))
	raw, _ := json.Marshal(m["result"])
	var caps struct {
		API      int            `json:"api"`
		Methods  []string       `json:"methods"`
		Families []string       `json:"families"`
		Topics   []string       `json:"topics"`
		Limits   map[string]int `json:"limits"`
	}
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatalf("capabilities %s: %v", raw, err)
	}
	if caps.API != 2 {
		t.Fatalf("api = %d, want 2", caps.API)
	}
	want := append(Methods(), "test.present")
	sort.Strings(want)
	if !reflect.DeepEqual(caps.Methods, want) {
		t.Fatalf("methods = %v, want %v", caps.Methods, want)
	}
	if !sort.StringsAreSorted(caps.Methods) {
		t.Fatalf("methods unsorted: %v", caps.Methods)
	}
	// Every listed method dispatches: none answers unsupported.
	for _, name := range caps.Methods {
		r := decodeReply(t, s.handleLine(`{"id":1,"method":"`+name+`"}`))
		if r["code"] == CodeUnsupported {
			t.Errorf("listed method %s answers unsupported", name)
		}
	}
	// A nil API entry is absent, and absent means unsupported.
	if r := decodeReply(t, s.handleLine(`{"id":1,"method":"test.absent"}`)); r["code"] != CodeUnsupported {
		t.Fatalf("nil API entry = %v, want unsupported", r)
	}
	if !reflect.DeepEqual(caps.Families, MethodPrefixes()) {
		t.Fatalf("families = %v", caps.Families)
	}
	if !reflect.DeepEqual(caps.Topics, []string{"fake"}) {
		t.Fatalf("topics = %v", caps.Topics)
	}
	wantLimits := map[string]int{
		"line_bytes": 65536, "consumers": 32, "topics_per_connection": 16,
		"pending_bytes": 2097152, "write_timeout_ms": 5000,
	}
	if !reflect.DeepEqual(caps.Limits, wantLimits) {
		t.Fatalf("limits = %v", caps.Limits)
	}

	// With no providers the topic list is empty, not absent.
	m = decodeReply(t, NewServer("", Handlers{}).handleLine(`{"id":1,"method":"ipc.capabilities"}`))
	if topics := m["result"].(map[string]any)["topics"]; !reflect.DeepEqual(topics, []any{}) {
		t.Fatalf("topics without providers = %#v", topics)
	}
}

func TestLineLimit(t *testing.T) {
	sock, cancel := startServer(t, Handlers{Status: func() map[string]any { return map[string]any{} }})
	defer cancel()

	// A request padded to exactly the limit is processed.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	head := `{"id":1,"method":"status"}`
	exact := head + strings.Repeat(" ", lineBytes-len(head))
	if _, err := conn.Write([]byte(exact + "\n")); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(conn)
	line, err := rd.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeReply(t, line); m["ok"] != true {
		t.Fatalf("exact-limit line = %s", line)
	}

	// One byte more answers limit_exceeded and closes.
	if _, err := conn.Write([]byte(exact + " \n")); err != nil {
		t.Fatal(err)
	}
	line, err = rd.ReadBytes('\n')
	if err != nil {
		t.Fatalf("oversize line: no reply: %v", err)
	}
	if m := decodeReply(t, line); m["code"] != CodeLimitExceeded {
		t.Fatalf("oversize reply = %s", line)
	}
	if _, err := rd.ReadBytes('\n'); err == nil {
		t.Fatal("connection still open after an oversize line")
	}
}
