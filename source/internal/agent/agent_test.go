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
	defer testid.Line(t, "1a3jeem")
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
	defer testid.Line(t, "1a3jehr")
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

	// The question and the form as the prompt has them, quoted back before
	// or after the answer.
	q := Question{Minecraft: "26.3", Jars: []Jar{{Path: "plugins/Chunky.jar", About: "Chunky 1.4.28", SHA256: sum256, Source: "https://hangar.papermc.io/x"}}}
	prompt := Prompt(q)
	question := prompt[strings.LastIndex(prompt, "\n{")+1:]
	form := prompt[strings.Index(prompt, "\n{\"jars\"")+1:]
	form = form[:strings.IndexByte(form, '\n')]
	if !strings.Contains(question, `"minecraftVersion"`) || !strings.Contains(form, `"<jar, as given>"`) {
		t.Fatalf("prompt changed:\n%s", prompt)
	}
	quoted := map[string]string{
		"question first":  "You asked about:\n~~~json\n" + question + "~~~\nMy answer:\n~~~json\n" + one + "\n~~~\n",
		"form first":      "In the form " + form + " that is:\n" + one,
		"both first":      question + form + "\n" + one,
		"whole prompt":    "> " + prompt + "\n" + one,
		"question after":  one + "\nfor " + question,
		"form after":      one + "\nin the form " + form,
		"draft, then fix": strings.Replace(one, "1.4.40", "1.4.39", 1) + "\nSorry, the newest is:\n" + one,
	}
	for name, reply := range quoted {
		got, err := Parse(reply)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v %v", name, got, err)
		}
	}
	for name, reply := range map[string]string{"only the question": question, "only the form": form, "only both": question + form} {
		if got, err := Parse(reply); err == nil || !strings.Contains(err.Error(), "only repeats") {
			t.Errorf("%s: got %+v %v", name, got, err)
		}
	}
	// Extra fields in a real answer still don't make it the question.
	withVersion := `{"minecraftVersion": "26.3", ` + one[1:]
	if got, err := Parse(question + withVersion); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("answer naming the version: got %+v %v", got, err)
	}
}

