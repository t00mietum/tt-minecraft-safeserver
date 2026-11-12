// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

var (
	sum256 = strings.Repeat("ab", 32)
	sum512 = strings.Repeat("cd", 64)
	hosts  = config.Defaults().AgentHosts
	asked  = []Jar{
		{Path: "plugins/LuckPerms-Bukkit.jar", About: "LuckPerms 5.4.150", SHA256: sum256},
		{Path: "plugins/Chunky.jar", About: "Chunky 1.4.28", SHA256: sum256},
	}
)

func TestPromptHoldsOnlyTheQuestion(t *testing.T) {
	defer testid.Line(t, "df75x")
	q := Question{Minecraft: "26.3", Paper: "26.3-249", Jars: asked}
	p := Prompt(q)
	if !strings.HasPrefix(p, instructions) {
		t.Fatal("prompt doesn't start with the instructions")
	}
	var got Question
	if err := json.Unmarshal([]byte(strings.TrimPrefix(p, instructions)), &got); err != nil {
		t.Fatalf("question after the instructions: %v", err)
	}
	if !reflect.DeepEqual(got, q) {
		t.Errorf("got %+v, want %+v", got, q)
	}
}

func TestParseFindsTheAnswer(t *testing.T) {
	defer testid.Line(t, "df75y")
	one := `{"jars": [{"jar": "plugins/Chunky.jar", "version": "1.4.40", "url": "https://cdn.modrinth.com/c.jar"}]}`
	want := Answer{Jars: []Suggestion{{Jar: "plugins/Chunky.jar", Version: "1.4.40", URL: "https://cdn.modrinth.com/c.jar"}}}
	good := map[string]string{
		"bare":         one,
		"fenced":       "~~~json\n" + one + "\n~~~\n",
		"in prose":     "Here's what I found {as asked}:\n\n" + one + "\n\nLet me know if {anything} else.",
		"extra fields": strings.Replace(one, `"version"`, `"confidence": 0.9, "version"`, 1),
	}
	for name, reply := range good {
		got, err := Parse(reply)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v", name, got)
		}
	}
	bad := map[string]string{
		"no json":   "I couldn't find anything.",
		"no jars":   `{"plugins": []}`,
		"cut short": `{"jars": [{"jar": "plugins/Chunky.jar", "url": "https://`,
		"jars text": `{"jars": "none"}`,
		"empty":     "",
	}
	for name, reply := range bad {
		if got, err := Parse(reply); err == nil {
			t.Errorf("%s: no error, got %+v", name, got)
		}
	}
}

func TestCheckHoldsSuggestionsToTheRules(t *testing.T) {
	defer testid.Line(t, "df75z")
	ok := Suggestion{
		Jar:      "plugins/LuckPerms-Bukkit.jar",
		Version:  "5.5.17",
		URL:      "https://cdn.modrinth.com/data/Vebnzrzj/versions/x/LuckPerms-Bukkit-5.5.17.jar",
		SHA512:   sum512,
		HashFrom: "https://api.modrinth.com/v2/project/luckperms/version",
	}
	with := func(change func(*Suggestion)) Suggestion {
		s := ok
		change(&s)
		return s
	}
	refused := map[string]struct {
		s   Suggestion
		why string
	}{
		"not asked":       {with(func(s *Suggestion) { s.Jar = "plugins/Evil.jar" }), "wasn't asked about"},
		"no version":      {with(func(s *Suggestion) { s.Version = "" }), "no version"},
		"http":            {with(func(s *Suggestion) { s.URL = "http://cdn.modrinth.com/x.jar" }), "isn't https"},
		"host not listed": {with(func(s *Suggestion) { s.URL = "https://evil.example/LuckPerms.jar" }), "evil.example isn't in agentHosts"},
		"listed suffix":   {with(func(s *Suggestion) { s.URL = "https://cdn.modrinth.com.evil.example/x.jar" }), "isn't in agentHosts"},
		"user in url":     {with(func(s *Suggestion) { s.URL = "https://a@github.com/x.jar" }), "user name"},
		"odd port":        {with(func(s *Suggestion) { s.URL = "https://github.com:8443/x.jar" }), "usual https port"},
		"not a url":       {with(func(s *Suggestion) { s.URL = "LuckPerms.jar" }), "isn't a web address"},
		"short sha256":    {with(func(s *Suggestion) { s.SHA256 = "abc" }), "sha256 isn't 64"},
		"short sha512":    {with(func(s *Suggestion) { s.SHA512 = sum256 }), "sha512 isn't 128"},
		"no hash page":    {with(func(s *Suggestion) { s.HashFrom = "" }), "hash page"},
		"hash page host":  {with(func(s *Suggestion) { s.HashFrom = "https://pastebin.com/raw/x" }), "hash page host pastebin.com"},
	}
	for name, tc := range refused {
		c := Check(Answer{Jars: []Suggestion{tc.s}}, asked, hosts)
		if len(c.Refused) != 1 || len(c.Updates) != 0 || !strings.Contains(c.Refused[0].Why, tc.why) {
			t.Errorf("%s: got %+v", name, c)
		}
	}

	noHash := with(func(s *Suggestion) { s.SHA512, s.HashFrom = "", "https://evil.example/" })
	upper := with(func(s *Suggestion) { s.SHA512 = strings.ToUpper(sum512) })
	current := Suggestion{Jar: "plugins/Chunky.jar", Notes: "already the newest"}
	twice := with(func(s *Suggestion) { s.Version = "9" })
	c := Check(Answer{Jars: []Suggestion{upper, current, twice}}, asked, hosts)
	if len(c.Updates) != 1 || c.Updates[0].SHA512 != sum512 || c.Updates[0].Version != ok.Version {
		t.Errorf("updates: %+v", c.Updates)
	}
	if !reflect.DeepEqual(c.Current, []Suggestion{current}) {
		t.Errorf("current: %+v", c.Current)
	}
	if len(c.Refused) != 1 || !strings.Contains(c.Refused[0].Why, "named twice") {
		t.Errorf("refused: %+v", c.Refused)
	}
	// A source with no hash is allowed, and its hash page means nothing.
	c = Check(Answer{Jars: []Suggestion{noHash}}, asked, hosts)
	if len(c.Updates) != 1 || c.Updates[0].HashFrom != "" {
		t.Errorf("no hash: %+v", c)
	}
}

