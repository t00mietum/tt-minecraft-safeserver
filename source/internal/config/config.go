// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package config reads config.shcl. The keys and what they mean are in
// design.md, under Configuration model.
package config

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yottacore/shcl/source/go/v3"
)

// Config holds the settings. Sizes are in bytes.
type Config struct {
	RunMode           string
	RunUser           string
	RunGroup          string
	OwnerAfterStop    string
	ServerDir         string
	OwnerTrees        []string
	ZfsDataset        string
	Image             string
	JavaBin           string
	HeapMin           int64
	HeapMax           int64
	MemCap            int64
	Nice              int
	JvmArgs           []string
	ExtraJvmArgs      []string
	BindIP            string
	GamePort          int
	Bedrock           bool
	BedrockPort       int
	AllowFrom         []string
	JoinRateLimit     int
	Egress            string
	HideOnlinePlayers bool
	StartTimeout      time.Duration
	StopTimeout       time.Duration
	Firewall          bool
	StartAtBoot       bool
	ExtraArgs         []string
	Agent             string
	AgentURL          string
	AgentModel        string
	AgentCommand      []string
	AgentHosts        []string
}

// Read lays the settings in an shcl file over c. A key that's unknown, set
// more than once, or has a bad value gets a warning and leaves c as it was,
// so the caller's defaults stand. Only a file that can't be read is an error.
func Read(path string, c *Config) ([]Warning, error) {
	doc, warns, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	setters := c.setters()

	tops := doc.ReadChildFields("").Value
	isTop := make(map[string]bool, len(tops))
	lines := make(map[string][]int, len(tops))
	for _, f := range tops {
		isTop[f.Path] = true
		lines[f.Name] = append(lines[f.Name], f.Line)
	}

	for _, f := range doc.Fields() {
		if !isTop[f.Path] {
			top, _, _ := strings.Cut(f.Path, ".")
			warns = append(warns, Warning{path, f.Line, fmt.Sprintf("unknown key %q under %s", f.Name, authored(doc, top, f.Name))})
		}
	}

	for _, f := range tops {
		name := authored(doc, f.Path, f.Name)
		set, known := setters[f.Name]
		switch {
		case !known:
			warns = append(warns, Warning{path, f.Line, fmt.Sprintf("unknown key %q", name)})
		case len(lines[f.Name]) > 1:
			// One warning for the lot, on the first line.
			if lines[f.Name][0] == f.Line {
				warns = append(warns, Warning{path, f.Line, fmt.Sprintf("%s is set on lines %s; using the default", name, joinInts(lines[f.Name]))})
			}
		default:
			if err := set(doc, f.Path); err != nil {
				warns = append(warns, Warning{path, f.Line, fmt.Sprintf("%s: %v; using the default", name, err)})
			}
		}
	}
	return warns, nil
}

// authored gives a key as it was typed, since shcl folds case.
func authored(doc *shcl.Document, path, folded string) string {
	if name := doc.AuthoredName(path); name != "" {
		return name
	}
	return folded
}

