// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package status gathers what status and watch show. It only looks, and needs
// no sudo. See design.md, status and watch.
package status

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
)

// BootUnit is the unit that runs start --foreground at boot.
const BootUnit = "tt-minecraftd.service"

// logLines is how much of latest.log status shows.
const logLines = 10

// Probe takes the readings. One Probe can take many, as watch does, and keeps
// the plugin sums between them.
type Probe struct {
	Run         host.Runner
	Cfg         *config.Config
	PluginsFile string
	Now         func() time.Time
	// ReadFile reads /proc and /sys, for the container's memory.
	ReadFile func(string) ([]byte, error)
	Ping     func(ctx context.Context, addr string) (server.PingReply, error)
	sums     plugins.Sums
}

// NewProbe gives a Probe on the real host.
func NewProbe(r host.Runner, c *config.Config, pluginsFile string) *Probe {
	return &Probe{Run: r, Cfg: c, PluginsFile: pluginsFile, Now: time.Now, ReadFile: os.ReadFile, Ping: server.Ping}
}

// Line is one labeled line. A Line with no label goes on under the one above.
type Line struct {
	Label string
	Text  string
}

// Report is the readings, in sections.
type Report [][]Line

// running is what the server section found, for the sections after it.
type running struct {
	up       bool
	memory   int64 // bytes, or -1 when unknown
	memError string
}

// Take reads everything once. A part that can't be read says why, and the
// rest still shows.
func (p *Probe) Take(ctx context.Context) Report {
	srv, run := p.server(ctx)
	srv = append(srv, p.version(ctx, run)...)
	srv = append(srv, p.memory(run))
	return Report{
		srv,
		p.ports(ctx),
		{p.whitelist(), p.ops(), p.plugins()},
		append(p.trees(), p.boot(ctx)),
		p.log(),
	}
}

func (p *Probe) server(ctx context.Context) ([]Line, running) {
	if p.Cfg.RunMode == "native" {
		return p.nativeServer(ctx)
	}
	return p.dockerServer(ctx)
}

func (p *Probe) dockerServer(ctx context.Context) ([]Line, running) {
	run := running{memory: -1}
	state, err := launch.ContainerState(ctx, p.Run)
	if err != nil {
		return []Line{{"Server", "can't ask docker: " + err.Error()}}, run
	}
	switch state {
	case "":
		return p.notRunning(ctx, "not running in docker mode"), run
	case "running":
	default:
		return p.notRunning(ctx, "not running in docker mode; the container is "+state), run
	}
	run.up = true
	out, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"docker", "inspect", "--format", "{{.State.StartedAt}} {{.State.Pid}} {{.Config.User}}", launch.Container}})
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 2 {
		run.memError = "unknown"
		return []Line{{"Server", "running in docker mode"}}, run
	}
	text := "running in docker mode"
	if len(fields) > 2 {
		uid, _, _ := strings.Cut(fields[2], ":")
		text += ", as " + userName(uid)
	}
	if started, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
		text += ", for " + since(p.Now().Sub(started))
	}
	run.memory, run.memError = p.cgroupMemory(fields[1])
	return []Line{{"Server", text}}, run
}

// cgroupMemory reads memory.current from the cgroup the process is in.
func (p *Probe) cgroupMemory(pid string) (int64, string) {
	cg, err := p.ReadFile("/proc/" + pid + "/cgroup")
	if err != nil {
		return -1, "unknown"
	}
	for _, l := range strings.Split(string(cg), "\n") {
		// cgroup v2 has the one line, 0::/path.
		path, ok := strings.CutPrefix(l, "0::")
		if !ok {
			continue
		}
		cur, err := p.ReadFile(filepath.Join("/sys/fs/cgroup", filepath.Clean("/"+path), "memory.current"))
		if err != nil {
			break
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(string(cur)), 10, 64); err == nil {
			return n, ""
		}
	}
	return -1, "unknown, without cgroup v2"
}

func (p *Probe) nativeServer(ctx context.Context) ([]Line, running) {
	run := running{memory: -1, memError: "unknown"}
	out, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "show", "--timestamp=unix", "--property=ActiveState,ActiveEnterTimestamp,MemoryCurrent,User", launch.Unit}})
	if err != nil {
		return []Line{{"Server", "can't ask systemd: " + err.Error()}}, run
	}
	props := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			props[k] = strings.TrimSpace(v)
		}
	}
	if !launch.UnitUp(props["ActiveState"]) {
		return p.notRunning(ctx, "not running in native mode"), run
	}
	run.up = true
	text := "running in native mode"
	if s := props["ActiveState"]; s != "active" {
		text += " (" + s + ")"
	}
	if u := props["User"]; u != "" {
		text += ", as " + u
	}
	if ts, ok := strings.CutPrefix(props["ActiveEnterTimestamp"], "@"); ok {
		if secs, err := strconv.ParseInt(ts, 10, 64); err == nil && secs > 0 {
			text += ", for " + since(p.Now().Sub(time.Unix(secs, 0)))
		}
	}
	if n, err := strconv.ParseInt(props["MemoryCurrent"], 10, 64); err == nil {
		run.memory, run.memError = n, ""
	}
	return []Line{{"Server", text}}, run
}

