// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package paper

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// fill is a stand-in for the download API. builds maps a version to its
// build list's JSON, and the jar is served at /jar.
type fakeFill struct {
	srv    *httptest.Server
	builds map[string]string
	jar    []byte
}

func newFill(t *testing.T, versions string, builds map[string]string, jar []byte) (*fakeFill, API) {
	t.Helper()
	f := &fakeFill{builds: builds, jar: jar}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "tt-minecraftd") {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		switch {
		case r.URL.Path == "/paper":
			fmt.Fprint(w, versions)
		case r.URL.Path == "/jar":
			_, _ = w.Write(f.jar)
		case strings.HasPrefix(r.URL.Path, "/paper/versions/") && strings.HasSuffix(r.URL.Path, "/builds"):
			v := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/paper/versions/"), "/builds")
			b, ok := f.builds[v]
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, strings.ReplaceAll(b, "JARURL", f.srv.URL+"/jar"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f, API{Base: f.srv.URL + "/paper", Client: f.srv.Client()}
}

func build(id int, channel, name string, jar []byte) string {
	sum := sha256.Sum256(jar)
	return fmt.Sprintf(`{"id":%d,"channel":%q,"downloads":{"server:default":{"name":%q,"checksums":{"sha256":%q},"size":%d,"url":"JARURL"}}}`,
		id, channel, name, hex.EncodeToString(sum[:]), len(jar))
}

func TestNewestVersion(t *testing.T) {
	defer testid.Line(t, "df65p")
	jar := []byte("jar")
	_, api := newFill(t, `{"project":{"id":"paper"},"versions":{"1.9":["1.9.4"],"26.3":["26.3-rc-3","26.3"],"26.4":["26.4-rc-1"],"1.21":["1.21.11","1.21.10"],"27.1":["27.1"]}}`,
		map[string]string{
			"27.1":    "[" + build(3, "BETA", "paper-27.1-3.jar", jar) + "]",
			"26.3":    "[" + build(249, "STABLE", "paper-26.3-249.jar", jar) + "]",
			"1.21.11": "[" + build(130, "STABLE", "paper-1.21.11-130.jar", jar) + "]",
		}, jar)
	v, err := api.NewestVersion(context.Background())
	if err != nil || v != "26.3" {
		t.Errorf("got %q, %v; want 26.3, since 27.1 has only a beta and 26.4 only a release candidate", v, err)
	}
	vs := []string{"1.9.4", "1.21.11", "26.3", "1.21", "1.21.2"}
	sortNewest(vs)
	if strings.Join(vs, " ") != "26.3 1.21.11 1.21.2 1.21 1.9.4" {
		t.Errorf("sorted %q", vs)
	}
}

func TestJava(t *testing.T) {
	defer testid.Line(t, "df67p")
	f := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/paper/versions/26.3":
			fmt.Fprint(w, `{"version":{"id":"26.3","java":{"version":{"minimum":25},"flags":{"recommended":[]}}},"builds":[249]}`)
		case "/paper/versions/old":
			fmt.Fprint(w, `{"version":{"id":"old"},"builds":[1]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	api := API{Base: f.URL + "/paper", Client: f.Client(), UserAgent: "tt-minecraftd/test"}
	if n, err := api.Java(context.Background(), "26.3"); err != nil || n != 25 {
		t.Errorf("got %d, %v; want 25", n, err)
	}
	for version, want := range map[string]string{"old": "no Java version", "../x": "isn't a Minecraft version", "missing": "404"} {
		if _, err := api.Java(context.Background(), version); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", version, err, want)
		}
	}
}

func TestNewestStable(t *testing.T) {
	defer testid.Line(t, "df65q")
	jar := []byte("jar")
	_, api := newFill(t, `{}`, map[string]string{
		"26.3":   "[" + build(250, "BETA", "paper-26.3-250.jar", jar) + "," + build(249, "STABLE", "paper-26.3-249.jar", jar) + "," + build(248, "STABLE", "paper-26.3-248.jar", jar) + "]",
		"bad":    "[" + build(1, "STABLE", "../paper.jar", jar) + "]",
		"nosum":  `[{"id":1,"channel":"STABLE","downloads":{"server:default":{"name":"paper-x-1.jar","size":3,"url":"JARURL"}}}]`,
		"plain":  `[{"id":1,"channel":"STABLE","downloads":{"server:default":{"name":"paper-x-1.jar","checksums":{"sha256":"` + strings.Repeat("a", 64) + `"},"size":3,"url":"http://example.com/x.jar"}}}]`,
		"nostbl": "[" + build(1, "ALPHA", "paper-x-1.jar", jar) + "]",
	}, jar)
	b, err := api.NewestStable(context.Background(), "26.3")
	if err != nil || b.ID != 249 || b.Name != "paper-26.3-249.jar" || b.Size != 3 {
		t.Errorf("got %+v, %v", b, err)
	}
	for version, want := range map[string]string{"bad": "odd file name", "nosum": "no SHA-256", "plain": "isn't https", "nostbl": "no stable build", "../x": "isn't a Minecraft version", "missing": "404"} {
		if _, err := api.NewestStable(context.Background(), version); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", version, err, want)
		}
	}
}

func TestInstall(t *testing.T) {
	defer testid.Line(t, "df65r")
	jar := []byte("PK not really a jar")
	f, api := newFill(t, `{}`, map[string]string{"26.3": "[" + build(249, "STABLE", "paper-26.3-249.jar", jar) + "]"}, jar)
	dir := t.TempDir()
	// An old paper.jar link gets replaced, not followed.
	outside := filepath.Join(t.TempDir(), "outside.jar")
	if err := os.WriteFile(outside, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	b, err := api.NewestStable(context.Background(), "26.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Install(context.Background(), dir, b); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "paper.jar")); err != nil || target != "paper-26.3-249.jar" {
		t.Errorf("paper.jar -> %q, %v", target, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "paper-26.3-249.jar")); err != nil || string(got) != string(jar) {
		t.Errorf("jar %q, %v", got, err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "old" {
		t.Errorf("the old link's target changed: %q", got)
	}

	// A jar that doesn't match the API's sum is left out, with no temp file.
	f.jar = []byte("PK not really a jbr")
	b.Name = "paper-26.3-250.jar"
	if err := api.Install(context.Background(), dir, b); err == nil || !strings.Contains(err.Error(), "SHA-256 doesn't match") {
		t.Errorf("bad sum: %v", err)
	}
	f.jar = append(jar, 'x')
	if err := api.Install(context.Background(), dir, b); err == nil || !strings.Contains(err.Error(), "the API said") {
		t.Errorf("too long: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "paper-26.3-249.jar paper.jar" {
		t.Errorf("left in the tree: %q", names)
	}
}

// Current reads the version and build from the name paper.jar links to.
func TestCurrent(t *testing.T) {
	defer testid.Line(t, "df66x")
	dir := t.TempDir()
	if l, err := Current(dir); l != nil || err != nil {
		t.Errorf("no paper.jar: %+v, %v", l, err)
	}
	jar := filepath.Join(dir, "paper.jar")
	if err := os.WriteFile(jar, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Current(dir); err == nil || !strings.Contains(err.Error(), "not a link") {
		t.Errorf("plain paper.jar: %v", err)
	}
	for target, want := range map[string]string{
		"paper-26.3-244.jar":         "26.3 244 false",
		"paper-26.3-rc-3-5.jar":      "26.3-rc-3 5 true",
		"builds/paper-1.21.11-7.jar": "1.21.11 7 true",
		"paper-26.3.jar":             "error",
		"paper-26.3-0.jar":           "error",
		"server.jar":                 "error",
	} {
		if err := os.Remove(jar); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, jar); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "paper-26.3-244.jar"), []byte("jar"), 0o600); err != nil {
			t.Fatal(err)
		}
		l, err := Current(dir)
		got := "error"
		if err == nil {
			got = fmt.Sprintf("%s %d %t", l.Version, l.ID, l.Gone)
			if l.Target != target {
				t.Errorf("%s: target %q", target, l.Target)
			}
		}
		if got != want {
			t.Errorf("%s: got %q, %v; want %q", target, got, err, want)
		}
	}
}

// Prune leaves the builds it's told to keep, and anything that isn't a build.
func TestPrune(t *testing.T) {
	defer testid.Line(t, "df66y")
	dir := t.TempDir()
	for _, name := range []string{"paper-26.3-240.jar", "paper-26.3-244.jar", "paper-26.3-249.jar", "paper-1.21.11-7.jar", "paper-stuff.jar", "paper.jar.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("jar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("paper-26.3-249.jar", filepath.Join(dir, "paper-26.3-1.jar")); err != nil {
		t.Fatal(err)
	}
	gone, err := Prune(dir, "paper-26.3-249.jar", "paper-26.3-244.jar")
	if err != nil || strings.Join(gone, " ") != "paper-1.21.11-7.jar paper-26.3-240.jar" {
		t.Errorf("removed %q, %v", gone, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 5 {
		t.Errorf("left %q", left)
	}
}

// Older orders versions by their numbers, with a pre-release before its
// release, so update --mc-version can refuse a move back.
func TestOlder(t *testing.T) {
	defer testid.Line(t, "df78k")
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"1.21.11", "26.3", true},
		{"26.3", "1.21.11", false},
		{"1.21.4", "1.21.11", true},
		{"26.3", "26.3.1", true},
		{"26.3-rc-3", "26.3", true},
		{"26.3", "26.3-rc-3", false},
		{"26.3", "26.3", false},
		{"26.2", "26.3-pre-1", true},
	} {
		if got := Older(c.a, c.b); got != c.older {
			t.Errorf("Older(%q, %q) = %v", c.a, c.b, got)
		}
	}
	if IsVersion("26.3/../x") || !IsVersion("26.3-rc-3") {
		t.Error("IsVersion")
	}
}

// paperJar is a jar laid out like Paper's, with main as its Main-Class.
func paperJar(t *testing.T, main, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, text := range map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: " + main + "\r\n",
		"version.json":         `{"id": "` + version + `", "name": "` + version + `"}`,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Claims reads the version from a Paper jar only, and Point links paper.jar.
func TestClaimsAndPoint(t *testing.T) {
	defer testid.Line(t, "df791")
	dir := t.TempDir()
	for _, c := range []struct {
		jar  []byte
		want string
	}{
		{paperJar(t, "io.papermc.paperclip.Main", "26.4"), "26.4"},
		{paperJar(t, "net.minecraft.server.Main", "26.4"), ""},
		{paperJar(t, "io.papermc.paperclip.Main", "../26.4"), ""},
		{[]byte("not a jar"), ""},
	} {
		file := filepath.Join(dir, "x.jar")
		if err := os.WriteFile(file, c.jar, 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(file) // #nosec G304 -- the test's own file
		if err != nil {
			t.Fatal(err)
		}
		if got := Claims(f); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
		_ = f.Close()
	}
	if err := Point(dir, Name("26.4", 12)); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "paper.jar")); err != nil || target != "paper-26.4-12.jar" {
		t.Errorf("paper.jar: %q %v", target, err)
	}
}
