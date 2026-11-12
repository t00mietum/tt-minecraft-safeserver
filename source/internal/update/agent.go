// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/agent"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
)

// AgentAnswer is what the operator's run got from the agent, handed to the
// root run in a file. The root run checks it all again.
type AgentAnswer struct {
	Asked  []agent.Jar  `json:"asked"`
	Answer agent.Answer `json:"answer"`
	Error  string       `json:"error,omitempty"`
}

// maxAnswerFile caps the hand-off file. The agent's reply is capped at 1 MiB.
const maxAnswerFile = 4 << 20

// ReadAgentAnswer reads the file the operator's run wrote.
func ReadAgentAnswer(file string) (AgentAnswer, error) {
	var a AgentAnswer
	f, err := os.Open(file) // #nosec G304 -- named by the operator's own run of this tool
	if err != nil {
		return a, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxAnswerFile+1))
	if err != nil {
		return a, err
	}
	if len(b) > maxAnswerFile {
		return a, fmt.Errorf("%s: over %d MiB", file, maxAnswerFile>>20)
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return a, fmt.Errorf("%s: %w", file, err)
	}
	return a, nil
}

// Replay answers the root run with what the agent said, for the jars both
// runs asked about with the same hash.
func (a AgentAnswer) Replay(_ context.Context, q agent.Question) (agent.Answer, error) {
	if a.Error != "" {
		return agent.Answer{}, errors.New(a.Error)
	}
	both := func(name string) bool {
		i := slices.IndexFunc(q.Jars, func(j agent.Jar) bool { return j.Path == name })
		k := slices.IndexFunc(a.Asked, func(j agent.Jar) bool { return j.Path == name })
		return i >= 0 && k >= 0 && q.Jars[i].SHA256 == a.Asked[k].SHA256
	}
	var ans agent.Answer
	for _, s := range a.Answer.Jars {
		if both(s.Jar) {
			ans.Jars = append(ans.Jars, s)
		}
	}
	return ans, nil
}

// Live asks a, in the operator's run.
func Live(a agent.Asker) func(context.Context, agent.Question) (agent.Answer, error) {
	return func(ctx context.Context, q agent.Question) (agent.Answer, error) {
		reply, err := a.Ask(ctx, agent.Prompt(q))
		if err != nil {
			return agent.Answer{}, err
		}
		return agent.Parse(reply)
	}
}

// AskAgent asks Agent about the jars no known source covers, for the
// operator's run to hand to the root one. Its lookups print nothing, since
// the root run does them again and prints them then.
func (u *Update) AskAgent(ctx context.Context) (AgentAnswer, error) {
	out := u.Out
	u.Out = io.Discard
	_, asks, err := u.pluginLookup(ctx)
	u.Out = out
	if err != nil || len(asks) == 0 {
		return AgentAnswer{}, err
	}
	q := u.question(asks)
	fmt.Fprintf(u.Out, "Asking the update agent about %s. This can take a few minutes.\n", count(len(q.Jars), "jar"))
	ans, err := u.Agent(ctx, q)
	if ctx.Err() != nil {
		return AgentAnswer{}, ctx.Err()
	}
	a := AgentAnswer{Asked: q.Jars, Answer: ans}
	if err != nil {
		a.Error = err.Error()
	}
	return a, nil
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

// question is what the agent is told, per design.md's Update agent.
func (u *Update) question(asks []plugins.Approved) agent.Question {
	var q agent.Question
	if cur, err := paper.Current(u.Cfg.ServerDir); err == nil && cur != nil {
		q.Minecraft, q.Paper = cur.Version, strconv.Itoa(cur.ID)
	}
	for _, a := range asks {
		j := agent.Jar{Path: a.Name, SHA256: a.SHA256, Source: a.Source}
		file := filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(a.Name))
		if info, err := plugins.Describe(plugins.Jar{File: file}); err == nil {
			j.About, j.SHA256 = info.About, info.SHA256
		}
		q.Jars = append(q.Jars, j)
	}
	return q
}

