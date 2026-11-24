package ai

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// The ways an OpenAI-style server is asked for JSON.
const (
	// formatSchema sends the schema as response_format json_schema.
	formatSchema = "json_schema"
	// formatObject asks for any JSON object, response_format json_object.
	formatObject = "json_object"
)

// openAI speaks the chat completions protocol of OpenAI, which Mistral, vLLM
// and many local servers also speak.
type openAI struct {
	// tokens is the name of the output token cap field.
	tokens string
	// format is how to ask for JSON, "" to only ask in the prompt.
	format string
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type oaResponse struct {
	Choices []struct {
		Message struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []oaToolCall    `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func text(s string) *string { return &s }

func (o openAI) headers(e *Endpoint) map[string]string {
	if e.Key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + e.Key}
}

func (o openAI) Chat(ctx context.Context, e *Endpoint, req Request) (Response, error) {
	var msgs []oaMessage
	if req.System != "" {
		msgs = append(msgs, oaMessage{Role: "system", Content: text(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			om := oaMessage{Role: "assistant"}
			if m.Text != "" || len(m.ToolCalls) == 0 {
				om.Content = text(m.Text)
			}
			for _, tc := range m.ToolCalls {
				var call oaToolCall
				call.ID, call.Type = tc.ID, "function"
				call.Function.Name, call.Function.Arguments = tc.Name, argsJSON(tc.Args)
				om.ToolCalls = append(om.ToolCalls, call)
			}
			msgs = append(msgs, om)
		case "tool":
			msgs = append(msgs, oaMessage{Role: "tool", Content: text(m.Text), ToolCallID: m.ToolCallID})
		default:
			msgs = append(msgs, oaMessage{Role: "user", Content: text(m.Text)})
		}
	}
	body := map[string]any{"model": req.Model, "messages": msgs, "stream": false}
	if req.MaxTokens > 0 {
		body[o.tokens] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]oaTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			var ot oaTool
			ot.Type = "function"
			ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Parameters
			tools = append(tools, ot)
		}
		body["tools"] = tools
	}
	if req.Schema != nil {
		switch o.format {
		case formatSchema:
			body["response_format"] = map[string]any{
				"type":        "json_schema",
				"json_schema": map[string]any{"name": "answer", "schema": req.Schema, "strict": true},
			}
		case formatObject:
			body["response_format"] = map[string]any{"type": "json_object"}
		}
	}
	var out oaResponse
	if err := e.DoJSON(ctx, "POST", "/chat/completions", o.headers(e), body, &out); err != nil {
		return Response{}, err
	}
	if len(out.Choices) == 0 {
		return Response{}, &Error{Status: 200, Message: "the answer has no choices"}
	}
	msg := out.Choices[0].Message
	resp := Response{
		Text:         contentText(msg.Content),
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
	}
	for i, tc := range msg.ToolCalls {
		id := tc.ID
		if id == "" {
			id = callID(i)
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: id, Name: tc.Function.Name, Args: parseArgs(tc.Function.Arguments)})
	}
	return resp, nil
}

// contentText reads a message content that is a string, or a list of parts
// of which the text parts count.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func (o openAI) Models(ctx context.Context, e *Endpoint) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := e.DoJSON(ctx, "GET", "/models", o.headers(e), nil, &out); err != nil {
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