func joinInts(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// NativeMode lets runMode take native. Off while native mode is deferred, per
// backlog 2026111318002855; its code is still built and tested.
const NativeMode = false

func runModes() []string {
	if NativeMode {
		return []string{"podman", "docker", "native"}
	}
	return []string{"podman", "docker"}
}

// A setter reads one key and stores it only when the value is good.
type setter func(doc *shcl.Document, path string) error

// setters maps each key, folded the way shcl folds it, to its setter.
func (c *Config) setters() map[string]setter {
	byName := map[string]setter{
		"runMode":           choice(&c.RunMode, runModes()...),
		"runUser":           text(&c.RunUser, checkAccount),
		"runGroup":          text(&c.RunGroup, checkAccount),
		"ownerAfterStop":    text(&c.OwnerAfterStop, checkAccount),
		"serverDir":         text(&c.ServerDir, checkTree),
		"ownerTrees":        list(&c.OwnerTrees, checkTree),
		"zfsDataset":        text(&c.ZfsDataset, checkDataset),
		"image":             text(&c.Image, checkImage),
		"javaBin":           text(&c.JavaBin, checkProgram),
		"heapMin":           size(&c.HeapMin),
		"heapMax":           size(&c.HeapMax),
		"memCap":            size(&c.MemCap),
		"nice":              integer(&c.Nice, -20, 19),
		"jvmArgs":           list(&c.JvmArgs, nil),
		"extraJvmArgs":      list(&c.ExtraJvmArgs, nil),
		"bindIP":            text(&c.BindIP, checkAddr),
		"gamePort":          integer(&c.GamePort, 1, 65535),
		"bedrock":           flag(&c.Bedrock),
		"bedrockPort":       integer(&c.BedrockPort, 1, 65535),
		"allowFrom":         list(&c.AllowFrom, checkAllow),
		"joinRateLimit":     integer(&c.JoinRateLimit, 1, math.MaxInt32),
		"egress":            choice(&c.Egress, "web", "open"),
		"hideOnlinePlayers": flag(&c.HideOnlinePlayers),
		"startTimeout":      seconds(&c.StartTimeout),
		"stopTimeout":       seconds(&c.StopTimeout),
		"firewall":          flag(&c.Firewall),
		"startAtBoot":       flag(&c.StartAtBoot),
		"extraArgs":         list(&c.ExtraArgs, nil),
		"agent":             choice(&c.Agent, "none", "openai", "anthropic", "command"),
		"agentURL":          text(&c.AgentURL, checkAgentURL),
		"agentModel":        text(&c.AgentModel, checkModel),
		"agentCommand":      argv(&c.AgentCommand),
		"agentHosts":        list(&c.AgentHosts, checkHost),
	}
	folded := make(map[string]setter, len(byName))
	for name, set := range byName {
		folded[strings.ToLower(name)] = set
	}
	return folded
}

var errNoValue = errors.New("no value")

// scalar gives the one value at path as typed.
func scalar(doc *shcl.Document, path string) (string, error) {
	switch doc.Kind(path) {
	case shcl.KindScalar:
	case shcl.KindEmpty:
		return "", errNoValue
	default:
		return "", errors.New("takes one value, not a list")
	}
	r := doc.ReadString(path)
	if r.Status != shcl.Good {
		return "", errNoValue
	}
	return r.Value, nil
}

func text(dst *string, check func(string) (string, error)) setter {
	return func(doc *shcl.Document, path string) error {
		s, err := scalar(doc, path)
		if err != nil {
			return err
		}
		if s, err = check(s); err != nil {
			return err
		}
		*dst = s
		return nil
	}
}

func choice(dst *string, options ...string) setter {
	return text(dst, func(s string) (string, error) {
		lower := strings.ToLower(s)
		for _, o := range options {
			if lower == o {
				return o, nil
			}
		}
		return "", fmt.Errorf("%q isn't one of: %s", s, strings.Join(options, ", "))
	})
}

func flag(dst *bool) setter {
	return func(doc *shcl.Document, path string) error {
		s, err := scalar(doc, path)
		if err != nil {
			return err
		}
		r := doc.ReadBool(path)
		if r.Status != shcl.Good {
			return fmt.Errorf("%q isn't true or false", s)
		}
		*dst = r.Value
		return nil
	}
}

func integer(dst *int, lowest, highest int) setter {
	return func(doc *shcl.Document, path string) error {
		s, err := scalar(doc, path)
		if err != nil {
			return err
		}
		r := doc.ReadInt(path)
		if r.Status != shcl.Good {
			return fmt.Errorf("%q isn't a whole number", s)
		}
		if r.Value < int64(lowest) || r.Value > int64(highest) {
			return fmt.Errorf("%d is out of range, %d to %d", r.Value, lowest, highest)
		}
		*dst = int(r.Value)
		return nil
	}
}

// size takes Java's form, such as 4G, as well as shcl's, such as 4 GiB.
// Either way K, M, G and T are powers of 1024, the same as -Xmx.
func size(dst *int64) setter {
	return func(doc *shcl.Document, path string) error {
		s, err := scalar(doc, path)
		if err != nil {
			return err
		}
		n, ok := javaSize(s)
		if !ok {
			r := doc.ReadSize(path, shcl.SizeNone, false)
			if r.Status != shcl.Good {
				return fmt.Errorf("%q isn't a size, such as 4G", s)
			}
			n = r.Value
		}
		if n <= 0 {
			return fmt.Errorf("%q is too small", s)
		}
		*dst = n
		return nil
	}
}

func javaSize(s string) (int64, bool) {
	if len(s) < 2 {
		return 0, false
	}
	var shift uint
	switch s[len(s)-1] {
	case 'k', 'K':
		shift = 10
	case 'm', 'M':
		shift = 20
	case 'g', 'G':
		shift = 30
	case 't', 'T':
		shift = 40
	default:
		return 0, false
	}
	n, err := strconv.ParseUint(s[:len(s)-1], 10, 63)
	if err != nil || n > math.MaxInt64>>shift {
		return 0, false
	}
	return int64(n) << shift, true
}

// seconds takes a bare number as seconds, or a duration such as 5m.
func seconds(dst *time.Duration) setter {
	return func(doc *shcl.Document, path string) error {
		s, err := scalar(doc, path)
		if err != nil {
			return err
		}
		r := doc.ReadDuration(path, shcl.DurationSeconds)
		if r.Status != shcl.Good {
			return fmt.Errorf("%q isn't a number of seconds, or a time such as 5m", s)
		}
		if r.Value <= 0 {
			return fmt.Errorf("%q is too short", s)
		}
		*dst = r.Value
		return nil
	}
}

// list takes a list, or a single value as a list of one. A bad item sinks the
// whole key, so a half-read list never stands in for what was meant.
func list(dst *[]string, check func(string) (string, error)) setter {
	return func(doc *shcl.Document, path string) error {
		switch doc.Kind(path) {
		case shcl.KindScalar, shcl.KindArray:
		case shcl.KindEmpty:
			return errNoValue
		default:
			return errors.New("takes a list, such as [a, b]")
		}
		r := doc.ReadStringArray(path)
		if r.Status != shcl.Good {
			return errors.New("takes a list, such as [a, b]")
		}
		items := make([]string, 0, len(r.Value))
		for _, s := range r.Value {
			if s == "" {
				return errors.New("has an empty item")
			}
			if check != nil {
				var err error
				if s, err = check(s); err != nil {
					return err
				}
			}
			items = append(items, s)
		}
		*dst = items
		return nil
	}
}

var (
	accountName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
	datasetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*(/[A-Za-z0-9_.:-]+)*$`)
	hostName    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

func checkAccount(s string) (string, error) {
	if !accountName.MatchString(s) {
		return "", fmt.Errorf("%q isn't a user or group name", s)
	}
	return s, nil
}

// checkTree guards paths that get a recursive chown.
func checkTree(s string) (string, error) {
	if !filepath.IsAbs(s) {
		return "", fmt.Errorf("%q isn't a full path", s)
	}
	s = filepath.Clean(s)
	if s == "/" {
		return "", errors.New("can't be /")
	}
	return s, nil
}

func checkDataset(s string) (string, error) {
	if !datasetName.MatchString(s) {
		return "", fmt.Errorf("%q isn't a ZFS dataset name, such as pool/minecraft", s)
	}
	return s, nil
}

func checkImage(s string) (string, error) {
	if strings.HasPrefix(s, "-") || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("%q isn't an image name", s)
	}
	// Podman won't pick a registry for a short name.
	reg, _, found := strings.Cut(s, "/")
	if !found || (reg != "localhost" && !strings.ContainsAny(reg, ".:")) {
		return "", fmt.Errorf("%q has no registry in its name, such as docker.io/library/", s)
	}
	return s, nil
}

// checkProgram takes a full path, or a bare name to look up on PATH.
func checkProgram(s string) (string, error) {
	if strings.HasPrefix(s, "-") || strings.IndexFunc(s, unicode.IsSpace) >= 0 ||
		(strings.Contains(s, "/") && !filepath.IsAbs(s)) {
		return "", fmt.Errorf("%q isn't a full path or a program name", s)
	}
	return s, nil
}

func checkAddr(s string) (string, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("%q isn't an IP address", s)
	}
	return addr.String(), nil
}

// checkAllow takes lan, any, an address, or a range.
func checkAllow(s string) (string, error) {
	if lower := strings.ToLower(s); lower == "lan" || lower == "any" {
		return lower, nil
	}
	if strings.Contains(s, "/") {
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			return "", fmt.Errorf("%q isn't an address range", s)
		}
		return prefix.Masked().String(), nil
	}
	if addr, err := checkAddr(s); err == nil {
		return addr, nil
	}
	return "", fmt.Errorf("%q isn't lan, any, an address, or a range", s)
}

// argv is a program and its arguments, run as given, without a shell.
func argv(dst *[]string) setter {
	return func(doc *shcl.Document, path string) error {
		var items []string
		if err := list(&items, nil)(doc, path); err != nil {
			return err
		}
		if len(items) == 0 {
			return errors.New("takes a program and its arguments, such as [/usr/local/bin/ask, --json]")
		}
		if _, err := checkProgram(items[0]); err != nil {
			return err
		}
		*dst = items
		return nil
	}
}

// checkAgentURL keeps keys out of the config, which everyone can read, and
// plain http on this host.
func checkAgentURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("%q isn't a web address", s)
	}
	if u.User != nil {
		return "", errors.New("can't hold a user or key, since everyone can read the config")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", fmt.Errorf("%q: plain http only goes to a loopback address", s)
		}
	default:
		return "", fmt.Errorf("%q isn't https", s)
	}
	return strings.TrimRight(s, "/"), nil
}

// loopback says whether host is localhost or a loopback address.
func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

func checkModel(s string) (string, error) {
	if len(s) > 200 || strings.HasPrefix(s, "-") || strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return "", fmt.Errorf("%q isn't a model name", s)
	}
	return s, nil
}

func checkHost(s string) (string, error) {
	lower := strings.ToLower(s)
	if !hostName.MatchString(lower) {
		return "", fmt.Errorf("%q isn't a host name, such as cdn.modrinth.com", s)
	}
	return lower, nil
}