// agentStep is a newer build of an approved jar that the agent found, not
// yet downloaded.
type agentStep struct {
	jar   plugins.Approved
	old   string
	found agent.Suggestion
}

// agentLookup asks the agent about asks, and reports what it said that
// won't go in the plan.
func (u *Update) agentLookup(ctx context.Context, asks []plugins.Approved) []agentStep {
	if len(asks) == 0 {
		return nil
	}
	if u.Agent == nil {
		fmt.Fprintf(u.Out, "%-7s %s\n", "NOTE", u.AgentNote)
		return nil
	}
	q := u.question(asks)
	ans, err := u.Agent(ctx, q)
	if err != nil {
		fmt.Fprintf(u.Out, "%-7s the update agent: %v\n", "FAILED", err)
		return nil
	}
	c := agent.Check(ans, q.Jars, u.Cfg.AgentHosts)
	answered := make(map[string]bool, len(q.Jars))
	for _, r := range c.Refused {
		answered[r.Jar] = true
		fmt.Fprintf(u.Out, "%-7s %s: the update agent's build is refused, since %s\n", "SKIPPED", r.Jar, r.Why)
	}
	for _, s := range c.Current {
		answered[s.Jar] = true
		fmt.Fprintf(u.Out, "%-7s %s: the update agent found nothing newer. %s\n", "OK", s.Jar, s.Notes)
	}
	var steps []agentStep
	for _, s := range c.Updates {
		answered[s.Jar] = true
		i := slices.IndexFunc(asks, func(a plugins.Approved) bool { return a.Name == s.Jar })
		file := filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(s.Jar))
		steps = append(steps, agentStep{jar: asks[i], old: oldAbout(file), found: s})
	}
	for _, j := range q.Jars {
		if !answered[j.Path] {
			fmt.Fprintf(u.Out, "%-7s %s: no answer from the update agent\n", "SKIPPED", j.Path)
		}
	}
	return steps
}

func (s agentStep) hash() (kind, sum string) {
	if s.found.SHA256 != "" {
		return "SHA-256", s.found.SHA256
	}
	if s.found.SHA512 != "" {
		return "SHA-512", s.found.SHA512
	}
	return "", ""
}

func hostOf(address string) string {
	if u, err := url.Parse(address); err == nil {
		return u.Hostname()
	}
	return address
}

func (s agentStep) plan(dryRun bool) string {
	f := s.found
	var b strings.Builder
	fmt.Fprintf(&b, "Update %s from %s to %s, which the update agent found on %s: ", s.jar.Name, s.old, f.Version, hostOf(f.URL))
	if kind, sum := s.hash(); kind != "" {
		fmt.Fprintf(&b, "check its %s %s is on %s, download it, check the hash and the plugin name, and ask before using it.", kind, sum, f.HashFrom)
	} else {
		b.WriteString("download it, check the plugin name, and ask before using it. Its source publishes no hash, so the download can't be checked against one.")
	}
	if f.Notes != "" {
		fmt.Fprintf(&b, " The agent's notes: %s", f.Notes)
	}
	if dryRun {
		b.WriteString(" Not checked, since a dry run downloads nothing.")
	}
	return b.String()
}

// maxHashPage caps a hash page. It's searched as is, not handed to the agent.
const maxHashPage = 4 << 20

func (u *Update) fetcher() download.Client {
	if u.Fetch.HTTP == nil {
		return download.Client{HTTP: agent.PublicClient(10 * time.Minute), UserAgent: u.Fetch.UserAgent}
	}
	return u.Fetch
}

