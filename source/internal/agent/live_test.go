// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// TestLiveAgent asks a real agent about one plugin, with the web. It's
// skipped unless TT_MINECRAFTD_LIVE_AGENT is set, as "<kind> <url> <model>",
// such as "openai http://127.0.0.1:11434/v1 qwen2.5:7b-instruct". Only the
// answer's form is checked, since what a model finds varies.
func TestLiveAgent(t *testing.T) {
	defer testid.Line(t, "df76c")
	spec := strings.Fields(os.Getenv("TT_MINECRAFTD_LIVE_AGENT"))
	if len(spec) != 3 {
		t.Skip("TT_MINECRAFTD_LIVE_AGENT isn't set")
	}
	cfg := config.Defaults()
	cfg.Agent, cfg.AgentURL, cfg.AgentModel = spec[0], spec[1], spec[2]
	a, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if api, ok := a.(API); ok {
		api.MaxTurns = 10
		a = api
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	q := Question{Minecraft: "1.21.4", Jars: []Jar{
		{Path: "plugins/Chunky.jar", About: "Chunky 1.4.28", SHA256: sum256, Source: "https://modrinth.com/plugin/chunky"},
	}}
	c, err := Suggest(ctx, a, q, cfg.AgentHosts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("updates %+v", c.Updates)
	t.Logf("current %+v", c.Current)
	t.Logf("refused %+v", c.Refused)
	if len(c.Updates)+len(c.Current)+len(c.Refused) == 0 {
		t.Error("the answer named no jars")
	}
}