// notRunning also names any other Paper server, such as one the old scripts
// started, since start refuses to run beside it.
func (p *Probe) notRunning(ctx context.Context, text string) []Line {
	lines := []Line{{"Server", text}}
	ps, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"ps", "-eo", "user=,pid=,args="}})
	if err != nil {
		return lines
	}
	for _, found := range server.RunningPaper(ps) {
		lines = append(lines, Line{"", "another Paper server is running: " + found})
	}
	return lines
}

func (p *Probe) version(ctx context.Context, run running) []Line {
	jar := "no paper.jar"
	if real, err := filepath.EvalSymlinks(filepath.Join(p.Cfg.ServerDir, "paper.jar")); err == nil {
		jar = filepath.Base(real)
	}
	if !run.up {
		return []Line{{"Version", jar}}
	}
	addr := pingAddr(p.Cfg)
	reply, err := p.Ping(ctx, addr)
	if err != nil {
		return []Line{{"Version", jar}, {"Players", "no answer to a ping on " + addr + ", as while it loads: " + err.Error()}}
	}
	players := fmt.Sprintf("%d of %d online", reply.Players.Online, reply.Players.Max)
	names := make([]string, 0, len(reply.Players.Sample))
	for _, s := range reply.Players.Sample {
		names = append(names, s.Name)
	}
	if len(names) > 0 {
		players += ": " + strings.Join(names, ", ")
	}
	return []Line{{"Version", reply.Version.Name + " (" + jar + ")"}, {"Players", players}}
}

// pingAddr is where the server listens on this host.
func pingAddr(c *config.Config) string {
	ip := c.BindIP
	if ip == "" || ip == "0.0.0.0" || ip == "::" {
		ip = "127.0.0.1"
	}
	return net.JoinHostPort(ip, strconv.Itoa(c.GamePort))
}

func (p *Probe) memory(run running) Line {
	capText := size(p.Cfg.MemCap) + " cap"
	switch {
	case !run.up:
		return Line{"Memory", "none in use; " + capText}
	case run.memory < 0:
		return Line{"Memory", run.memError + "; " + capText}
	}
	pct := 0
	if p.Cfg.MemCap > 0 {
		pct = int(run.memory * 100 / p.Cfg.MemCap)
	}
	return Line{"Memory", fmt.Sprintf("%s of %s (%d%%)", size(run.memory), capText, pct)}
}

func (p *Probe) ports(ctx context.Context) []Line {
	lines := []Line{}
	out, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"ss", "-H", "-t", "-u", "-a", "-n"}})
	if err != nil {
		lines = append(lines, Line{"Ports", "can't list sockets: " + err.Error()})
	} else {
		lines = append(lines, portLines(out, "tcp", p.Cfg.GamePort)...)
		if p.Cfg.Bedrock {
			lines = append(lines, portLines(out, "udp", p.Cfg.BedrockPort)...)
		}
		lines[0].Label = "Ports"
	}
	fw := "off in the config"
	if p.Cfg.Firewall {
		fw = "drop counters not set up yet"
	}
	return append(lines, Line{"Firewall", fw})
}

// portLines reads ss -H -t -u -a -n: netid, state, queues, local, peer.
func portLines(ss []byte, proto string, port int) []Line {
	want := strconv.Itoa(port)
	var listen []string
	conns := 0
	for _, l := range strings.Split(string(ss), "\n") {
		f := strings.Fields(l)
		if len(f) < 6 || f[0] != proto {
			continue
		}
		i := strings.LastIndexByte(f[4], ':')
		if i < 0 || f[4][i+1:] != want {
			continue
		}
		switch f[1] {
		case "LISTEN", "UNCONN":
			listen = append(listen, f[4])
		case "ESTAB":
			conns++
		}
	}
	if len(listen) == 0 {
		return []Line{{"", fmt.Sprintf("nothing listens on %s %d", proto, port)}}
	}
	text := fmt.Sprintf("%s %s listening", proto, strings.Join(listen, ", "))
	if proto == "tcp" {
		text += fmt.Sprintf(", %d %s", conns, plural(conns, "connection"))
	}
	return []Line{{"", text}}
}

func (p *Probe) whitelist() Line {
	list, err := settings.ReadPlayers(p.Cfg.ServerDir, "whitelist.json")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Line{"Whitelist", "no whitelist.json yet"}
	case err != nil:
		return Line{"Whitelist", "can't read it: " + err.Error()}
	}
	return Line{"Whitelist", fmt.Sprintf("%d %s", len(list), plural(len(list), "player"))}
}

func (p *Probe) ops() Line {
	list, err := settings.ReadPlayers(p.Cfg.ServerDir, "ops.json")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Line{"Ops", "none"}
	case err != nil:
		return Line{"Ops", "can't read them: " + err.Error()}
	case len(list) == 0:
		return Line{"Ops", "none"}
	}
	names := make([]string, 0, len(list))
	for _, o := range list {
		names = append(names, clean(o.Name))
	}
	return Line{"Ops", strings.Join(names, ", ")}
}

