// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/yottacore/shcl/source/go/v3"
)

// imageStep is a pull of the image's tag, to pin the build it points at now.
type imageStep struct {
	runtime string
	repo    string
	tag     string
	paper   string
	java    int
}

func (s imageStep) tagged() string { return s.repo + ":" + s.tag }

// splitImage takes name[:tag][@digest] apart.
func splitImage(s string) (repo, tag, digest string) {
	s, digest, _ = strings.Cut(s, "@")
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		return s[:i], s[i+1:], digest
	}
	return s, "", digest
}

// imageLookup asks Paper which Java the server's Minecraft version needs.
// The pull waits for the go-ahead, since it's what finds the new digest.
func (u *Update) imageLookup(ctx context.Context, paperVersion func() (string, error)) (*imageStep, error) {
	rt := u.Cfg.RunMode
	if rt != "podman" && rt != "docker" {
		if u.Parts.Image {
			fmt.Fprintf(u.Out, "%-7s image: %s mode runs no container\n", "SKIPPED", rt)
		}
		return nil, nil
	}
	repo, tag, digest := splitImage(u.Cfg.Image)
	switch {
	case tag == "" && digest != "":
		fmt.Fprintf(u.Out, "%-7s image %s has no tag to follow; give it one in %s, such as %s:25-jre\n", "SKIPPED", u.Cfg.Image, u.ConfigPath, repo)
		return nil, nil
	case tag == "":
		tag = "latest"
	}
	version, err := paperVersion()
	if err != nil {
		return nil, err
	}
	java, err := u.Paper.Java(ctx, version)
	if err != nil {
		err = fmt.Errorf("can't get the Java version Paper %s needs: %w", version, err)
		// With agent set, PaperMC being down doesn't stop the other parts,
		// so it doesn't stop them here either. Asked for alone, it fails.
		if u.agentOn() && u.parts() != (Parts{Image: true}) {
			fmt.Fprintf(u.Out, "%-7s image: %v, so it stays as it is\n", "SKIPPED", err)
			return nil, nil
		}
		return nil, err
	}
	return &imageStep{runtime: rt, repo: repo, tag: tag, paper: version, java: java}, nil
}

func (s imageStep) plan(configPath string) string {
	return fmt.Sprintf("Pull %s with %s, check that its Java is %d or newer, as Paper %s needs, and pin its digest in %s.", s.tagged(), s.runtime, s.java, s.paper, configPath)
}

func (u *Update) getImage(ctx context.Context, s imageStep) error {
	if u.DryRun {
		fmt.Fprintf(u.Out, "would pull %s, run java -version in it, and pin its digest in %s\n", s.tagged(), u.ConfigPath)
		return nil
	}
	if _, err := u.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{s.runtime, "pull", s.tagged()}}); err != nil {
		return fmt.Errorf("can't pull %s: %w", s.tagged(), err)
	}
	out, err := u.Run.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: []string{s.runtime, "image", "inspect", s.tagged()}})
	if err != nil {
		return err
	}
	digest, err := pickDigest(s.repo, out)
	if err != nil {
		return fmt.Errorf("%s: %w", s.tagged(), err)
	}
	pinned := s.tagged() + "@" + digest
	// Locked down like the server's container, and with no network.
	out, err = u.Run.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: []string{s.runtime, "run", "--rm", "--pull", "never",
		"--user", "65534:65534", "--network", "none", "--read-only", "--tmpfs", "/tmp",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		pinned, "sh", "-c", "java -version 2>&1"}})
	if err != nil {
		return fmt.Errorf("can't run java -version in %s: %w", pinned, err)
	}
	have, err := javaMajor(string(out))
	if err != nil {
		return fmt.Errorf("%s: %w", pinned, err)
	}
	if have < s.java {
		return fmt.Errorf("%s has Java %d, and Paper %s needs %d or newer; nothing changed. Point image in %s at a newer tag, such as %s:%d-jre", pinned, have, s.paper, s.java, u.ConfigPath, s.repo, s.java)
	}
	if pinned == u.Cfg.Image {
		fmt.Fprintf(u.Out, "%-7s image %s is the newest build of its tag, with Java %d\n", "OK", pinned, have)
		return nil
	}
	if err := pinImage(u.ConfigPath, pinned); err != nil {
		return fmt.Errorf("can't pin the image in %s: %w", u.ConfigPath, err)
	}
	u.Cfg.Image = pinned
	fmt.Fprintf(u.Out, "%-7s image %s pulled, with Java %d, and pinned in %s\n", "FIXED", pinned, have, u.ConfigPath)
	return nil
}

// pickDigest finds what the tag pointed at when it was pulled. Both runtimes
// list it in RepoDigests. Podman also lists the build for this machine, which
// it gives as Digest, and that one's left out, so the pin works on any arch.
func pickDigest(repo string, inspect []byte) (string, error) {
	var images []struct {
		RepoDigests []string
		Digest      string
	}
	if err := json.Unmarshal(inspect, &images); err != nil {
		return "", fmt.Errorf("can't read the image's details: %w", err)
	}
	if len(images) != 1 {
		return "", fmt.Errorf("%d images came back, not 1", len(images))
	}
	img := images[0]
	var found []string
	for _, rd := range img.RepoDigests {
		name, d, _ := strings.Cut(rd, "@")
		if shortName(name) == shortName(repo) && d != img.Digest && !slices.Contains(found, d) {
			found = append(found, d)
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) == 0 && img.Digest != "":
		return img.Digest, nil
	case len(found) == 0:
		return "", errors.New("no digest came back for it")
	}
	return "", fmt.Errorf("more than one digest came back for it, %s, so which one the tag points at isn't clear", strings.Join(found, ", "))
}

// shortName is a Docker Hub name the way Docker gives it back.
func shortName(name string) string {
	name = strings.TrimPrefix(name, "docker.io/")
	return strings.TrimPrefix(name, "library/")
}

var javaVersion = regexp.MustCompile(`version "([0-9]+)(?:\.([0-9]+))?`)

// javaMajor reads java -version, such as `openjdk version "25.0.1"`, or
// "1.8.0_392" for Java 8.
func javaMajor(text string) (int, error) {
	m := javaVersion.FindStringSubmatch(text)
	if m == nil {
		first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
		return 0, fmt.Errorf("java -version gave no version: %q", first)
	}
	n, _ := strconv.Atoi(m[1]) // the regexp gave digits
	if n == 1 && m[2] != "" {
		n, _ = strconv.Atoi(m[2])
	}
	return n, nil
}

// pinImage sets image in the config. The operator's other lines stay byte for
// byte, since a plain save would rewrite them in shcl's own form.
func pinImage(file, image string) error {
	doc, st, err := shcl.LoadFileKeepLines(file, shcl.Standard)
	fresh := st == shcl.FileNotFound
	switch {
	case err != nil:
		return err
	case st == shcl.FileUnreadable:
		return fmt.Errorf("can't read %s", file)
	}
	if s := doc.SetString("image", image); s != shcl.SetOk {
		return fmt.Errorf("can't set image: %s", s)
	}
	if _, err := doc.SaveFileKeepLines(file); err != nil {
		return err
	}
	if fresh {
		return os.Chmod(file, 0o644) // #nosec G302 -- readable by all, per design.md's Configuration model
	}
	return nil
}
