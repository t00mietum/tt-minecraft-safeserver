// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package selfupdate

import (
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

// fakeGitHub serves releases as the list's JSON, with BASE standing for its
// own address, and files under /dl/.
func fakeGitHub(t *testing.T, releases string, files map[string]string) API {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "tt-minecraftd") {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		switch {
		case r.URL.Path == "/repo/releases":
			fmt.Fprint(w, strings.ReplaceAll(releases, "BASE", srv.URL))
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			body, ok := files[strings.TrimPrefix(r.URL.Path, "/dl/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return API{Base: srv.URL + "/repo", Client: srv.Client(), Arch: "arm64"}
}

func sumHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// release is one entry in the list. Each asset is served from /dl/<tag>/.
func release(tag string, draft, pre bool, assets ...string) string {
	var list []string
	for _, a := range assets {
		list = append(list, fmt.Sprintf(`{"name":%q,"browser_download_url":"BASE/dl/%s/%s","size":0}`, a, tag, a))
	}
	return fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[%s]}`, tag, draft, pre, strings.Join(list, ","))
}

// Newest skips drafts, takes pre-releases only from a pre-release, and
// reads the SHA-256 for this arch from SHA256SUMS.
func TestNewest(t *testing.T) {
	defer testid.Line(t, "df77g")
	ctx := context.Background()
	bin := AssetName("arm64")
	both := []string{AssetName("amd64"), bin, SumsName}
	sums := func(tag string) string {
		return fmt.Sprintf("%s  %s\n%s *%s\n", sumHex("x86"), AssetName("amd64"), strings.ToUpper(sumHex(tag)), bin)
	}
	list := "[" + strings.Join([]string{
		release("v2.0.0", true, false, both...),
		release("v1.0.0-beta.1", false, true, both...),
		release("v1.0.0-alpha.10", false, true, both...),
		release("v0.9.0", false, false, both...),
		release("nightly", false, false, both...),
	}, ",") + "]"
	files := map[string]string{}
	for _, tag := range []string{"v1.0.0-beta.1", "v1.0.0-alpha.10", "v0.9.0"} {
		files[tag+"/"+SumsName] = sums(tag)
	}
	api := fakeGitHub(t, list, files)

	r, found, err := api.Newest(ctx, "v1.0.0-alpha.1")
	if err != nil || !found || r.Tag != "v1.0.0-beta.1" || r.Asset != bin || r.SHA256 != sumHex("v1.0.0-beta.1") || !strings.HasSuffix(r.URL, "/dl/v1.0.0-beta.1/"+bin) {
		t.Errorf("from alpha.1: %+v %v %v", r, found, err)
	}
	if r, found, err := api.Newest(ctx, "v0.8.1"); err != nil || !found || r.Tag != "v0.9.0" {
		t.Errorf("from a release, a pre-release was taken: %+v %v %v", r, found, err)
	}
	for _, cur := range []string{"v1.0.0-beta.1", "v1.0.0", "v0.9.0"} {
		if r, found, err := api.Newest(ctx, cur); err != nil || found {
			t.Errorf("from %s: %+v %v %v", cur, r, found, err)
		}
	}
	if _, _, err := api.Newest(ctx, "dev"); err == nil {
		t.Error("a version that doesn't parse was compared")
	}
	if _, found, err := fakeGitHub(t, "[]", nil).Newest(ctx, "v1.0.0-alpha.1"); err != nil || found {
		t.Errorf("no releases: %v %v", found, err)
	}

	for name, c := range map[string]struct {
		assets []string
		sums   string
		want   string
	}{
		"no build":      {[]string{AssetName("amd64"), SumsName}, sums("v1.0.0"), "has no " + bin},
		"no sums":       {[]string{bin}, "", "can't be checked"},
		"sums lacks it": {[]string{bin, SumsName}, sumHex("a") + "  other\n", "no SHA-256 for " + bin},
		"bad sum":       {[]string{bin, SumsName}, "abc  " + bin + "\n", "no SHA-256 for " + bin},
	} {
		api := fakeGitHub(t, "["+release("v1.0.0", false, false, c.assets...)+"]", map[string]string{"v1.0.0/" + SumsName: c.sums})
		if _, _, err := api.Newest(ctx, "v1.0.0-alpha.1"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCompare(t *testing.T) {
	defer testid.Line(t, "df77h")
	// Each is older than the next, per semver.org's own example plus ours.
	order := []string{"v0.9.9", "v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.2", "v1.0.0-alpha.10", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.2.0", "v1.10.0", "v2.0.0"}
	for i := range order {
		for j := range order {
			a, okA := parseVersion(order[i])
			b, okB := parseVersion(order[j])
			if !okA || !okB {
				t.Fatalf("%s or %s doesn't parse", order[i], order[j])
			}
			if got, want := a.compare(b), cmpIdx(i, j); got != want {
				t.Errorf("%s vs %s: %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, bad := range []string{"", "1.0.0", "v1.0", "v1.0.0.0", "v01.0.0", "v1.0.0-", "v1.0.0-a..b", "vx.0.0", "v-1.0.0"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func cmpIdx(i, j int) int {
	switch {
	case i < j:
		return -1
	case i > j:
		return 1
	}
	return 0
}

// script is a stand-in binary that prints a --version line.
func script(tag string) string {
	return "#!/bin/sh\necho 'tt-minecraftd " + tag + " (build abc)'\n"
}

// Install swaps the new build in only once its size, SHA-256, and version
// match, keeps the old one's mode, and leaves no temp file either way.
func TestInstall(t *testing.T) {
	defer testid.Line(t, "df77j")
	ctx := context.Background()
	newBin := script("v1.0.0-alpha.2")
	files := map[string]string{"new": newBin, "arm": "\x7fELF not for here", "liar": script("v1.0.0-alpha.9")}
	api := fakeGitHub(t, "[]", files)
	base := strings.TrimSuffix(api.Base, "/repo")
	good := Release{Tag: "v1.0.0-alpha.2", Asset: "tt-minecraftd-linux-arm64", URL: base + "/dl/new", Size: int64(len(newBin)), SHA256: sumHex(newBin)}

	setup := func() (dir, exe string) {
		dir = t.TempDir()
		exe = filepath.Join(dir, "tt-minecraftd")
		if err := os.WriteFile(exe, []byte(script("v1.0.0-alpha.1")), 0o750); err != nil {
			t.Fatal(err)
		}
		return dir, exe
	}
	check := func(name, dir, exe, want string, mode os.FileMode) {
		t.Helper()
		b, _ := os.ReadFile(exe)
		st, err := os.Stat(exe)
		if err != nil || string(b) != want || st.Mode().Perm() != mode {
			t.Errorf("%s: left %q, mode %v, %v", name, b, st.Mode(), err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("%s: left %d files", name, len(entries))
		}
	}

	dir, exe := setup()
	if err := api.Install(ctx, exe, good); err != nil {
		t.Fatal(err)
	}
	check("good", dir, exe, newBin, 0o750)

	for name, c := range map[string]struct {
		r    Release
		want string
	}{
		"sum":     {Release{Tag: good.Tag, Asset: good.Asset, URL: good.URL, SHA256: sumHex("other")}, "SHA-256 doesn't match"},
		"size":    {Release{Tag: good.Tag, Asset: good.Asset, URL: good.URL, Size: good.Size - 1, SHA256: good.SHA256}, "the release said"},
		"arch":    {Release{Tag: good.Tag, Asset: good.Asset, URL: base + "/dl/arm", SHA256: sumHex(files["arm"])}, "doesn't run here"},
		"version": {Release{Tag: good.Tag, Asset: good.Asset, URL: base + "/dl/liar", SHA256: sumHex(files["liar"])}, "not v1.0.0-alpha.2"},
		"missing": {Release{Tag: good.Tag, Asset: good.Asset, URL: base + "/dl/gone", SHA256: good.SHA256}, "404"},
	} {
		dir, exe := setup()
		if err := api.Install(ctx, exe, c.r); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
		check(name, dir, exe, script("v1.0.0-alpha.1"), 0o750)
	}

	dir, exe = setup()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	if err := api.Install(ctx, link, good); err == nil {
		t.Error("a link was replaced, not the file it points at")
	}
}

func TestFromNix(t *testing.T) {
	defer testid.Line(t, "df77k")
	if !FromNix("/nix/store/abc-tt-minecraftd/bin/tt-minecraftd") {
		t.Error("a Nix store path isn't from Nix")
	}
	_, err := os.Stat("/etc/NIXOS")
	if got := FromNix("/usr/local/bin/tt-minecraftd"); got != (err == nil) {
		t.Errorf("on this host: %v, /etc/NIXOS: %v", got, err)
	}
}