func TestCheckHoldsSuggestionsToTheRules(t *testing.T) {
	defer testid.Line(t, "1a3jemw")
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
	defer testid.Line(t, "1a3jer0")
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
	defer testid.Line(t, "1a3jev4")
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
	defer testid.Line(t, "1a3jf7m")
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

// Paper's find is held to the version asked for, a SHA-256, and agentHosts.
// The move report keeps only the jars asked about. Both parts of the prompt
// show only when asked.
func TestPaperAndMoveReport(t *testing.T) {
	defer testid.Line(t, "1a3jr40")
	q := Question{Minecraft: "26.3", Paper: "249", FindPaper: "26.4", MoveTo: "26.4", Plugins: asked}
	p := Prompt(q)
	if !strings.Contains(p, `"paper": {"version"`) || !strings.Contains(p, `"moveReport": [{"jar"`) || !strings.Contains(p, `"findPaperFor": "26.4"`) {
		t.Errorf("prompt:\n%s", p)
	}
	if p := Prompt(Question{Minecraft: "26.3", Jars: asked}); strings.Contains(p, "findPaperFor") || strings.Contains(p, "moveReport") {
		t.Errorf("a jars-only prompt asks about Paper or a move:\n%s", p)
	}
	if !(Question{Minecraft: "26.3", MoveTo: "26.4"}).Empty() || (Question{FindPaper: "26.4"}).Empty() {
		t.Error("Empty")
	}

	reply := `Found it: {"paper": {"version": "26.4", "build": 12, "url": "https://fill-data.papermc.io/v1/objects/x/paper-26.4-12.jar", "sha256": "` + strings.ToUpper(sum256) + `", "hashFrom": "https://fill.papermc.io/v3/projects/paper/versions/26.4/builds", "notes": "first stable\u001b[2J"}, "moveReport": [{"jar": "plugins/Chunky.jar", "hasBuild": "Yes", "version": "1.5"}, {"jar": "plugins/Other.jar", "hasBuild": "yes"}]}`
	a, err := Parse(reply)
	if err != nil {
		t.Fatal(err)
	}
	// A question with jars has a jars key, and is no answer for Paper or the move.
	all := Prompt(Question{Minecraft: "26.3", Paper: "249", Jars: asked, FindPaper: "26.4", MoveTo: "26.4", Plugins: asked})
	if again, err := Parse("You asked:\n" + all + "\n" + reply); err != nil || !reflect.DeepEqual(again, a) {
		t.Errorf("after the prompt quoted back: %+v %v", again, err)
	}
	got, err := CheckPaper(a, "26.4", hosts)
	if err != nil || got.Build != 12 || got.SHA256 != sum256 || got.Notes != "first stable[2J" {
		t.Errorf("good build: %+v %v", got, err)
	}
	bad := map[string]func(p *PaperBuild){
		"no stable Paper":   func(p *PaperBuild) { p.URL = "" },
		"for Minecraft":     func(p *PaperBuild) { p.Version = "26.3" },
		"no build number":   func(p *PaperBuild) { p.Build = 0 },
		"has no SHA-256":    func(p *PaperBuild) { p.SHA256 = "" },
		"Paper download":    func(p *PaperBuild) { p.URL = "https://evil.example/paper.jar" },
		"Paper hash page":   func(p *PaperBuild) { p.HashFrom = "http://fill.papermc.io/x" },
		"didn't answer":     nil,
		"SHA-256, which Pa": func(p *PaperBuild) { p.SHA256 = sum512 },
	}
	for want, change := range bad {
		b := *a.Paper
		ans := Answer{Paper: &b}
		if change == nil {
			ans.Paper = nil
		} else {
			change(&b)
		}
		if got, err := CheckPaper(ans, "26.4", hosts); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %+v %v", want, got, err)
		}
	}

	notes := CheckMove(a, asked)
	want := []MoveNote{{Jar: "plugins/LuckPerms-Bukkit.jar", Has: HasUnknown}, {Jar: "plugins/Chunky.jar", Has: HasYes, Version: "1.5"}}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("report %+v", notes)
	}
	a.Move = []MoveNote{{Jar: "plugins/Chunky.jar", Has: "maybe"}, {Jar: "plugins/LuckPerms-Bukkit.jar", Has: "no", Notes: "not yet"}}
	if n := CheckMove(a, asked); n[0].Has != HasNo || n[0].Notes != "not yet" || n[1].Has != HasUnknown {
		t.Errorf("report %+v", n)
	}
	if _, err := Parse(`{"moveReport": []}`); err != nil {
		t.Errorf("a report alone: %v", err)
	}
}

// The vendor's own key goes only to the vendor's own API, not to a gateway or
// local server agentURL names. TT_MINECRAFTD_AGENT_KEY goes to any.
func TestVendorKeyStaysHome(t *testing.T) {
	defer testid.Line(t, "1a3k10c")
	env := map[string]string{"ANTHROPIC_API_KEY": "ak", "OPENAI_API_KEY": "ok"}
	old := getenv
	getenv = func(k string) string { return env[k] }
	t.Cleanup(func() { getenv = old })
	cfg := config.Defaults()
	cfg.AgentModel = "m"
	for _, c := range []struct{ kind, url, key string }{
		{"openai", "", "ok"},
		{"openai", "https://api.openai.com/v1", "ok"},
		{"openai", "https://API.OpenAI.com./v1", "ok"},
		{"anthropic", "https://api.anthropic.com/v1", "ak"},
		{"openai", "https://openrouter.ai/api/v1", ""},
		{"openai", "https://api.openai.com.evil.example/v1", ""},
		{"openai", "http://127.0.0.1:11434/v1", ""},
		{"openai", "https://api.anthropic.com/v1", ""},
		{"anthropic", "https://gateway.example/v1", ""},
	} {
		cfg.Agent, cfg.AgentURL = c.kind, c.url
		a, err := New(&cfg)
		if api, ok := a.(API); err != nil || !ok || api.Key != c.key {
			t.Errorf("%s at %q: %+v, %v", c.kind, c.url, a, err)
		}
	}
	env["TT_MINECRAFTD_AGENT_KEY"] = "tk"
	cfg.Agent, cfg.AgentURL = "openai", "https://openrouter.ai/api/v1"
	if a, err := New(&cfg); err != nil || a.(API).Key != "tk" {
		t.Errorf("tool key: %+v, %v", a, err)
	}
}
