// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// fakeAPI answers each call with the next reply, after check has looked at
// the request body.
func fakeAPI(t *testing.T, check func(n int, r *http.Request, body map[string]any), replies ...string) *httptest.Server {
	t.Helper()
	n := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("call %d: %v", n, err)
		}
		check(n, r, body)
		if n >= len(replies) {
			n = len(replies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, replies[n])
		n++
	}))
	t.Cleanup(ts.Close)
	return ts
}

// last digs the newest message out of a request body.
func last(body map[string]any) map[string]any {
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		return nil
	}
	m, _ := msgs[len(msgs)-1].(map[string]any)
	return m
}

func TestOpenAIRunsFetches(t *testing.T) {
	defer testid.Line(t, "df769")
	page := pages(t)
	call := fmt.Sprintf(`{"choices": [{"message": {"role": "assistant", "content": null, "tool_calls": [
		{"id": "c1", "type": "function", "function": {"name": "fetch", "arguments": "{\"url\": \"%s/json\"}"}}]}}]}`, page.URL)
	done := `{"choices": [{"message": {"role": "assistant", "content": "{\"jars\": []}"}}]}`
	api := fakeAPI(t, func(n int, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k1" || body["model"] != "qwen3:32b" {
			t.Errorf("call %d: %s %q %v", n, r.URL.Path, r.Header.Get("Authorization"), body["model"])
		}
		if tools, _ := json.Marshal(body["tools"]); !strings.Contains(string(tools), `"name":"fetch"`) {
			t.Errorf("call %d tools: %s", n, tools)
		}
		m := last(body)
		switch n {
		case 0:
			if m["role"] != "user" || m["content"] != "q" {
				t.Errorf("first message: %v", m)
			}
		case 1:
			if m["role"] != "tool" || m["tool_call_id"] != "c1" || m["content"] != `{"version": "1.4.40"}` {
				t.Errorf("tool result: %v", m)
			}
		}
	}, call, done)
	a := API{Kind: "openai", URL: api.URL + "/v1", Model: "qwen3:32b", Key: "k1", Fetch: &Fetcher{Client: page.Client()}}
	if got, err := a.Ask(context.Background(), "q"); err != nil || got != `{"jars": []}` {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestAnthropicRunsFetches(t *testing.T) {
	defer testid.Line(t, "df76a")
	page := pages(t)
	call := fmt.Sprintf(`{"stop_reason": "tool_use", "content": [{"type": "text", "text": "Looking."},
		{"type": "tool_use", "id": "tu1", "name": "fetch", "input": {"url": "%s/json"}}]}`, page.URL)
	done := `{"stop_reason": "end_turn", "content": [{"type": "text", "text": "{\"jars\": []}"}]}`
	api := fakeAPI(t, func(n int, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k2" || r.Header.Get("anthropic-version") == "" ||
			r.Header.Get("Authorization") != "" || body["model"] != "claude-sonnet-4-5" {
			t.Errorf("call %d: %s %v", n, r.URL.Path, r.Header)
		}
		if n == 1 {
			js, _ := json.Marshal(last(body))
			if !strings.Contains(string(js), `"tool_use_id":"tu1"`) || !strings.Contains(string(js), `1.4.40`) {
				t.Errorf("tool result: %s", js)
			}
		}
	}, call, done)
	a := API{Kind: "anthropic", URL: api.URL + "/v1", Model: "claude-sonnet-4-5", Key: "k2", Fetch: &Fetcher{Client: page.Client()}}
	if got, err := a.Ask(context.Background(), "q"); err != nil || got != `{"jars": []}` {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestAPIFailures(t *testing.T) {
	defer testid.Line(t, "df76b")
	ctx := context.Background()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error": "bad key"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(denied.Close)
	_, err := API{Kind: "openai", URL: denied.URL, Model: "m", Key: "sk-secret"}.Ask(ctx, "q")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "TT_MINECRAFTD_AGENT_KEY") ||
		strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("401: %v", err)
	}

	var results []string
	loop := fakeAPI(t, func(_ int, _ *http.Request, body map[string]any) {
		if m := last(body); m["role"] == "tool" {
			results = append(results, fmt.Sprint(m["content"]))
		}
	}, `{"choices": [{"message": {"tool_calls": [{"id": "x", "type": "function", "function": {"name": "shell", "arguments": "{}"}}]}}]}`)
	_, err = API{Kind: "openai", URL: loop.URL, Model: "m", MaxTurns: 3}.Ask(ctx, "q")
	if err == nil || !strings.Contains(err.Error(), "within 3 turns") {
		t.Errorf("turns: %v", err)
	}
	if len(results) != 2 || results[0] != "refused: fetch is the only tool" {
		t.Errorf("tool results: %q", results)
	}

	junk := fakeAPI(t, func(int, *http.Request, map[string]any) {}, `<html>`)
	if _, err := (API{Kind: "anthropic", URL: junk.URL, Model: "m"}).Ask(ctx, "q"); err == nil ||
		!strings.Contains(err.Error(), "isn't its JSON") {
		t.Errorf("junk: %v", err)
	}

	old := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = old })
	if _, err := (API{Kind: "openai", URL: junk.URL, Model: "m"}).Ask(ctx, "q"); err == nil ||
		!strings.Contains(err.Error(), "doesn't run as root") {
		t.Errorf("root: %v", err)
	}
}
