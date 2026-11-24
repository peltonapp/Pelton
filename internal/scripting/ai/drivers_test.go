package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testKey = "sk-test-0123456789abcdef"

// recorded is one request a fake provider got.
type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   map[string]any
}

// fakeProvider answers each request with the next of answers, as JSON with
// the status, and records what it got.
type fakeProvider struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	got     []recorded
	answers []func(r recorded) (int, any)
}

func newFake(t *testing.T, answers ...func(r recorded) (int, any)) *fakeProvider {
	f := unstartedFake(t, answers...)
	f.srv.Start()
	return f
}

// newFakeTLS is newFake served over https with httptest's certificate.
func newFakeTLS(t *testing.T, answers ...func(r recorded) (int, any)) *fakeProvider {
	f := unstartedFake(t, answers...)
	f.srv.StartTLS()
	return f
}

func unstartedFake(t *testing.T, answers ...func(r recorded) (int, any)) *fakeProvider {
	f := &fakeProvider{t: t, answers: answers}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		rec := recorded{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone()}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &rec.Body); err != nil {
				t.Errorf("request body is not JSON: %s", data)
			}
		}
		f.mu.Lock()
		i := len(f.got)
		f.got = append(f.got, rec)
		f.mu.Unlock()
		if i >= len(f.answers) {
			t.Errorf("unexpected request %d to %s", i+1, rec.Path)
			w.WriteHeader(500)
			return
		}
		status, out := f.answers[i](rec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeProvider) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.got...)
}

func reply(v any) func(recorded) (int, any) {
	return func(recorded) (int, any) { return 200, v }
}

func oaAnswer(text string) map[string]any {
	return map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}},
		"usage":   map[string]any{"prompt_tokens": 11, "completion_tokens": 3},
	}
}

func endpoint(t *testing.T, kind, base, key string) (*Endpoint, Driver) {
	t.Helper()
	k, ok := KindByID(kind)
	if !ok {
		t.Fatalf("no kind %q", kind)
	}
	ep, _, err := Network{}.Open(context.Background(), Provider{ID: "p", Name: "P", Kind: kind, BaseURL: base}, key, 0)
	if err != nil {
		t.Fatal(err)
	}
	return ep, k.Driver
}

