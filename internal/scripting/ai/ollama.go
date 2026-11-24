package ai

import (
	"context"
	"sort"
)

// ollama speaks Ollama's native chat API.
type ollama struct{}

type olMessage struct {
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	ToolCalls []olToolCall `json:"tool_calls,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
}

type olToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

func (ollama) headers(e *Endpoint) map[string]string {
	if e.Key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + e.Key}
}

func (o ollama) Chat(ctx context.Context, e *Endpoint, req Request) (Response, error) {
	var msgs []olMessage
	if req.System != "" {
		msgs = append(msgs, olMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			om := olMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				var call olToolCall
				call.Function.Name, call.Function.Arguments = tc.Name, tc.Args
				if call.Function.Arguments == nil {
					call.Function.Arguments = map[string]any{}
				}
				om.ToolCalls = append(om.ToolCalls, call)
			}
			msgs = append(msgs, om)
		case "tool":
			msgs = append(msgs, olMessage{Role: "tool", Content: m.Text, ToolName: m.ToolName})
		default:
			msgs = append(msgs, olMessage{Role: "user", Content: m.Text})
		}
	}
	body := map[string]any{"model": req.Model, "messages": msgs, "stream": false}
	if req.MaxTokens > 0 {
		body["options"] = map[string]any{"num_predict": req.MaxTokens}
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
		body["format"] = req.Schema
	}
	var out struct {
		Message struct {
			Content   string       `json:"content"`
			ToolCalls []olToolCall `json:"tool_calls"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := e.DoJSON(ctx, "POST", "/api/chat", o.headers(e), body, &out); err != nil {
		return Response{}, err
	}
	resp := Response{Text: out.Message.Content, InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount}
	for i, tc := range out.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == nil {
			args = map[string]any{}
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: callID(i), Name: tc.Function.Name, Args: args})
	}
	return resp, nil
}

func (o ollama) Models(ctx context.Context, e *Endpoint) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := e.DoJSON(ctx, "GET", "/api/tags", o.headers(e), nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			ids = append(ids, m.Name)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
