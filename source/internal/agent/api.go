// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// API is an agent behind a chat API: OpenAI's chat completions, which local
// servers such as Ollama speak too, or Anthropic's messages.
type API struct {
	Kind  string // openai or anthropic
	URL   string // up to and including /v1
	Model string
	Key   string // may be empty for a local server
	// Client is for the API itself, which may be on this host.
	Client *http.Client
	Fetch  *Fetcher
	// MaxTurns is 30 when zero. Each turn is one call to the API.
	MaxTurns int
}

const maxAPIReply = 8 << 20

// Ask holds the conversation, running the agent's fetches, until it answers
// in text.
func (a API) Ask(ctx context.Context, prompt string) (string, error) {
	if geteuid() == 0 {
		return "", errors.New("the update agent doesn't run as root")
	}
	if a.Fetch == nil {
		a.Fetch = &Fetcher{}
	}
	if a.Client == nil {
		a.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	turns := a.MaxTurns
	if turns == 0 {
		turns = 30
	}
	if a.Kind == "anthropic" {
		return a.anthropic(ctx, prompt, turns)
	}
	return a.openai(ctx, prompt, turns)
}

func (a API) openai(ctx context.Context, prompt string, turns int) (string, error) {
	type call struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	type message struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCalls  []call `json:"tool_calls,omitempty"`
		ToolCallID string `json:"tool_call_id,omitempty"`
	}
	tools := []map[string]any{{"type": "function", "function": map[string]any{
		"name": "fetch", "description": fetchAbout, "parameters": json.RawMessage(fetchSpec),
	}}}
	msgs := []message{{Role: "user", Content: prompt}}
	for range turns {
		var resp struct {
			Choices []struct {
				Message message `json:"message"`
			} `json:"choices"`
		}
		body := map[string]any{"model": a.Model, "messages": msgs, "tools": tools}
		if err := a.post(ctx, "/chat/completions", body, &resp); err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("the agent's server sent no answer")
		}
		m := resp.Choices[0].Message
		if len(m.ToolCalls) == 0 {
			return m.Content, nil
		}
		msgs = append(msgs, message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		for _, c := range m.ToolCalls {
			msgs = append(msgs, message{Role: "tool", ToolCallID: c.ID, Content: a.runTool(ctx, c.Function.Name, []byte(c.Function.Arguments))})
		}
	}
	return "", fmt.Errorf("the agent didn't answer within %d turns", turns)
}

func (a API) anthropic(ctx context.Context, prompt string, turns int) (string, error) {
	type block struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   string          `json:"content,omitempty"`
	}
	type message struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}
	tools := []map[string]any{{"name": "fetch", "description": fetchAbout, "input_schema": json.RawMessage(fetchSpec)}}
	msgs := []message{{Role: "user", Content: []block{{Type: "text", Text: prompt}}}}
	for range turns {
		var resp struct {
			Content    []block `json:"content"`
			StopReason string  `json:"stop_reason"`
		}
		body := map[string]any{"model": a.Model, "max_tokens": 8192, "messages": msgs, "tools": tools}
		if err := a.post(ctx, "/messages", body, &resp); err != nil {
			return "", err
		}
		var text strings.Builder
		var results []block
		for _, b := range resp.Content {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				results = append(results, block{Type: "tool_result", ToolUseID: b.ID, Content: a.runTool(ctx, b.Name, b.Input)})
			}
		}
		if len(results) == 0 {
			return text.String(), nil
		}
		msgs = append(msgs, message{Role: "assistant", Content: resp.Content}, message{Role: "user", Content: results})
	}
	return "", fmt.Errorf("the agent didn't answer within %d turns", turns)
}

func (a API) runTool(ctx context.Context, name string, args []byte) string {
	if name != "fetch" {
		return "refused: fetch is the only tool"
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.URL == "" {
		return `refused: fetch takes {"url": "https://..."}`
	}
	return a.Fetch.Fetch(ctx, in.URL)
}

func (a API) post(ctx context.Context, path string, body, into any) error {
	js, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL+path, bytes.NewReader(js))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tt-minecraftd")
	if a.Kind == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
		if a.Key != "" {
			req.Header.Set("x-api-key", a.Key)
		}
	} else if a.Key != "" {
		req.Header.Set("Authorization", "Bearer "+a.Key)
	}
	resp, err := a.Client.Do(req) // #nosec G107 G704 -- agentURL, from the root-owned config
	if err != nil {
		return fmt.Errorf("the agent's server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only, nothing to lose
	if resp.StatusCode != http.StatusOK {
		says, _ := io.ReadAll(io.LimitReader(resp.Body, 1024)) // the status is the error either way
		hint := ""
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			hint = ". The key comes from TT_MINECRAFTD_AGENT_KEY"
		}
		return fmt.Errorf("the agent's server said %s: %s%s", resp.Status, clean(string(says), 400), hint)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIReply)).Decode(into); err != nil {
		return fmt.Errorf("the agent's server sent something that isn't its JSON: %w", err)
	}
	return nil
}