func TestCheckCleansText(t *testing.T) {
	defer testid.Line(t, "df760")
	s := Suggestion{
		Jar:     "plugins/Chunky.jar",
		Version: "1.4\x1b[2J.40\u202e",
		URL:     "https://cdn.modrinth.com/c.jar",
		Notes:   "\x1b]0;owned\x07Config " + strings.Repeat("é", 400) + "\n",
	}
	c := Check(Answer{Jars: []Suggestion{s}}, asked, hosts)
	if len(c.Updates) != 1 {
		t.Fatalf("got %+v", c)
	}
	got := c.Updates[0]
	if got.Version != "1.4[2J.40" {
		t.Errorf("version %q", got.Version)
	}
	if strings.ContainsAny(got.Notes, "\x1b\x07\n") || !strings.HasPrefix(got.Notes, "]0;ownedConfig é") ||
		!strings.HasSuffix(got.Notes, "é...") || len(got.Notes) > 600+3 {
		t.Errorf("notes %q", got.Notes)
	}
}

func TestNewPicksTheKind(t *testing.T) {
	defer testid.Line(t, "df761")
	cfg := config.Defaults()
	for kind, says := range map[string]string{
		"none":      "no agent is set",
		"command":   "agentCommand isn't set",
		"openai":    "agentModel isn't set",
		"anthropic": "agentModel isn't set",
	} {
		cfg.Agent = kind
		if _, err := New(&cfg); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: got %v", kind, err)
		}
	}
	cfg.Agent, cfg.AgentCommand = "command", []string{"ask", "--json"}
	a, err := New(&cfg)
	if err != nil || !reflect.DeepEqual(a, Command{Argv: cfg.AgentCommand}) {
		t.Errorf("got %+v, %v", a, err)
	}

	env := map[string]string{"ANTHROPIC_API_KEY": "ak", "OPENAI_API_KEY": "ok"}
	old := getenv
	getenv = func(k string) string { return env[k] }
	t.Cleanup(func() { getenv = old })
	cfg.Agent, cfg.AgentModel = "anthropic", "sonnet-x"
	if a, err = New(&cfg); err != nil || !reflect.DeepEqual(a, API{Kind: "anthropic", URL: "https://api.anthropic.com/v1", Model: cfg.AgentModel, Key: "ak"}) {
		t.Errorf("anthropic: %+v, %v", a, err)
	}
	env["TT_MINECRAFTD_AGENT_KEY"] = "tk"
	cfg.Agent, cfg.AgentURL = "openai", "http://127.0.0.1:11434/v1"
	if a, err = New(&cfg); err != nil || !reflect.DeepEqual(a, API{Kind: "openai", URL: cfg.AgentURL, Model: cfg.AgentModel, Key: "tk"}) {
		t.Errorf("openai: %+v, %v", a, err)
	}
}

type askFunc func(ctx context.Context, prompt string) (string, error)

func (f askFunc) Ask(ctx context.Context, prompt string) (string, error) { return f(ctx, prompt) }

func TestSuggestAsksAndChecks(t *testing.T) {
	defer testid.Line(t, "df765")
	q := Question{Minecraft: "26.3", Jars: asked}
	var sent string
	reply := `{"jars": [{"jar": "plugins/Chunky.jar", "version": "1.4.40", "url": "https://evil.example/c.jar"}]}`
	c, err := Suggest(context.Background(), askFunc(func(_ context.Context, p string) (string, error) {
		sent = p
		return reply, nil
	}), q, hosts)
	if err != nil || sent != Prompt(q) || len(c.Refused) != 1 {
		t.Errorf("got %+v, %v", c, err)
	}
	failed := errors.New("no network")
	if _, err := Suggest(context.Background(), askFunc(func(context.Context, string) (string, error) {
		return "", failed
	}), q, hosts); !errors.Is(err, failed) {
		t.Errorf("failed ask: %v", err)
	}
	if _, err := Suggest(context.Background(), askFunc(func(context.Context, string) (string, error) {
		return "Sorry, I can't browse.", nil
	}), q, hosts); err == nil {
		t.Error("an answer with no JSON passed")
	}
}