func TestOpenAIStyleRequestShape(t *testing.T) {
	cases := []struct {
		kind, tokens, format string
	}{
		{"openai", "max_completion_tokens", "json_schema"},
		{"mistral", "max_tokens", "json_object"},
		{"vllm", "max_tokens", "json_schema"},
		{"openai_compatible", "max_tokens", ""},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			f := newFake(t, reply(oaAnswer("hello")))
			ep, d := endpoint(t, tc.kind, f.srv.URL+"/v1", testKey)
			resp, err := d.Chat(context.Background(), ep, Request{
				Model: "m1", System: "sys", MaxTokens: 50,
				Messages: []Message{{Role: "user", Text: "hi"}},
				Schema:   classifySchema([]string{"a", "b"}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Text != "hello" || resp.InputTokens != 11 || resp.OutputTokens != 3 {
				t.Fatalf("response %+v", resp)
			}
			r := f.requests()[0]
			if r.Method != "POST" || r.Path != "/v1/chat/completions" {
				t.Fatalf("%s %s", r.Method, r.Path)
			}
			if r.Header.Get("Authorization") != "Bearer "+testKey || r.Header.Get("User-Agent") != UserAgent {
				t.Fatalf("headers %v", r.Header)
			}
			if r.Body["stream"] != false || r.Body["model"] != "m1" || r.Body[tc.tokens] != float64(50) {
				t.Fatalf("body %v", r.Body)
			}
			msgs := r.Body["messages"].([]any)
			if first := msgs[0].(map[string]any); first["role"] != "system" || first["content"] != "sys" {
				t.Fatalf("messages %v", msgs)
			}
			rf, _ := r.Body["response_format"].(map[string]any)
			if got, _ := rf["type"].(string); got != tc.format {
				t.Fatalf("response_format %v, want %q", r.Body["response_format"], tc.format)
			}
		})
	}
}

func TestOpenAIToolCalls(t *testing.T) {
	f := newFake(t, reply(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": nil,
			"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "mail_flag", "arguments": `{"message":5}`}}},
		}}},
	}))
	ep, d := endpoint(t, "openai", f.srv.URL, "")
	resp, err := d.Chat(context.Background(), ep, Request{
		Model: "m", Messages: []Message{
			{Role: "user", Text: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c0", Name: "x", Args: map[string]any{"a": 1.0}}}},
			{Role: "tool", ToolCallID: "c0", Text: "done"},
		},
		Tools: []Tool{{Name: "mail_flag", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "c1" || resp.ToolCalls[0].Args["message"] != 5.0 {
		t.Fatalf("tool calls %+v", resp.ToolCalls)
	}
	r := f.requests()[0]
	if r.Header.Get("Authorization") != "" {
		t.Fatal("sent an authorization header without a key")
	}
	msgs := r.Body["messages"].([]any)
	asst := msgs[1].(map[string]any)
	call := asst["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["arguments"] != `{"a":1}` {
		t.Fatalf("assistant turn %v", asst)
	}
	if tool := msgs[2].(map[string]any); tool["role"] != "tool" || tool["tool_call_id"] != "c0" {
		t.Fatalf("tool turn %v", tool)
	}
	if tools := r.Body["tools"].([]any); tools[0].(map[string]any)["type"] != "function" {
		t.Fatalf("tools %v", tools)
	}
}

func TestAnthropicRequestShape(t *testing.T) {
	f := newFake(t, reply(map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "let me"},
			map[string]any{"type": "tool_use", "id": "tu1", "name": "mail_flag", "input": map[string]any{"message": 3}},
		},
		"usage": map[string]any{"input_tokens": 20, "output_tokens": 7},
	}))
	ep, d := endpoint(t, "anthropic", f.srv.URL+"/v1", testKey)
	resp, err := d.Chat(context.Background(), ep, Request{
		Model: "claude-x", System: "sys",
		Messages: []Message{
			{Role: "user", Text: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "a", Name: "t1"}, {ID: "b", Name: "t2"}}},
			{Role: "tool", ToolCallID: "a", Text: "one"},
			{Role: "tool", ToolCallID: "b", Text: "two", ToolError: true},
		},
		Tools: []Tool{{Name: "mail_flag", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "let me" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "tu1" || resp.InputTokens != 20 {
		t.Fatalf("response %+v", resp)
	}
	r := f.requests()[0]
	if r.Path != "/v1/messages" || r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") != AnthropicVersion {
		t.Fatalf("%s %v", r.Path, r.Header)
	}
	if r.Header.Get("Authorization") != "" {
		t.Fatal("anthropic got a bearer header")
	}
	if r.Body["system"] != "sys" || r.Body["max_tokens"] != float64(defaultAnthropicTokens) || r.Body["stream"] != false {
		t.Fatalf("body %v", r.Body)
	}
	msgs := r.Body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("tool results should share one user turn: %v", msgs)
	}
	asst := msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if asst["type"] != "tool_use" || asst["input"] == nil {
		t.Fatalf("tool use block %v", asst)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 || results[1].(map[string]any)["is_error"] != true {
		t.Fatalf("tool results %v", results)
	}
	if tools := r.Body["tools"].([]any); tools[0].(map[string]any)["input_schema"] == nil {
		t.Fatalf("tools %v", tools)
	}
}

func TestAnthropicStructuredAnswer(t *testing.T) {
	f := newFake(t, reply(map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": "x", "name": anthropicAnswerTool, "input": map[string]any{"label": "b"}}},
	}))
	ep, d := endpoint(t, "anthropic", f.srv.URL, testKey)
	resp, err := d.Chat(context.Background(), ep, Request{Model: "m", Messages: []Message{{Role: "user", Text: "q"}}, Schema: classifySchema([]string{"a", "b"})})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := matchLabel(resp.Text, []string{"a", "b"}); !ok || got != "b" || len(resp.ToolCalls) != 0 {
		t.Fatalf("response %+v", resp)
	}
	choice := f.requests()[0].Body["tool_choice"].(map[string]any)
	if choice["type"] != "tool" || choice["name"] != anthropicAnswerTool {
		t.Fatalf("tool_choice %v", choice)
	}
}

func TestOllamaRequestShape(t *testing.T) {
	f := newFake(t, reply(map[string]any{
		"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "mail_flag", "arguments": map[string]any{"message": 9}}},
		}},
		"prompt_eval_count": 30, "eval_count": 4, "done": true,
	}))
	ep, d := endpoint(t, "ollama", f.srv.URL, "")
	resp, err := d.Chat(context.Background(), ep, Request{
		Model: "llama", System: "sys", MaxTokens: 64,
		Messages: []Message{{Role: "user", Text: "go"}, {Role: "tool", ToolName: "x", Text: "r"}},
		Schema:   classifySchema([]string{"a", "b"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID == "" || resp.InputTokens != 30 || resp.OutputTokens != 4 {
		t.Fatalf("response %+v", resp)
	}
	r := f.requests()[0]
	if r.Path != "/api/chat" || r.Body["stream"] != false || r.Header.Get("Authorization") != "" {
		t.Fatalf("%s %v %v", r.Path, r.Body, r.Header)
	}
	if opts := r.Body["options"].(map[string]any); opts["num_predict"] != 64.0 {
		t.Fatalf("options %v", opts)
	}
	if _, ok := r.Body["format"].(map[string]any); !ok {
		t.Fatalf("format %v", r.Body["format"])
	}
	if tool := r.Body["messages"].([]any)[2].(map[string]any); tool["role"] != "tool" || tool["tool_name"] != "x" {
		t.Fatalf("tool turn %v", tool)
	}
}

func TestModels(t *testing.T) {
	cases := []struct {
		kind, path string
		answer     any
	}{
		{"openai", "/v1/models", map[string]any{"data": []any{map[string]any{"id": "b"}, map[string]any{"id": "a"}}}},
		{"mistral", "/v1/models", map[string]any{"data": []any{map[string]any{"id": "b"}, map[string]any{"id": "a"}}}},
		{"anthropic", "/v1/models?limit=1000", map[string]any{"data": []any{map[string]any{"id": "b"}, map[string]any{"id": "a"}}}},
		{"ollama", "/api/tags", map[string]any{"models": []any{map[string]any{"name": "b"}, map[string]any{"name": "a"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			f := newFake(t, reply(tc.answer))
			base := f.srv.URL + "/v1"
			if tc.kind == "ollama" {
				base = f.srv.URL
			}
			ep, d := endpoint(t, tc.kind, base, testKey)
			got, err := d.Models(context.Background(), ep)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != "a,b" {
				t.Fatalf("models %v", got)
			}
			if r := f.requests()[0]; r.Method != "GET" || r.Path != tc.path {
				t.Fatalf("%s %s", r.Method, r.Path)
			}
		})
	}
}

func TestErrorsNeverCarryTheKey(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "ollama", "mistral"} {
		t.Run(kind, func(t *testing.T) {
			f := newFake(t, func(recorded) (int, any) {
				return 401, map[string]any{"error": map[string]any{"message": "Incorrect API key provided: " + testKey}}
			})
			ep, d := endpoint(t, kind, f.srv.URL, testKey)
			_, err := d.Chat(context.Background(), ep, Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}})
			var perr *Error
			if err == nil || !errors.As(err, &perr) || perr.Status != 401 {
				t.Fatalf("error %v", err)
			}
			if strings.Contains(err.Error(), testKey) || !strings.Contains(err.Error(), "[key]") {
				t.Fatalf("error %q", err)
			}
		})
	}
}

func TestErrorShapes(t *testing.T) {
	cases := map[string]string{
		`{"error":{"message":"m1"}}`: "m1",
		`{"error":"m2"}`:             "m2",
		`{"message":"m3"}`:           "m3",
		`{"detail":"m4"}`:            "m4",
		`plain text`:                 "plain text",
		``:                           "no reason given",
	}
	for body, want := range cases {
		if got := errorText([]byte(body)); got != want {
			t.Errorf("%q: got %q, want %q", body, got, want)
		}
	}
}

func TestRedirectsAreRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.org/", http.StatusFound)
	}))
	defer srv.Close()
	ep, d := endpoint(t, "openai", srv.URL, testKey)
	_, err := d.Chat(context.Background(), ep, Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("error %v", err)
	}
}
