// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// testHost gives a Host on its own passwd and group files. reply answers
// docker, systemctl, zfs, and ps.
func testHost(t *testing.T, passwd, group string, reply func(c host.Cmd) ([]byte, error)) (*Host, *host.Fake) {
	t.Helper()
	dir := t.TempDir()
	h := &Host{
		Cfg:    &config.Config{RunMode: "docker", RunUser: "minecraftd", RunGroup: "minecraftd", OwnerAfterStop: "alice", ServerDir: filepath.Join(dir, "server")},
		Passwd: filepath.Join(dir, "passwd"),
		Group:  filepath.Join(dir, "group"),
	}
	h.Cfg.OwnerTrees = []string{h.Cfg.ServerDir}
	h.ConfigPath = filepath.Join(dir, "config.shcl")
	h.RulesFile, h.UnitFile, h.ResolvConf = filepath.Join(dir, "firewall.nft"), filepath.Join(dir, "fw.service"), filepath.Join(dir, "resolv.conf")
	for file, text := range map[string]string{h.Passwd: passwd, h.Group: group} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := &host.Fake{Reply: reply}
	h.Run = f
	was := launch.Installed
	launch.Installed = func(string) bool { return false }
	t.Cleanup(func() { launch.Installed = was })
	return h, f
}

// look runs one check and gives its findings as text, with fixes marked.
func look(t *testing.T, h *Host, c Check) []string {
	t.Helper()
	found, err := c.Look(context.Background(), h)
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	var out []string
	for _, f := range found {
		s := f.Problem
		if f.Fix != nil {
			s += " [fix]"
		}
		out = append(out, s)
	}
	return out
}

