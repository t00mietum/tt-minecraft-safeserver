// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

const (
	oldPin = "docker.io/library/eclipse-temurin:25-jre@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	index  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	single = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// imageRig is newRig with --image named, a config that pins oldPin, and a
// runtime whose tag now points at index, with Java java.
func imageRig(t *testing.T, answers, java string) (*rig, *host.Fake) {
	t.Helper()
	r := newRig(t, answers, 244)
	r.u.Parts = Parts{Image: true}
	r.u.Cfg.RunMode = "podman"
	r.u.Cfg.Image = oldPin
	r.u.ConfigPath = filepath.Join(filepath.Dir(r.dir), "config.shcl")
	if err := os.WriteFile(r.u.ConfigPath, []byte("# mine\nrunMode: podman  # by hand\nimage: "+oldPin+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &host.Fake{Reply: func(c host.Cmd) ([]byte, error) {
		switch c.Args[1] {
		case "image":
			return []byte(`[{"Id":"x","Digest":"` + single + `","RepoDigests":["docker.io/library/eclipse-temurin@` + index + `","docker.io/library/eclipse-temurin@` + single + `"]}]`), nil
		case "run":
			return []byte(java), nil
		}
		return nil, nil
	}}
	r.u.Run = fake
	return r, fake
}

func (r *rig) config(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(r.u.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// --image pulls the tag, checks its Java against Paper's, and pins the build
// the tag points at, for every arch.
func TestImage(t *testing.T) {
	defer testid.Line(t, "df67q")
	ctx := context.Background()
	temurin := `openjdk version "25.0.1" 2025-10-21 LTS` + "\nOpenJDK Runtime Environment Temurin-25.0.1+8\n"
	newPin := "docker.io/library/eclipse-temurin:25-jre@" + index

	r, fake := imageRig(t, "y\n", temurin)
	if err := r.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	want := []string{
		"sudo podman pull docker.io/library/eclipse-temurin:25-jre",
		"sudo podman image inspect docker.io/library/eclipse-temurin:25-jre",
		"sudo podman run --rm --pull never --user 65534:65534 --network none --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges " + newPin + " sh -c 'java -version 2>&1'",
	}
	if !reflect.DeepEqual(fake.Lines(), want) || !reflect.DeepEqual(r.p.asked, []string{"java 26.3"}) {
		t.Errorf("ran %q, asked %q", fake.Lines(), r.p.asked)
	}
	if got := r.config(t); got != "# mine\nrunMode: podman  # by hand\nimage: \""+newPin+"\"\n" || r.u.Cfg.Image != newPin {
		t.Errorf("config:\n%s", got)
	}
	for _, want := range []string{"Java is 25 or newer, as Paper 26.3 needs", "Go ahead?", "FIXED   image " + newPin + " pulled, with Java 25"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("no %q:\n%s", want, r.out)
		}
	}

	// Pinned already: nothing written.
	r, _ = imageRig(t, "y\n", temurin)
	r.u.Cfg.Image = newPin
	if err := r.u.Do(ctx); err != nil || !strings.Contains(r.out.String(), "OK      image "+newPin+" is the newest") || strings.Contains(r.config(t), index) {
		t.Errorf("up to date: %v\n%s", err, r.out)
	}

	r, _ = imageRig(t, "y\n", `openjdk version "21.0.5" 2024-10-15 LTS`)
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "has Java 21, and Paper 26.3 needs 25") || strings.Contains(r.config(t), index) {
		t.Errorf("old Java: %v\n%s", err, r.config(t))
	}

	r, _ = imageRig(t, "y\n", "sh: java: not found")
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "gave no version") || strings.Contains(r.config(t), index) {
		t.Errorf("no Java: %v", err)
	}

	r, fake = imageRig(t, "n\n", temurin)
	if err := r.u.Do(ctx); err == nil || len(fake.Ran) > 0 {
		t.Errorf("a no went on: %v, ran %q", err, fake.Lines())
	}

	r, fake = imageRig(t, "", temurin)
	r.u.DryRun = true
	if err := r.u.Do(ctx); err != nil || len(fake.Ran) > 0 || !strings.Contains(r.out.String(), "would pull docker.io/library/eclipse-temurin:25-jre") {
		t.Errorf("dry run: %v, ran %q\n%s", err, fake.Lines(), r.out)
	}

	r, fake = imageRig(t, "y\n", temurin)
	r.u.Cfg.Image = "docker.io/library/eclipse-temurin@" + index
	if err := r.u.Do(ctx); err != nil || len(fake.Ran) > 0 || !strings.Contains(r.out.String(), "has no tag to follow") {
		t.Errorf("digest only: %v\n%s", err, r.out)
	}

	r, fake = imageRig(t, "y\n", temurin)
	r.u.Cfg.RunMode = "native"
	if err := r.u.Do(ctx); err != nil || len(fake.Ran) > 0 || len(r.p.asked) > 0 || !strings.Contains(r.out.String(), "native mode runs no container") {
		t.Errorf("native: %v\n%s", err, r.out)
	}

	// No config file yet: one gets made, readable by all.
	r, _ = imageRig(t, "y\n", temurin)
	if err := os.Remove(r.u.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Do(ctx); err != nil || !strings.Contains(r.config(t), newPin) {
		t.Errorf("new config: %v\n%s", err, r.out)
	}
	if fi, err := os.Stat(r.u.ConfigPath); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("new config's mode: %v %v", fi.Mode(), err)
	}
	if c, warns, err := config.Load(r.u.ConfigPath, nil); err != nil || len(warns) > 0 || c.Image != newPin {
		t.Errorf("reads back as %q, %v %v", c.Image, warns, err)
	}
}

func TestPickDigest(t *testing.T) {
	defer testid.Line(t, "df67r")
	for _, c := range []struct {
		repo, inspect, want string
	}{
		// Docker gives the short name, and no Digest.
		{"docker.io/library/eclipse-temurin", `[{"RepoDigests":["eclipse-temurin@` + index + `"]}]`, index},
		{"docker.io/library/eclipse-temurin", `[{"Digest":"` + single + `","RepoDigests":["docker.io/library/eclipse-temurin@` + single + `","docker.io/library/eclipse-temurin@` + index + `"]}]`, index},
		// A single-arch image has only its own digest.
		{"quay.io/x/jre", `[{"Digest":"` + single + `","RepoDigests":["quay.io/x/jre@` + single + `"]}]`, single},
		{"quay.io/x/jre", `[{"RepoDigests":["quay.io/other/jre@` + index + `"]}]`, "no digest"},
		{"quay.io/x/jre", `[{"RepoDigests":["quay.io/x/jre@` + index + `","quay.io/x/jre@` + single + `"]}]`, "more than one digest"},
		{"quay.io/x/jre", `[]`, "0 images"},
		{"quay.io/x/jre", `nope`, "can't read"},
	} {
		got, err := pickDigest(c.repo, []byte(c.inspect))
		digest := strings.HasPrefix(c.want, "sha256:")
		if (digest && (err != nil || got != c.want)) || (!digest && (err == nil || !strings.Contains(err.Error(), c.want))) {
			t.Errorf("%s %s: got %q, %v; want %q", c.repo, c.inspect, got, err, c.want)
		}
	}
}

func TestSplitImageJavaMajor(t *testing.T) {
	defer testid.Line(t, "df67s")
	for in, want := range map[string][3]string{
		"docker.io/library/eclipse-temurin:25-jre@sha256:ab": {"docker.io/library/eclipse-temurin", "25-jre", "sha256:ab"},
		"localhost:5000/jre:21":                              {"localhost:5000/jre", "21", ""},
		"localhost:5000/jre":                                 {"localhost:5000/jre", "", ""},
		"docker.io/library/eclipse-temurin@sha256:ab":        {"docker.io/library/eclipse-temurin", "", "sha256:ab"},
	} {
		if repo, tag, digest := splitImage(in); [3]string{repo, tag, digest} != want {
			t.Errorf("%s: got %q %q %q", in, repo, tag, digest)
		}
	}
	for in, want := range map[string]int{
		`openjdk version "25.0.1" 2025-10-21 LTS`:                                      25,
		`openjdk version "1.8.0_392"`:                                                  8,
		`java version "21" 2023-09-19`:                                                 21,
		"Picked up JAVA_TOOL_OPTIONS: -Xss1m\n" + `openjdk version "26-ea" 2026-03-17`: 26,
	} {
		if n, err := javaMajor(in); err != nil || n != want {
			t.Errorf("%q: got %d, %v; want %d", in, n, err, want)
		}
	}
}
