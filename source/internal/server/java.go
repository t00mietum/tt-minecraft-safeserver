// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package server knows the Paper server itself: its command line and its log.
package server

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

// JavaArgs is the whole server command, run from serverDir. The launchers only
// decide where it runs.
func JavaArgs(c *config.Config, java string) []string {
	args := make([]string, 0, 8+len(c.JvmArgs)+len(c.ExtraJvmArgs)+len(c.ExtraArgs))
	args = append(args, java, "-Xms"+heapForm(c.HeapMin), "-Xmx"+heapForm(c.HeapMax))
	args = append(args, c.JvmArgs...)
	args = append(args, c.ExtraJvmArgs...)
	// Current Paper isn't open to Log4Shell. This is in case an old build is.
	args = append(args, "-Dlog4j2.formatMsgNoLookups=true", "-jar", "paper.jar", "--nogui")
	return append(args, c.ExtraArgs...)
}

func heapForm(n int64) string {
	switch {
	case n%(1<<30) == 0:
		return fmt.Sprintf("%dG", n>>30)
	case n%(1<<20) == 0:
		return fmt.Sprintf("%dM", n>>20)
	}
	return fmt.Sprintf("%dk", max(n>>10, 1))
}

var doneLoading = regexp.MustCompile(`Done \([0-9.,]+s\)! For help, type "help"`)

// Loaded tells whether a log shows the server done loading.
func Loaded(log []byte) bool {
	return doneLoading.Match(log)
}

// paperProcess matches a Java command line that runs a Paper jar, whoever
// started it, the old scripts included.
var (
	paperProcess = regexp.MustCompile(`(?i)(^|/)java\s.*-jar\s+(\S*/)?paper[^/\s]*\.jar(\s|$)`)
	psLine       = regexp.MustCompile(`^\s*(\S+)\s+(\d+)\s+(.*)$`)
)

// Proc is one Paper server process. User is as ps gave it, a name or a uid.
type Proc struct {
	User string
	PID  string
}

// PaperProcs picks the Paper servers out of `ps -eo user=,pid=,args=`, or
// uid= in place of user=.
func PaperProcs(ps []byte) []Proc {
	var found []Proc
	for _, line := range strings.Split(string(ps), "\n") {
		fields := psLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if fields != nil && IsPaper(fields[3]) {
			found = append(found, Proc{User: fields[1], PID: fields[2]})
		}
	}
	return found
}

// IsPaper tells whether a process's command line runs a Paper jar.
func IsPaper(args string) bool {
	return paperProcess.MatchString(args)
}

// RunningPaper picks the Paper servers out of `ps -eo user=,pid=,args=`.
func RunningPaper(ps []byte) []string {
	var found []string
	for _, p := range PaperProcs(ps) {
		found = append(found, fmt.Sprintf("pid %s, as %s", p.PID, p.User))
	}
	return found
}