func (p *Probe) plugins() Line {
	approved, _, err := plugins.ReadApproved(p.PluginsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Line{"Plugins", "no approved list at " + p.PluginsFile + " yet"}
	}
	if err != nil {
		return Line{"Plugins", "can't read the approved list: " + err.Error()}
	}
	c, err := plugins.CheckJars(p.Cfg.ServerDir, approved, &p.sums)
	if err != nil {
		return Line{"Plugins", "can't check the jars: " + err.Error()}
	}
	var text string
	switch {
	case c.Jars == 0:
		text = "none"
	case c.OK():
		text = fmt.Sprintf("%d %s, all approved", c.Jars, plural(c.Jars, "jar"))
	default:
		text = fmt.Sprintf("%d %s, NOT all approved, so start refuses", c.Jars, plural(c.Jars, "jar"))
	}
	if len(c.New) > 0 {
		text += "; new: " + strings.Join(c.New, ", ")
	}
	if len(c.Changed) > 0 {
		text += "; changed: " + strings.Join(c.Changed, ", ")
	}
	if len(c.Missing) > 0 {
		text += "; approved but gone: " + strings.Join(c.Missing, ", ")
	}
	return Line{"Plugins", text}
}

func (p *Probe) trees() []Line {
	lines := make([]Line, 0, len(p.Cfg.OwnerTrees))
	for _, t := range p.Cfg.OwnerTrees {
		fi, err := os.Stat(t)
		if err != nil {
			lines = append(lines, Line{"", t + " is missing"})
			continue
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		uid, gid := strconv.FormatUint(uint64(st.Uid), 10), strconv.FormatUint(uint64(st.Gid), 10)
		lines = append(lines, Line{"", fmt.Sprintf("%s owned by %s:%s", t, userName(uid), groupName(gid))})
	}
	if len(lines) == 0 {
		return lines
	}
	lines[0].Label = "Trees"
	return lines
}

func (p *Probe) boot(ctx context.Context) Line {
	out, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "show", "--property=UnitFileState", "--value", BootUnit}})
	state := strings.TrimSpace(string(out))
	switch {
	case err != nil:
		return Line{"Boot unit", "can't ask systemd: " + err.Error()}
	case state == "":
		return Line{"Boot unit", "not installed"}
	}
	return Line{"Boot unit", state}
}

// log is the end of latest.log. It's the same file in both modes, since the
// server tree is mounted in the container.
func (p *Probe) log() []Line {
	file := filepath.Join(p.Cfg.ServerDir, "logs", "latest.log")
	tail, err := tailFile(file, logLines)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []Line{{"Log", "no " + file + " yet"}}
	case err != nil:
		return []Line{{"Log", "can't read it: " + err.Error()}}
	}
	lines := []Line{{"Log", file}}
	for _, l := range tail {
		lines = append(lines, Line{"", clean(l)})
	}
	return lines
}

// openRegular opens a file in the server tree for reading. The server owns
// the tree, so it could swap a file for a pipe or a device, to hang status or
// feed it without end.
func openRegular(file string) (*os.File, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- under the configured serverDir
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s isn't a regular file", file)
	}
	if err != nil {
		_ = f.Close() // only read from, so nothing is lost
		return nil, err
	}
	return f, nil
}

// tailFile reads only the end of a file, since a log can be large.
func tailFile(file string, n int) ([]string, error) {
	f, err := openRegular(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const chunk = 64 << 10
	start := max(0, fi.Size()-chunk)
	buf := make([]byte, fi.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	buf = bytes.TrimRight(buf, "\r\n")
	if len(buf) == 0 {
		return nil, nil
	}
	lines := strings.Split(string(buf), "\n")
	if start > 0 && len(lines) > 1 {
		lines = lines[1:] // the first one is likely cut
	}
	return lines[max(0, len(lines)-n):], nil
}

// clean drops control characters, so a player's name or chat line can't send
// escape codes to the operator's terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// Write prints the report, with a blank line at the start, the end, and
// between sections. Lines longer than width are cut, unless width is 0.
func (r Report) Write(w io.Writer, width int) error {
	var b strings.Builder
	b.WriteString("\n")
	for _, section := range r {
		for _, l := range section {
			label := ""
			if l.Label != "" {
				label = l.Label + ":"
			}
			b.WriteString(cut(fmt.Sprintf("%-11s %s", label, l.Text), width))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func cut(s string, width int) string {
	if width <= 0 {
		return s
	}
	n := 0
	for i := range s {
		if n == width {
			return s[:i]
		}
		n++
	}
	return s
}

func since(d time.Duration) string {
	d = d.Round(time.Second)
	days, h, m, s := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", max(s, 0))
}

func size(n int64) string {
	if n < 1<<30 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func userName(uid string) string {
	if u, err := user.LookupId(uid); err == nil {
		return u.Username
	}
	return uid
}

func groupName(gid string) string {
	if g, err := user.LookupGroupId(gid); err == nil {
		return g.Name
	}
	return gid
}
