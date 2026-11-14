// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/agent"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// jarBytes is a jar whose plugin.yml says name and version.
func jarBytes(t *testing.T, name, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("plugin.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("name: " + name + "\nversion: " + version + "\nmain: x.Y\n")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// agentRig has Chunky 1.0 approved with no source, and a host at
// example.com with Chunky 1.1 and a page of hashes.
type agentRig struct {
	*rig
	asked   []agent.Question
	answer  agent.Answer
	err     error
	newJar  []byte
	page    string
	fetched []string
	// extra are more files the host serves, by path.
	extra map[string][]byte
}

func newAgentRig(t *testing.T, answers string) *agentRig {
	t.Helper()
	a := &agentRig{rig: newRig(t, answers, 244), newJar: jarBytes(t, "Chunky", "1.1")}
	if err := os.Mkdir(filepath.Join(a.dir, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	old := jarBytes(t, "Chunky", "1.0")
	if err := os.WriteFile(filepath.Join(a.dir, "plugins", "Chunky.jar"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	// Paper's build is approved too, and isn't the agent's to look up.
	if err := plugins.Approve(a.u.PluginsFile, []plugins.Info{{Jar: plugins.Jar{Name: "paper-26.3-244.jar"}, SHA256: sumOf("old")}, {Jar: plugins.Jar{Name: "plugins/Chunky.jar"}, SHA256: sumOfBytes(old)}}); err != nil {
		t.Fatal(err)
	}
	a.page = "Chunky 1.1 sha256 " + strings.ToUpper(sumOfBytes(a.newJar))
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.fetched = append(a.fetched, r.URL.Path)
		switch r.URL.Path {
		case "/Chunky-1.1.jar":
			_, _ = w.Write(a.newJar)
		case "/sums":
			_, _ = w.Write([]byte(a.page))
		default:
			if b, ok := a.extra[r.URL.Path]; ok {
				_, _ = w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	// The test server's certificate is for example.com, which agentHosts
	// has to name, so every name dials it.
	client := ts.Client()
	tr := client.Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, ts.Listener.Addr().String())
	}
	client.Transport = tr
	a.u.Fetch = download.Client{HTTP: client}
	a.u.Cfg.AgentHosts = []string{"example.com"}
	a.u.Parts = Parts{Plugins: true}
	a.answer = agent.Answer{Jars: []agent.Suggestion{{
		Jar: "plugins/Chunky.jar", Version: "1.1", URL: "https://example.com/Chunky-1.1.jar",
		SHA256: sumOfBytes(a.newJar), HashFrom: "https://example.com/sums", Notes: "No config changes.",
	}}}
	a.u.Agent = func(_ context.Context, q agent.Question) (agent.Answer, error) {
		a.asked = append(a.asked, q)
		return a.answer, a.err
	}
	return a
}

func sumOfBytes(b []byte) string { return sumOf(string(b)) }

func (a *agentRig) jar(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.dir, "plugins", "Chunky.jar"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// noTemps fails on a download left beside the jars.
func (a *agentRig) noTemps(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(a.dir, "plugins"))
	for _, e := range entries {
		if e.Name() != "Chunky.jar" {
			t.Errorf("left in plugins/: %s", e.Name())
		}
	}
}

// The agent's jar is checked, needs its own yes even with --yes, and goes on
// the list with no source. Each failed check leaves the old jar.
func TestAgentJars(t *testing.T) {
	defer testid.Line(t, "df76h")
	ctx := context.Background()

	a := newAgentRig(t, "y\n")
	a.u.Yes = true
	if err := a.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, a.out)
	}
	if len(a.asked) != 1 || len(a.asked[0].Jars) != 1 || a.asked[0].Jars[0].About != "Chunky 1.0" || a.asked[0].Minecraft != "26.3" || a.asked[0].Paper != "244" {
		t.Errorf("asked %+v", a.asked)
	}
	for _, want := range []string{"Update plugins/Chunky.jar from Chunky 1.0 to 1.1, which the update agent found on example.com", "No config changes.", "Use it in place of plugins/Chunky.jar? [y/n]", "FIXED   plugins/Chunky.jar approved"} {
		if !strings.Contains(a.out.String(), want) {
			t.Errorf("no %q:\n%s", want, a.out)
		}
	}
	if !bytes.Equal(a.jar(t), a.newJar) {
		t.Error("the new jar isn't in place")
	}
	list, _, err := plugins.ReadApproved(a.u.PluginsFile)
	if err != nil || len(list) != 2 || list[1].SHA256 != sumOfBytes(a.newJar) || list[1].Source != "" {
		t.Errorf("approved %+v %v", list, err)
	}
	a.noTemps(t)

	// --yes with no answer left is a no.
	a = newAgentRig(t, "")
	a.u.Yes = true
	old := a.jar(t)
	if err := a.u.Do(ctx); err != nil || !bytes.Equal(a.jar(t), old) || !strings.Contains(a.out.String(), "kept as it was") {
		t.Errorf("--yes alone: %v\n%s", err, a.out)
	}
	a.noTemps(t)

	failures := []struct {
		name, want string
		change     func(a *agentRig)
	}{
		{"hash not on its page", "the hash isn't on https://example.com/sums", func(a *agentRig) { a.page = "nothing here" }},
		{"hash doesn't match", "SHA-256 doesn't match", func(a *agentRig) {
			a.answer.Jars[0].SHA256 = sumOf("other")
			a.page = sumOf("other")
		}},
		{"another plugin", `the download says it's "Chunk", not "Chunky"`, func(a *agentRig) {
			a.newJar = jarBytes(t, "Chunk", "1.1")
			a.answer.Jars[0].SHA256 = sumOfBytes(a.newJar)
			a.page = a.answer.Jars[0].SHA256
		}},
	}
	for _, f := range failures {
		a = newAgentRig(t, "y\ny\n")
		f.change(a)
		old := a.jar(t)
		if err := a.u.Do(ctx); err != nil || !bytes.Equal(a.jar(t), old) || !strings.Contains(a.out.String(), "FAILED  plugins/Chunky.jar: ") || !strings.Contains(a.out.String(), f.want) {
			t.Errorf("%s: %v\n%s", f.name, err, a.out)
		}
		a.noTemps(t)
	}

	// SHA-512 works too, and a source with no hash is shown with a warning.
	a = newAgentRig(t, "y\ny\n")
	sum := sha512.Sum512(a.newJar)
	a.answer.Jars[0].SHA256, a.answer.Jars[0].SHA512 = "", hex.EncodeToString(sum[:])
	a.page = a.answer.Jars[0].SHA512
	if err := a.u.Do(ctx); err != nil || !bytes.Equal(a.jar(t), a.newJar) {
		t.Errorf("sha512: %v\n%s", err, a.out)
	}
	a = newAgentRig(t, "y\ny\n")
	a.answer.Jars[0].SHA256, a.answer.Jars[0].HashFrom = "", ""
	if err := a.u.Do(ctx); err != nil || !bytes.Equal(a.jar(t), a.newJar) || !strings.Contains(a.out.String(), "publishes no hash") {
		t.Errorf("no hash: %v\n%s", err, a.out)
	}

	a = newAgentRig(t, "y\n")
	a.u.Cfg.AgentHosts = []string{"modrinth.com"}
	if err := a.u.Do(ctx); err != nil || len(a.fetched) > 0 || !strings.Contains(a.out.String(), "host example.com isn't in agentHosts") {
		t.Errorf("other host: %v, fetched %q\n%s", err, a.fetched, a.out)
	}

	a = newAgentRig(t, "y\n")
	a.err = errors.New("no reply")
	if err := a.u.Do(ctx); err != nil || !strings.Contains(a.out.String(), "FAILED  the update agent: no reply") {
		t.Errorf("agent failed: %v\n%s", err, a.out)
	}

	a = newAgentRig(t, "")
	a.u.DryRun = true
	if err := a.u.Do(ctx); err != nil || len(a.fetched) > 0 || !strings.Contains(a.out.String(), "Not checked, since a dry run downloads nothing") {
		t.Errorf("dry run: %v, fetched %q\n%s", err, a.fetched, a.out)
	}

	a = newAgentRig(t, "y\n")
	a.u.Agent, a.u.AgentNote = nil, "not asked as root"
	if err := a.u.Do(ctx); err != nil || !strings.Contains(a.out.String(), "NOTE    not asked as root") {
		t.Errorf("as root: %v\n%s", err, a.out)
	}
}

// A failed known source goes to the agent rather than stopping update, and
// with no agent it still stops it.
func TestAgentAfterFailedLookup(t *testing.T) {
	defer testid.Line(t, "df76j")
	ctx := context.Background()
	r, g := pluginRig(t, "")
	g.err = errors.New("no network")
	var asked []agent.Question
	r.u.Agent = func(_ context.Context, q agent.Question) (agent.Answer, error) {
		asked = append(asked, q)
		return agent.Answer{}, nil
	}
	if err := r.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	var paths []string
	for _, j := range asked[0].Jars {
		paths = append(paths, j.Path)
	}
	if strings.Join(paths, " ") != "plugins/Geyser-Spigot.jar plugins/floodgate-spigot.jar plugins/Thing.jar" {
		t.Errorf("asked about %q", paths)
	}
	if !strings.Contains(r.out.String(), "warning: can't get Geyser: no network, so the update agent is asked") || !strings.Contains(r.out.String(), "plugins/Thing.jar: no answer from the update agent") {
		t.Errorf("output:\n%s", r.out)
	}
}

// The operator's run hands its answer to the root run, which uses only what
// both asked about with the same hash.
func TestAgentHandOff(t *testing.T) {
	defer testid.Line(t, "df76k")
	ctx := context.Background()
	a := newAgentRig(t, "")
	ans, err := a.u.AskAgent(ctx)
	if err != nil || len(ans.Asked) != 1 || len(ans.Answer.Jars) != 1 || !strings.Contains(a.out.String(), "Asking the update agent about 1 jar.") {
		t.Fatalf("asked: %+v %v\n%s", ans, err, a.out)
	}
	if strings.Contains(a.out.String(), "SKIPPED") {
		t.Errorf("the operator's run printed its lookups:\n%s", a.out)
	}
	file := filepath.Join(t.TempDir(), "answer.json")
	b, _ := json.Marshal(ans)
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadAgentAnswer(file)
	if err != nil {
		t.Fatal(err)
	}
	q := a.u.question([]plugins.Approved{{Name: "plugins/Chunky.jar"}})
	if got, err := back.Replay(ctx, q); err != nil || len(got.Jars) != 1 {
		t.Errorf("replay: %+v %v", got, err)
	}
	q.Jars[0].SHA256 = sumOf("changed since")
	if got, _ := back.Replay(ctx, q); len(got.Jars) != 0 {
		t.Errorf("a changed jar was answered: %+v", got)
	}
	back.Error = "no reply"
	if _, err := back.Replay(ctx, q); err == nil || err.Error() != "no reply" {
		t.Errorf("error: %v", err)
	}

	a = newAgentRig(t, "")
	a.err = errors.New("no reply")
	if ans, err := a.u.AskAgent(ctx); err != nil || ans.Error != "no reply" {
		t.Errorf("failed agent: %+v %v", ans, err)
	}
}

// paperJarBytes is a jar laid out like Paper's, with main as its Main-Class.
func paperJarBytes(t *testing.T, main, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range [][2]string{
		{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\nMain-Class: " + main + "\n"},
		{"version.json", `{"id": "` + version + `"}`},
	} {
		w, err := z.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// newPaperAgentRig has PaperMC failing, and the agent finding build 249 on
// example.com.
func newPaperAgentRig(t *testing.T, answers string) *agentRig {
	t.Helper()
	a := newAgentRig(t, answers)
	a.u.Parts = Parts{Paper: true}
	a.p.err = errors.New("no network")
	jar := paperJarBytes(t, "io.papermc.paperclip.Main", "26.3")
	a.extra = map[string][]byte{"/paper-26.3-249.jar": jar, "/builds": []byte(`[{"id":249,"sha256":"` + sumOfBytes(jar) + `"}]`)}
	a.answer = agent.Answer{Paper: &agent.PaperBuild{Version: "26.3", Build: 249, URL: "https://example.com/paper-26.3-249.jar", SHA256: sumOfBytes(jar), HashFrom: "https://example.com/builds"}}
	return a
}

func (a *agentRig) linked(t *testing.T) string {
	t.Helper()
	target, _ := os.Readlink(filepath.Join(a.dir, "paper.jar"))
	return target
}

// When PaperMC's lookup fails, the agent's Paper build is checked like a
// plugin's, and needs its own yes even with --yes. Each failed check stops
// the update with paper.jar as it was.
func TestAgentPaper(t *testing.T) {
	defer testid.Line(t, "df792")
	ctx := context.Background()
	kept := []string{"paper-26.3-240.jar", "paper-26.3-244.jar", "paper.jar", "plugins"}

	a := newPaperAgentRig(t, "y\n")
	a.u.Yes = true
	if err := a.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, a.out)
	}
	if len(a.asked) != 1 || a.asked[0].FindPaper != "26.3" || len(a.asked[0].Jars) != 0 || a.asked[0].MoveTo != "" {
		t.Errorf("asked %+v", a.asked)
	}
	for _, want := range []string{"warning: can't get Paper: no network, so the update agent's find is used", "from build 244 to build 249", "The update agent found it on example.com", "Use it, and point paper.jar at paper-26.3-249.jar? [y/n]", "FIXED   paper-26.3-249.jar approved", "older builds removed: paper-26.3-240.jar"} {
		if !strings.Contains(a.out.String(), want) {
			t.Errorf("no %q:\n%s", want, a.out)
		}
	}
	if b, err := os.ReadFile(filepath.Join(a.dir, "paper.jar")); err != nil || !bytes.Equal(b, a.extra["/paper-26.3-249.jar"]) || a.linked(t) != "paper-26.3-249.jar" {
		t.Errorf("paper.jar links to %q: %v", a.linked(t), err)
	}
	list, _, err := plugins.ReadApproved(a.u.PluginsFile)
	if err != nil || !slices.ContainsFunc(list, func(p plugins.Approved) bool {
		return p.Name == "paper-26.3-249.jar" && p.SHA256 == a.answer.Paper.SHA256
	}) {
		t.Errorf("approved %+v %v", list, err)
	}

	a = newPaperAgentRig(t, "")
	a.u.Yes = true
	if err := a.u.Do(ctx); err != nil || a.linked(t) != "paper-26.3-244.jar" || !strings.Contains(a.out.String(), "SKIPPED Paper: kept as it was") || !slices.Equal(a.files(), kept) {
		t.Errorf("--yes alone: %v %q\n%s", err, a.files(), a.out)
	}

	failures := []struct {
		name, want string
		change     func(a *agentRig)
	}{
		{"hash not on its page", "the hash isn't on https://example.com/builds", func(a *agentRig) { a.extra["/builds"] = []byte("nothing") }},
		{"hash doesn't match", "SHA-256 doesn't match", func(a *agentRig) {
			a.answer.Paper.SHA256 = sumOf("other")
			a.extra["/builds"] = []byte(sumOf("other"))
		}},
		{"not Paper", "isn't a Paper server jar", func(a *agentRig) {
			jar := paperJarBytes(t, "x.Main", "26.3")
			a.extra["/paper-26.3-249.jar"], a.answer.Paper.SHA256, a.extra["/builds"] = jar, sumOfBytes(jar), []byte(sumOfBytes(jar))
		}},
		{"other version", "Paper for Minecraft 26.4, not 26.3", func(a *agentRig) {
			jar := paperJarBytes(t, "io.papermc.paperclip.Main", "26.4")
			a.extra["/paper-26.3-249.jar"], a.answer.Paper.SHA256, a.extra["/builds"] = jar, sumOfBytes(jar), []byte(sumOfBytes(jar))
		}},
		{"other host", "can't get Paper: no network, and the update agent's Paper download host other.example isn't in agentHosts", func(a *agentRig) {
			a.answer.Paper.URL = "https://other.example/paper.jar"
		}},
		{"agent failed", "can't get Paper: no network", func(a *agentRig) { a.err = errors.New("no reply") }},
		{"no agent", "can't get Paper: no network", func(a *agentRig) { a.u.Agent = nil }},
		{"no answer", "the update agent didn't answer about Paper", func(a *agentRig) { a.answer.Paper = nil }},
	}
	for _, f := range failures {
		a = newPaperAgentRig(t, "y\ny\n")
		f.change(a)
		err := a.u.Do(ctx)
		if err == nil || !strings.Contains(err.Error(), f.want) || a.linked(t) != "paper-26.3-244.jar" || !slices.Equal(a.files(), kept) {
			t.Errorf("%s: %v %q\n%s", f.name, err, a.files(), a.out)
		}
	}

	a = newPaperAgentRig(t, "")
	a.answer.Paper.Build = 244
	if err := a.u.Do(ctx); err != nil || !strings.Contains(a.out.String(), "OK      Paper 26.3 build 244 is the newest stable build the update agent found") {
		t.Errorf("nothing newer: %v\n%s", err, a.out)
	}

	a = newPaperAgentRig(t, "")
	a.u.DryRun = true
	if err := a.u.Do(ctx); err != nil || len(a.fetched) > 0 || !strings.Contains(a.out.String(), "would check Paper's hash page, download paper-26.3-249.jar") {
		t.Errorf("dry run: %v, fetched %q\n%s", err, a.fetched, a.out)
	}
}

// Before --mc-version the agent says which approved plugins have a build for
// the new version. It's shown before the question, and changes nothing.
func TestAgentMoveReport(t *testing.T) {
	defer testid.Line(t, "df793")
	ctx := context.Background()
	report := "The update agent's report on the approved plugins for Minecraft 26.4. It changes nothing.\n  - plugins/Chunky.jar: has no build yet. waiting on 26.4\n"

	a := newAgentRig(t, "n\n")
	a.u.Parts, a.u.MCVersion = Parts{Paper: true}, "26.4"
	a.answer = agent.Answer{Move: []agent.MoveNote{{Jar: "plugins/Chunky.jar", Has: "no", Notes: "waiting on 26.4"}, {Jar: "plugins/Other.jar", Has: "yes"}}}
	err := a.u.Do(ctx)
	out := a.out.String()
	if err == nil || len(a.asked) != 1 || a.asked[0].MoveTo != "26.4" || len(a.asked[0].Plugins) != 1 || a.asked[0].Plugins[0].About != "Chunky 1.0" || a.asked[0].FindPaper != "" {
		t.Errorf("asked %+v: %v", a.asked, err)
	}
	if i, k := strings.Index(out, report), strings.Index(out, "Go ahead, and move to 26.4?"); i < 0 || k < i || strings.Contains(out, "Other.jar") {
		t.Errorf("report:\n%s", out)
	}
	if !bytes.Equal(a.jar(t), jarBytes(t, "Chunky", "1.0")) || a.linked(t) != "paper-26.3-244.jar" {
		t.Error("the report changed something")
	}

	// No move, nothing to ask.
	a = newAgentRig(t, "n\n")
	a.u.Parts = Parts{Paper: true}
	_ = a.u.Do(ctx)
	if len(a.asked) != 0 {
		t.Errorf("asked without a move: %+v", a.asked)
	}

	a = newAgentRig(t, "")
	a.u.Parts, a.u.MCVersion, a.u.DryRun = Parts{Paper: true}, "26.4", true
	a.answer = agent.Answer{Move: []agent.MoveNote{{Jar: "plugins/Chunky.jar", Has: "yes", Version: "1.2"}}}
	if err := a.u.Do(ctx); err != nil || !strings.Contains(a.out.String(), "  - plugins/Chunky.jar: has a build, 1.2\n") {
		t.Errorf("dry run: %v\n%s", err, a.out)
	}

	a = newAgentRig(t, "n\n")
	a.u.Parts, a.u.MCVersion = Parts{Paper: true}, "26.4"
	a.u.Agent, a.u.AgentNote = nil, "not asked as root"
	_ = a.u.Do(ctx)
	if !strings.Contains(a.out.String(), "NOTE    not asked as root") || strings.Contains(a.out.String(), "report") {
		t.Errorf("as root:\n%s", a.out)
	}
}

// Paper and the move report go through the hand-off too, and the root run
// uses them only for the same version, and the same plugin hashes.
func TestAgentHandOffPaperAndMove(t *testing.T) {
	defer testid.Line(t, "df794")
	ctx := context.Background()
	a := newPaperAgentRig(t, "")
	a.u.MCVersion = "26.4"
	a.answer.Paper.Version = "26.4"
	a.answer.Move = []agent.MoveNote{{Jar: "plugins/Chunky.jar", Has: "yes"}}
	ans, err := a.u.AskAgent(ctx)
	if err != nil || ans.FindPaper != "26.4" || ans.MoveTo != "26.4" || len(ans.MoveAsked) != 1 || !strings.Contains(a.out.String(), "Asking the update agent about Paper for Minecraft 26.4, and which plugins have a build for Minecraft 26.4.") {
		t.Fatalf("asked: %+v %v\n%s", ans, err, a.out)
	}
	b, _ := json.Marshal(ans)
	file := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadAgentAnswer(file)
	if err != nil {
		t.Fatal(err)
	}
	q := agent.Question{FindPaper: "26.4", MoveTo: "26.4", Plugins: slices.Clone(ans.MoveAsked)}
	if got, err := back.Replay(ctx, q); err != nil || got.Paper == nil || got.Paper.Build != 249 || len(got.Move) != 1 {
		t.Errorf("replay: %+v %v", got, err)
	}
	q.FindPaper, q.MoveTo = "26.5", "26.5"
	if got, _ := back.Replay(ctx, q); got.Paper != nil || len(got.Move) != 0 {
		t.Errorf("another version was answered: %+v", got)
	}
	q.MoveTo = "26.4"
	q.Plugins[0].SHA256 = sumOf("changed since")
	if got, _ := back.Replay(ctx, q); len(got.Move) != 0 {
		t.Errorf("a changed jar was answered: %+v", got)
	}

	// The operator may not be able to read paper.jar. That's left to the
	// root run, and the plugins are still asked about.
	a = newAgentRig(t, "")
	a.u.Parts = Parts{}
	if err := os.Remove(filepath.Join(a.dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dir, "paper.jar"), []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ans, err := a.u.AskAgent(ctx); err != nil || len(ans.Asked) != 1 {
		t.Errorf("unreadable paper.jar: %+v %v", ans, err)
	}
}