// fixAll runs every fix the check offers, and gives the commands they ran.
func fixAll(t *testing.T, h *Host, f *host.Fake, c Check) []string {
	t.Helper()
	found, err := c.Look(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	f.Ran = nil
	for _, x := range found {
		if x.Fix != nil {
			if err := x.Fix(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f.Lines()
}

func hasAll(t *testing.T, what string, have []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.ContainsFunc(have, func(s string) bool { return strings.Contains(s, w) }) {
			t.Errorf("%s lacks %q:\n%s", what, w, strings.Join(have, "\n"))
		}
	}
}

func TestRun(t *testing.T) {
	defer testid.Line(t, "df5za")
	fixed := 0
	checks := []Check{
		{Name: "fine", Look: func(context.Context, *Host) ([]Finding, error) { return nil, nil }},
		{Name: "skipped", Applies: func(*config.Config) bool { return false }, Look: func(context.Context, *Host) ([]Finding, error) {
			t.Error("a check that doesn't apply ran")
			return nil, nil
		}},
		{Name: "broken", Look: func(context.Context, *Host) ([]Finding, error) { return nil, errors.New("can't look") }},
		{Name: "mixed", Look: func(context.Context, *Host) ([]Finding, error) {
			return []Finding{
				{Problem: "a is wrong", Fixed: "a fixed", Fix: func(context.Context) error { fixed++; return nil }},
				{Problem: "b is wrong"},
				{Problem: "c is wrong", Fixed: "c fixed", Fix: func(context.Context) error { return errors.New("no luck") }},
			}, nil
		}},
	}
	run := func(checkOnly bool) []string {
		var lines []string
		Run(context.Background(), &Host{Cfg: &config.Config{}}, checks, checkOnly, func(state, text string) {
			lines = append(lines, state+" "+text)
		})
		return lines
	}
	want := []string{"OK fine", "FAILED broken: can't look", "FIXED a fixed", "FAILED b is wrong", "FAILED c is wrong; the fix failed: no luck"}
	if got := run(false); !reflect.DeepEqual(got, want) || fixed != 1 {
		t.Errorf("fix: %q, %d fixes", got, fixed)
	}
	want = []string{"OK fine", "FAILED broken: can't look", "FAILED a is wrong", "FAILED b is wrong", "FAILED c is wrong"}
	if got := run(true); !reflect.DeepEqual(got, want) || fixed != 1 {
		t.Errorf("--check: %q, %d fixes", got, fixed)
	}
}

func TestConfigCheck(t *testing.T) {
	defer testid.Line(t, "df5zb")
	h, f := testHost(t, "", "", nil)
	hasAll(t, "missing", look(t, h, configCheck), "no config at "+h.ConfigPath+"; sudo tt-minecraftd setup writes one")

	if err := os.WriteFile(h.ConfigPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.ConfigPath, 0o664); err != nil {
		t.Fatal(err)
	}
	found := look(t, h, configCheck)
	hasAll(t, "group-writable", found, "is mode 0664, so more than root can change it [fix]")
	wantFixes := []string{"sudo chmod go-w -- " + h.ConfigPath}
	if os.Geteuid() != 0 {
		hasAll(t, "not root's", found, "is owned by uid", "[fix]")
		wantFixes = append([]string{"sudo chown root:root -- " + h.ConfigPath}, wantFixes...)
	}
	if got := fixAll(t, h, f, configCheck); !reflect.DeepEqual(got, wantFixes) {
		t.Errorf("fixes = %q, want %q", got, wantFixes)
	}
}

const (
	goodPasswd = "root:x:0:0:root:/root:/bin/bash\nminecraftd:x:990:990::/nonexistent:/usr/sbin/nologin\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n"
	goodGroup  = "root:x:0:\nsudo:x:27:alice\nminecraftd:x:990:alice\nalice:x:1000:\n"
)

func TestAccountCheck(t *testing.T) {
	defer testid.Line(t, "df5zc")
	h, _ := testHost(t, goodPasswd, goodGroup, nil)
	if got := look(t, h, accountCheck); len(got) != 0 {
		t.Errorf("good account: %q", got)
	}

	h, f := testHost(t, "root:x:0:0::/root:/bin/bash\n", "root:x:0:\n", nil)
	want := []string{
		"sudo groupadd --system minecraftd",
		"sudo useradd --system --gid minecraftd --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin minecraftd",
	}
	if got := fixAll(t, h, f, accountCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("missing: %q", got)
	}

	h, f = testHost(t, "minecraftd:x:990:27::/home/m:/bin/bash\n", "sudo:x:27:minecraftd,alice\ndocker:x:998:minecraftd\nminecraftd:x:990:minecraftd\n", nil)
	want = []string{
		"sudo usermod --shell /usr/sbin/nologin minecraftd",
		"sudo usermod --gid minecraftd minecraftd",
		"sudo gpasswd --delete minecraftd sudo",
		"sudo gpasswd --delete minecraftd docker",
	}
	if got := fixAll(t, h, f, accountCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("loose account: %q", got)
	}

	for _, uid := range []string{"0", "1000"} {
		h, f = testHost(t, "minecraftd:x:"+uid+":990::/home/m:/bin/bash\n", "sudo:x:27:minecraftd\nminecraftd:x:990:\n", nil)
		found := look(t, h, accountCheck)
		if len(found) != 1 || !strings.Contains(found[0], "is uid "+uid+", which is root or a person's account, so it's left alone") || strings.Contains(found[0], "[fix]") {
			t.Errorf("uid %s: %q", uid, found)
		}
		if got := fixAll(t, h, f, accountCheck); len(got) != 0 {
			t.Errorf("uid %s fixed: %q", uid, got)
		}
	}
}

func TestOperatorCheck(t *testing.T) {
	defer testid.Line(t, "df5zd")
	h, _ := testHost(t, goodPasswd, goodGroup, nil)
	if got := look(t, h, operatorCheck); len(got) != 0 {
		t.Errorf("listed member: %q", got)
	}
	h, _ = testHost(t, goodPasswd+"bob:x:1001:990::/home/bob:/bin/bash\n", goodGroup, nil)
	h.Cfg.OwnerAfterStop = "bob"
	if got := look(t, h, operatorCheck); len(got) != 0 {
		t.Errorf("own group: %q", got)
	}

	h, f := testHost(t, goodPasswd, "minecraftd:x:990:\n", nil)
	if got := fixAll(t, h, f, operatorCheck); !reflect.DeepEqual(got, []string{"sudo gpasswd --add alice minecraftd"}) {
		t.Errorf("not in group: %q", got)
	}

	h.Cfg.OwnerAfterStop = "carol"
	hasAll(t, "no operator", look(t, h, operatorCheck), "operator carol, from ownerAfterStop, doesn't exist")
	h.Cfg.OwnerAfterStop = ""
	if operatorCheck.Applies(h.Cfg) {
		t.Error("applies with ownerAfterStop empty")
	}
}

func TestServerTreeCheck(t *testing.T) {
	defer testid.Line(t, "df5ze")
	h, _ := testHost(t, "", "", nil)
	dir := h.Cfg.ServerDir
	hasAll(t, "no folder", look(t, h, serverTreeCheck), "server folder "+dir+" is missing", "sudo tt-minecraftd setup makes one")
	// With a config there, setup only runs repair, so it can't be the answer.
	if err := os.WriteFile(h.ConfigPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := look(t, h, serverTreeCheck)
	hasAll(t, "no folder, config there", got, "server folder "+dir+" is missing", "serverDir in "+h.ConfigPath, "move "+h.ConfigPath+" aside")
	if strings.Contains(strings.Join(got, "\n"), "setup makes one") {
		t.Errorf("no folder, config there: still sends to setup alone: %q", got)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	hasAll(t, "no jar", look(t, h, serverTreeCheck), "no paper.jar in "+dir)
	if err := os.Symlink("paper-1.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	hasAll(t, "dangling link", look(t, h, serverTreeCheck), "no paper.jar in "+dir)
	if err := os.WriteFile(filepath.Join(dir, "paper-1.jar"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, serverTreeCheck); len(got) != 0 {
		t.Errorf("all there: %q", got)
	}
}

// ownHost names the test's own uid and gid as alice, the operator, and as
// minecraftd, the service account, as asked.
func ownHost(t *testing.T, serverIsMe bool, state string) (*Host, *host.Fake) {
	t.Helper()
	me, mine := os.Getuid(), os.Getgid()
	serverUID := 990
	if serverIsMe {
		serverUID = me
	}
	passwd := fmt.Sprintf("alice:x:%d:%d::/home/alice:/bin/bash\nminecraftd:x:%d:%d::/nonexistent:/usr/sbin/nologin\n", me, mine, serverUID, mine)
	group := fmt.Sprintf("minecraftd:x:%d:alice\n", mine)
	h, f := testHost(t, passwd, group, func(c host.Cmd) ([]byte, error) {
		if c.Args[0] == "docker" || c.Args[0] == "systemctl" {
			return []byte(state + "\n"), nil
		}
		return nil, nil
	})
	// tree refuses folders less than 3 levels deep.
	real, err := filepath.EvalSymlinks(filepath.Dir(h.Cfg.ServerDir))
	if err != nil {
		t.Fatal(err)
	}
	h.Cfg.ServerDir = filepath.Join(real, "srv", "server")
	h.Cfg.OwnerTrees = []string{h.Cfg.ServerDir}
	if err := os.MkdirAll(filepath.Join(h.Cfg.ServerDir, "world"), 0o770); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Dir(h.Cfg.ServerDir), h.Cfg.ServerDir, filepath.Join(h.Cfg.ServerDir, "world")} {
		if err := os.Chmod(d, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(h.Cfg.ServerDir, "world", "level.dat"), nil, 0o660); err != nil {
		t.Fatal(err)
	}
	return h, f
}

func TestOwnershipCheck(t *testing.T) {
	defer testid.Line(t, "df5zf")
	if os.Getuid() == 990 {
		t.Skip("the test's uid is the fake service account's")
	}
	// Stopped, the trees are the operator's, and modes aren't looked at.
	h, _ := ownHost(t, false, "exited")
	if err := os.WriteFile(filepath.Join(h.Cfg.ServerDir, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, ownershipCheck); len(got) != 0 {
		t.Errorf("stopped, operator's: %q", got)
	}

	// Running, the same tree belongs to the wrong account.
	h, f := ownHost(t, false, "running")
	got := fixAll(t, h, f, ownershipCheck)
	hasAll(t, "running, operator's", look(t, h, ownershipCheck), "with the server running, "+h.Cfg.ServerDir+" has 3 files not owned by minecraftd:minecraftd [fix]")
	hasAll(t, "running fix", got, "sudo chown -R -- minecraftd:minecraftd "+h.Cfg.ServerDir, "sudo chmod -R ug+rwX,o-rwx -- "+h.Cfg.ServerDir)

	// Running as the right account, one file is open to others.
	h, _ = ownHost(t, true, "running")
	if got := look(t, h, ownershipCheck); len(got) != 0 {
		t.Errorf("running, good: %q", got)
	}
	if err := os.WriteFile(filepath.Join(h.Cfg.ServerDir, "world", "open.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(h.Cfg.ServerDir, "world", "open.txt"), 0o664); err != nil {
		t.Fatal(err)
	}
	hasAll(t, "running, open file", look(t, h, ownershipCheck), "has 1 file with modes other than ug+rwX,o-rwx [fix]")

	// Stopped, the trees go back to the operator.
	h, _ = ownHost(t, true, "")
	h.Cfg.OwnerAfterStop = "bob"
	if _, err := ownershipCheck.Look(context.Background(), h); err == nil || !strings.Contains(err.Error(), "account bob doesn't exist") {
		t.Errorf("no operator account: %v", err)
	}
	h.Cfg.OwnerAfterStop = "minecraftd"
	h.Cfg.RunUser = "alice"
	if got := look(t, h, ownershipCheck); len(got) != 0 {
		t.Errorf("stopped, with the service account as operator: %q", got)
	}

	// Native mode leaves root's jars out while it runs.
	h, f = ownHost(t, true, "active")
	h.Cfg.RunMode = "native"
	if err := os.WriteFile(filepath.Join(h.Cfg.ServerDir, "paper.jar"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(h.Cfg.ServerDir, "paper.jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, ownershipCheck); len(got) != 0 {
		t.Errorf("native, locked jar: %q", got)
	}
	h.Cfg.RunMode = "docker"
	f.Reply = func(host.Cmd) ([]byte, error) { return []byte("running\n"), nil }
	hasAll(t, "docker, open jar", look(t, h, ownershipCheck), "has 1 file with modes")

	h.Cfg.OwnerTrees = append(h.Cfg.OwnerTrees, filepath.Join(filepath.Dir(h.Cfg.ServerDir), "gone"))
	hasAll(t, "missing tree", look(t, h, ownershipCheck), "tree "+filepath.Join(filepath.Dir(h.Cfg.ServerDir), "gone")+" is missing")
}

func TestBedrockCheck(t *testing.T) {
	defer testid.Line(t, "df5zg")
	h, f := testHost(t, "", "", nil)
	if bedrockCheck.Applies(h.Cfg) {
		t.Error("applies with bedrock off")
	}
	h.Cfg.Bedrock = true
	hasAll(t, "no plugins", look(t, h, bedrockCheck), "Geyser isn't in", "Floodgate isn't in")

	plugins := filepath.Join(h.Cfg.ServerDir, "plugins")
	if err := os.MkdirAll(filepath.Join(plugins, "floodgate"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Geyser-Spigot.jar", "floodgate-spigot.jar"} {
		if err := os.WriteFile(filepath.Join(plugins, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := look(t, h, bedrockCheck); len(got) != 0 {
		t.Errorf("jars, no key yet: %q", got)
	}
	key := filepath.Join(plugins, "floodgate", "key.pem")
	if err := os.WriteFile(key, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fixAll(t, h, f, bedrockCheck); !reflect.DeepEqual(got, []string{"sudo chmod o-rwx -- " + key}) {
		t.Errorf("open key: %q", got)
	}
	if err := os.Chmod(key, 0o640); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, bedrockCheck); len(got) != 0 {
		t.Errorf("closed key: %q", got)
	}
}

func TestDockerCheck(t *testing.T) {
	defer testid.Line(t, "df5zh")
	var noImage bool
	var state string
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) {
		switch {
		case c.Args[1] == "image" && noImage:
			return nil, errors.New("exit status 1: No such image")
		case c.Args[1] == "ps":
			return []byte(state + "\n"), nil
		}
		return nil, nil
	})
	h.Cfg.Image = "temurin@sha256:abc"
	if got := look(t, h, containerCheck); len(got) != 0 {
		t.Errorf("image, no container: %q", got)
	}
	noImage, state = true, "exited"
	want := []string{"sudo docker pull temurin@sha256:abc", "sudo docker rm tt-minecraftd"}
	if got := fixAll(t, h, f, containerCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("no image, old container: %q", got)
	}
	noImage, state = false, "running"
	if got := look(t, h, containerCheck); len(got) != 0 {
		t.Errorf("running: %q", got)
	}
	h.Cfg.RunMode = "native"
	if containerCheck.Applies(h.Cfg) {
		t.Error("applies in native mode")
	}
}

// Podman mode also finds an old container left under Docker, and needs
// catatonit.
func TestPodmanChecks(t *testing.T) {
	defer testid.Line(t, "df64h")
	states := map[string]string{"podman": "running", "docker": "exited"}
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) {
		if c.Args[1] == "ps" {
			return []byte(states[c.Args[0]] + "\n"), nil
		}
		return nil, nil
	})
	h.Cfg.RunMode, h.Cfg.Image = "podman", "docker.io/library/temurin@sha256:abc"
	if got := look(t, h, containerCheck); len(got) != 0 {
		t.Errorf("docker not installed: %q", got)
	}
	launch.Installed = func(name string) bool { return name == "docker" }
	if got := fixAll(t, h, f, containerCheck); !reflect.DeepEqual(got, []string{"sudo docker rm tt-minecraftd"}) {
		t.Errorf("old docker container: %q", got)
	}
	for _, c := range f.Ran {
		if c.Args[0] == "podman" && !c.Sudo {
			t.Errorf("podman without sudo: %q", c.Args)
		}
	}

	if !catatonitCheck.Applies(h.Cfg) {
		t.Error("catatonit check left out in podman mode")
	}
	was := catatonitPaths
	t.Cleanup(func() { catatonitPaths = was })
	dir := t.TempDir()
	catatonitPaths = []string{filepath.Join(dir, "catatonit")}
	hasAll(t, "no catatonit", look(t, h, catatonitCheck), "catatonit isn't installed", "apt-get install catatonit")
	if err := os.WriteFile(catatonitPaths[0], nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, catatonitCheck); len(got) != 0 {
		t.Errorf("catatonit there: %q", got)
	}
	h.Cfg.RunMode = "docker"
	if catatonitCheck.Applies(h.Cfg) {
		t.Error("catatonit check in docker mode")
	}
}

func TestDatasetCheck(t *testing.T) {
	defer testid.Line(t, "df5zj")
	var mounted string
	var err error
	h, f := testHost(t, "", "", func(host.Cmd) ([]byte, error) { return []byte(mounted + "\n"), err })
	if datasetCheck.Applies(h.Cfg) {
		t.Error("applies with no dataset")
	}
	h.Cfg.ZfsDataset = "tank/mc"
	mounted = "yes"
	if got := look(t, h, datasetCheck); len(got) != 0 {
		t.Errorf("mounted: %q", got)
	}
	mounted = "no"
	if got := fixAll(t, h, f, datasetCheck); !reflect.DeepEqual(got, []string{"sudo zfs mount -- tank/mc"}) {
		t.Errorf("not mounted: %q", got)
	}
	mounted = "-"
	hasAll(t, "volume", look(t, h, datasetCheck), "isn't a file system that can be mounted")
	err = errors.New("dataset does not exist")
	hasAll(t, "missing", look(t, h, datasetCheck), "dataset tank/mc doesn't exist, or zfs can't be asked: dataset does not exist")
}

func TestOthersCheck(t *testing.T) {
	defer testid.Line(t, "df5zk")
	ps := "  990  100 java -Xmx4G -jar /srv/mc/paper.jar nogui\n 1000  200 /usr/bin/java -jar paper-26.3-244.jar\n 1000  300 bash\n 1234  400 java -jar paper.jar\n"
	h, f := testHost(t, goodPasswd, goodGroup, func(host.Cmd) ([]byte, error) { return []byte(ps), nil })
	found := look(t, h, othersCheck)
	want := []string{
		"a Paper server this tool didn't start is running, pid 200 as alice; it's left alone, and start refuses until it's stopped",
		"a Paper server this tool didn't start is running, pid 400 as 1234; it's left alone, and start refuses until it's stopped",
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("found %q", found)
	}
	if got := fixAll(t, h, f, othersCheck); len(got) != 0 {
		t.Errorf("fixed: %q", got)
	}
}

func TestHostFirewallCheck(t *testing.T) {
	defer testid.Line(t, "df5zm")
	units := map[string]bool{}
	var ufw, firewalld string
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) {
		switch c.Args[0] {
		case "systemctl":
			if units[c.Args[2]] {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), errors.New("exit status 3")
		case "ufw":
			return []byte(ufw), nil
		case "firewall-cmd":
			return []byte(firewalld), nil
		}
		return nil, nil
	})
	h.Cfg.GamePort, h.Cfg.Bedrock, h.Cfg.BedrockPort = 25565, true, 19132
	if got := look(t, h, hostFirewallCheck); len(got) != 0 {
		t.Errorf("neither active: %q", got)
	}

	units["ufw"] = true
	hasAll(t, "not root", look(t, h, hostFirewallCheck), "ufw is active, and its rules can only be read as root")
	h.Root = true
	ufw = "Status: active\nDefault: deny (incoming), allow (outgoing)\n\nTo                         Action      From\n--                         ------      ----\n22/tcp                     ALLOW IN    Anywhere\n25565/tcp                  ALLOW IN    Anywhere\n19000:19200/udp            ALLOW IN    Anywhere\n19132/tcp                  DENY IN     Anywhere\n"
	want := []string{"ufw is active and doesn't let players in on 19132/tcp; sudo ufw allow 19132/tcp adds it"}
	if got := look(t, h, hostFirewallCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("ufw: %q", got)
	}
	ufw = "Status: active\nDefault: allow (incoming), allow (outgoing)\n"
	if got := look(t, h, hostFirewallCheck); len(got) != 0 {
		t.Errorf("ufw allows all: %q", got)
	}

	units["ufw"], units["firewalld"] = false, true
	firewalld = "22/tcp 25565/tcp 19132/udp 19130-19140/tcp\n"
	if got := look(t, h, hostFirewallCheck); len(got) != 0 {
		t.Errorf("firewalld, all open: %q", got)
	}
	firewalld = "25565/udp\n"
	hasAll(t, "firewalld", look(t, h, hostFirewallCheck),
		"firewalld is active and doesn't let players in on 25565/tcp; sudo firewall-cmd --permanent --add-port=25565/tcp && sudo firewall-cmd --reload adds it",
		"on 19132/udp")
	if got := fixAll(t, h, f, hostFirewallCheck); len(got) != 0 {
		t.Errorf("changed rules: %q", got)
	}
}