// hashOnPage checks the hash the agent gave is on the page it named.
func (u *Update) hashOnPage(ctx context.Context, page, sum string) error {
	resp, err := u.fetcher().Get(ctx, page)
	if err != nil {
		return fmt.Errorf("can't read its hash page: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHashPage))
	if err != nil {
		return fmt.Errorf("can't read its hash page: %w", err)
	}
	if !strings.Contains(strings.ToLower(string(body)), sum) {
		return fmt.Errorf("the hash isn't on %s", page)
	}
	return nil
}

// openUnder opens the folder rel under serverDir without following links,
// since the server account may own the tree.
func openUnder(serverDir, rel string) (int, error) {
	fd, err := syscall.Open(serverDir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, &fs.PathError{Op: "open", Path: serverDir, Err: err}
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." {
			continue
		}
		next, err := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		_ = syscall.Close(fd) // a folder, opened to read
		if err != nil {
			return -1, &fs.PathError{Op: "open", Path: filepath.Join(serverDir, rel), Err: err}
		}
		fd = next
	}
	return fd, nil
}

// claimedName is the plugin name the jar named name in dirfd says it is.
func claimedName(dirfd int, name string) string {
	fd, err := syscall.Openat(dirfd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return ""
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	n, _ := plugins.Claims(f)
	return n
}

// getAgentJar checks a jar the agent found, then asks for its own yes, which
// --yes doesn't give. A failed check is reported, and the rest go on.
func (u *Update) getAgentJar(ctx context.Context, s agentStep) error {
	f := s.found
	if u.DryRun {
		fmt.Fprintf(u.Out, "would check %s's hash page, download %s over it, check it, and ask before using it\n", s.jar.Name, f.Version)
		return nil
	}
	failed := func(err error) error {
		fmt.Fprintf(u.Out, "%-7s %s: %v; not used\n", "FAILED", s.jar.Name, err)
		return nil
	}
	kind, sum := s.hash()
	if kind != "" {
		if err := u.hashOnPage(ctx, f.HashFrom, sum); err != nil {
			return failed(err)
		}
	}
	rel, base := path.Split(s.jar.Name)
	dirfd, err := openUnder(u.Cfg.ServerDir, rel)
	if err != nil {
		return failed(err)
	}
	defer syscall.Close(dirfd)
	dir := filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(rel))
	got, err := u.fetcher().Stage(ctx, dirfd, dir, base, f.URL, download.MaxJar)
	if err != nil {
		return failed(err)
	}
	newName, oldName := "", claimedName(dirfd, base)
	if file, err := got.Open(); err == nil {
		newName, _ = plugins.Claims(file)
		_ = file.Close() // only read from
	}
	switch {
	case kind == "SHA-256" && got.SHA256 != sum, kind == "SHA-512" && got.SHA512 != sum:
		err = fmt.Errorf("its %s doesn't match the one on its hash page", kind)
	case oldName == "":
		err = errors.New("the approved jar doesn't say which plugin it is, so the download can't be matched to it")
	case newName != oldName:
		err = fmt.Errorf("the download says it's %q, not %q", newName, oldName)
	}
	if err != nil {
		got.Drop()
		return failed(err)
	}
	fmt.Fprintf(u.Out, "\n%s %s, from %s\n  %d bytes, SHA-256 %s\n", newName, f.Version, f.URL, got.Size, got.SHA256)
	if kind == "" {
		fmt.Fprintln(u.Out, "  warning: its source publishes no hash, so only the plugin name was checked")
	}
	if !u.ask(fmt.Sprintf("Use it in place of %s? [y/n] ", s.jar.Name)) {
		got.Drop()
		fmt.Fprintf(u.Out, "%-7s %s: kept as it was\n", "SKIPPED", s.jar.Name)
		return nil
	}
	if err := got.Keep(); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "%-7s %s %s downloaded over %s\n", "FIXED", newName, f.Version, s.jar.Name)
	// No source: the agent's address isn't one update knows how to follow.
	if err := plugins.Approve(u.PluginsFile, []plugins.Info{{Jar: plugins.Jar{Name: s.jar.Name}, SHA256: got.SHA256, Size: got.Size}}); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	fmt.Fprintf(u.Out, "%-7s %s approved\n", "FIXED", s.jar.Name)
	return nil
}
