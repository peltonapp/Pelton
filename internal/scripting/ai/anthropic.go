package ai

import (
	"context"
	"encoding/json"
	"sort"
)

// AnthropicVersion is the API version Pelton sends to Anthropic.
const AnthropicVersion = "2023-06-01"

// anthropicAnswerTool is the tool a structured answer is forced through,
// since a tool's input follows its schema.
const anthropicAnswerTool = "answer"

// defaultAnthropicTokens is the output cap sent when a request names none;
// the Messages API requires one.
const defaultAnthropicTokens = 1024

// anthropic speaks Anthropic's Messages API.
type anthropic struct{}

type anBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

type anTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

func (anthropic) headers(e *Endpoint) map[string]string {
	h := map[string]string{"anthropic-version": AnthropicVersion}
	if e.Key != "" {
		h["x-api-key"] = e.Key
	}
	return h
}

func (a anthropic) Chat(ctx context.Context, e *Endpoint, req Request) (Response, error) {
	var msgs []anMessage
	add := func(role string, b anBlock) {
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content = append(msgs[n-1].Content, b)
			return
		}
		msgs = append(msgs, anMessage{Role: role, Content: []anBlock{b}})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			if m.Text != "" {
				add("assistant", anBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				input := tc.Args
				if input == nil {
					input = map[string]any{}
				}
				add("assistant", anBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
		case "tool":
			add("user", anBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text, IsError: m.ToolError})
		default:
			add("user", anBlock{Type: "text", Text: m.Text})
		}
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicTokens
	}
	body := map[string]any{"model": req.Model, "max_tokens": maxTokens, "messages": msgs, "stream": false}
	if req.System != "" {
		body["system"] = req.System
	}
	var tools []anTool
	for _, t := range req.Tools {
		tools = append(tools, anTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}
	if req.Schema != nil && len(req.Tools) == 0 {
		tools = append(tools, anTool{Name: anthropicAnswerTool, Description: "Give the answer.", InputSchema: req.Schema})
		body["tool_choice"] = map[string]any{"type": "tool", "name": anthropicAnswerTool}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	var out struct {
		Content []anBlock `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := e.DoJSON(ctx, "POST", "/messages", a.headers(e), body, &out); err != nil {
		return Response{}, err
	}
	resp := Response{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}
	for i, b := range out.Content {
		switch b.Type {
		case "text":
			resp.Text += b.Text
		case "tool_use":
			if req.Schema != nil && len(req.Tools) == 0 && b.Name == anthropicAnswerTool {
				data, err := json.Marshal(b.Input)
				if err == nil {
					resp.Text = string(data)
				}
				continue
			}
			id := b.ID
			if id == "" {
				id = callID(i)
			}
			args, _ := b.Input.(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: id, Name: b.Name, Args: args})
		}
	}
	return resp, nil
}

func (a anthropic) Models(ctx context.Context, e *Endpoint) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := e.DoJSON(ctx, "GET", "/models?limit=1000", a.headers(e), nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
