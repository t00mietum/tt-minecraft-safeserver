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
	defer testid.Line(t, "1a3jfm4")
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
	defer testid.Line(t, "1a3jfq8")
	page := pages(t)
	call := fmt.Sprintf(`{"stop_reason": "tool_use", "content": [{"type": "text", "text": "Looking."},
		{"type": "tool_use", "id": "tu1", "name": "fetch", "input": {"url": "%s/json"}}]}`, page.URL)
	done := `{"stop_reason": "end_turn", "content": [{"type": "text", "text": "{\"jars\": []}"}]}`
	api := fakeAPI(t, func(n int, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k2" || r.Header.Get("anthropic-version") == "" ||
			r.Header.Get("Authorization") != "" || body["model"] != "sonnet-x" {
			t.Errorf("call %d: %s %v", n, r.URL.Path, r.Header)
		}
		if n == 1 {
			js, _ := json.Marshal(last(body))
			if !strings.Contains(string(js), `"tool_use_id":"tu1"`) || !strings.Contains(string(js), `1.4.40`) {
				t.Errorf("tool result: %s", js)
			}
		}
	}, call, done)
	a := API{Kind: "anthropic", URL: api.URL + "/v1", Model: "sonnet-x", Key: "k2", Fetch: &Fetcher{Client: page.Client()}}
	if got, err := a.Ask(context.Background(), "q"); err != nil || got != `{"jars": []}` {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestAPIFailures(t *testing.T) {
	defer testid.Line(t, "1a3jftc")
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

// rawStatus answers 404 with a reason phrase holding escape codes.
func rawStatus(w http.ResponseWriter, _ *http.Request) {
	c, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = buf.WriteString("HTTP/1.1 404 \x1b]0;owned\x07\x1b[2Jgone\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	_ = buf.Flush()
}

// The agent's server and a fetched page give their own reason phrase, so
// errors name the status by its code.
func TestStatusByCode(t *testing.T) {
	defer testid.Line(t, "1a3jv1r")
	ts := httptest.NewServer(http.HandlerFunc(rawStatus))
	t.Cleanup(ts.Close)
	_, err := API{Kind: "openai", URL: ts.URL, Model: "m"}.Ask(context.Background(), "q")
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "said 404 Not Found") {
		t.Errorf("API: %v", err)
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(rawStatus))
	t.Cleanup(tls.Close)
	got := (&Fetcher{Client: tls.Client()}).Fetch(context.Background(), tls.URL+"/page")
	if strings.ContainsAny(got, "\x1b\x07") || !strings.Contains(got, "HTTP 404 Not Found") {
		t.Errorf("fetch: %q", got)
	}
}

// A redirect could send the key and the prompt to another host, or down to
// plain http, so the API client doesn't follow one.
func TestAPIKeepsTheKeyOnRedirect(t *testing.T) {
	defer testid.Line(t, "1a3jw78")
	var elsewhere []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere = append(elsewhere, r.Method+" "+r.URL.Path+" x-api-key="+r.Header.Get("x-api-key")+" auth="+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices": [{"message": {"content": "{\"jars\": []}"}}], "content": [{"type": "text", "text": "{\"jars\": []}"}]}`)
	}))
	t.Cleanup(other.Close)
	// localhost against httptest's 127.0.0.1, so it's another host.
	away := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusFound} {
		moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, away+r.URL.Path, code)
		}))
		t.Cleanup(moved.Close)
		given := moved.Client()
		for _, kind := range []string{"anthropic", "openai"} {
			for _, client := range []*http.Client{nil, given} {
				api := API{Kind: kind, URL: moved.URL + "/v1", Model: "m", Key: "secret-" + kind, Client: client}
				_, err := api.Ask(context.Background(), "q")
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("said %d", code)) || !strings.Contains(err.Error(), "Redirects aren't followed") || strings.Contains(err.Error(), "secret") {
					t.Errorf("%d %s, own client %v: %v", code, kind, client != nil, err)
				}
			}
		}
		if given.CheckRedirect != nil {
			t.Errorf("%d: the client passed in was changed", code)
		}
	}
	if len(elsewhere) > 0 {
		t.Errorf("the other host got:\n%s", strings.Join(elsewhere, "\n"))
	}
}
