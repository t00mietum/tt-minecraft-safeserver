// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package geyser

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

// fakeAPI answers latest with a redirect, as GeyserMC's does, and serves the
// jar only at the address that names the version and build.
type fakeAPI struct {
	name string
	sum  string
	jar  []byte
}

func newAPI(t *testing.T, f *fakeAPI) API {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "tt-minecraftd") {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v2/geyser/versions/latest/builds/latest":
			http.Redirect(w, r, "/v2/geyser/versions/2.12.1/builds/1300", http.StatusFound)
		case "/v2/geyser/versions/2.12.1/builds/1300":
			fmt.Fprintf(w, `{"project_id":"geyser","version":"2.12.1","build":1300,"downloads":{"fabric":{"name":"Geyser-Fabric.jar","sha256":"%s"},"spigot":{"name":%q,"sha256":%q}}}`, strings.Repeat("0", 64), f.name, f.sum)
		case "/v2/geyser/versions/2.12.1/builds/1300/downloads/spigot":
			_, _ = w.Write(f.jar)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return API{Base: srv.URL + "/v2", Client: srv.Client()}
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestLatest(t *testing.T) {
	defer testid.Line(t, "df65x")
	jar := []byte("PK not really a jar")
	f := &fakeAPI{name: "Geyser-Spigot.jar", sum: strings.ToUpper(sum(jar)), jar: jar}
	api := newAPI(t, f)
	b, err := api.Latest(context.Background(), "geyser")
	if err != nil {
		t.Fatal(err)
	}
	want := Build{Project: "geyser", Version: "2.12.1", ID: 1300, Name: "Geyser-Spigot.jar", SHA256: sum(jar), URL: api.Base + "/geyser/versions/2.12.1/builds/1300/downloads/spigot"}
	if b != want {
		t.Errorf("got %+v\nwant %+v", b, want)
	}
	if b.Path() != "plugins/Geyser-Spigot.jar" || api.Source("floodgate") != api.Base+"/floodgate/versions/latest/builds/latest/downloads/spigot" {
		t.Errorf("path %q, source %q", b.Path(), api.Source("floodgate"))
	}
	if (API{}).Source("geyser") != "https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/spigot" {
		t.Error("default source")
	}

	for _, c := range []struct{ name, sum, want string }{
		{"../Geyser-Spigot.jar", sum(jar), "odd file name"},
		{"Floodgate-Spigot.jar", sum(jar), "odd file name"},
		{"Geyser-Spigot.jar", "", "has no SHA-256"},
	} {
		f.name, f.sum = c.name, c.sum
		if _, err := api.Latest(context.Background(), "geyser"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q %q: %v, want %q", c.name, c.sum, err, c.want)
		}
	}
	if _, err := api.Latest(context.Background(), "floodgate"); err == nil {
		t.Error("no error for a project the API doesn't have")
	}
}

func TestInstall(t *testing.T) {
	defer testid.Line(t, "df65y")
	jar := []byte("PK not really a jar")
	f := &fakeAPI{name: "Geyser-Spigot.jar", sum: sum(jar), jar: jar}
	api := newAPI(t, f)
	b, err := api.Latest(context.Background(), "geyser")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := api.Install(context.Background(), dir, b); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "plugins")); err != nil || fi.Mode().Perm() != 0o770 {
		t.Errorf("plugins/: %v %v", fi.Mode(), err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "plugins", "Geyser-Spigot.jar")); err != nil || string(got) != string(jar) {
		t.Errorf("jar %q, %v", got, err)
	}
	if has, err := Installed(dir); err != nil || !has["geyser"] || has["floodgate"] {
		t.Errorf("Installed: %v %v", has, err)
	}

	// A jar that doesn't match leaves the old one, and no temp file.
	f.jar = []byte("PK not really a jbr")
	if err := api.Install(context.Background(), dir, b); err == nil || !strings.Contains(err.Error(), "SHA-256 doesn't match") {
		t.Errorf("bad sum: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "plugins"))
	if err != nil || len(entries) != 1 {
		t.Errorf("left in plugins/: %v %v", entries, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plugins", "Geyser-Spigot.jar")); string(got) != string(jar) {
		t.Errorf("old jar now %q", got)
	}

	// plugins/ as a link isn't followed.
	f.jar = jar
	other, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(other, "plugins")); err != nil {
		t.Fatal(err)
	}
	if err := api.Install(context.Background(), other, b); err == nil {
		t.Error("no error with plugins/ a link")
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Errorf("wrote through the link: %v", entries)
	}
}
