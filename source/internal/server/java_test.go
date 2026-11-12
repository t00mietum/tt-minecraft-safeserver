// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"reflect"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestJavaArgs(t *testing.T) {
	defer testid.Line(t, "df5xy")
	c := &config.Config{
		HeapMin: 1536 << 20, HeapMax: 4 << 30,
		JvmArgs: []string{"-XX:+UseG1GC"}, ExtraJvmArgs: []string{"-Dfoo=bar"}, ExtraArgs: []string{"--port", "25566"},
	}
	want := []string{"java", "-Xms1536M", "-Xmx4G", "-XX:+UseG1GC", "-Dfoo=bar", "-Dlog4j2.formatMsgNoLookups=true", "-jar", "paper.jar", "--nogui", "--port", "25566"}
	if got := JavaArgs(c, "java"); !reflect.DeepEqual(got, want) {
		t.Errorf("JavaArgs =\n%q\nwant\n%q", got, want)
	}
	if !Loaded([]byte("[12:00:01 INFO]: Done (12.345s)! For help, type \"help\"\n")) || Loaded([]byte("[12:00:01 INFO]: Preparing spawn area\n")) {
		t.Error("Loaded misreads the log")
	}
}

// Old scripts' servers count, under any account and any jar name Paper uses.
func TestRunningPaper(t *testing.T) {
	defer testid.Line(t, "df5xz")
	ps := "root 1 /sbin/init\n" +
		"safeserver 4242 /usr/lib/jvm/java-25/bin/java -Xmx6G -jar paper-26.3-244.jar nogui\n" +
		"  minecraftd 77 java -Xms1G -jar paper.jar --nogui\r\n" +
		"alice 900 vim paper.jar\n" +
		"alice 901 java -jar notpaper-tool.jar\n" +
		"alice 902 grep java -jar paper.jar\n"
	want := []string{"pid 4242, as safeserver", "pid 77, as minecraftd"}
	if got := RunningPaper([]byte(ps)); !reflect.DeepEqual(got, want) {
		t.Errorf("RunningPaper = %q, want %q", got, want)
	}
}

// The thread comes from latest.log's own line form, and nothing else.
func TestLogThread(t *testing.T) {
	defer testid.Line(t, "df754")
	for line, want := range map[string]string{
		"[12:00:03] [Server thread/INFO]: There are 0 of a max of 20 players online: ": MainThread,
		"[09:37:50] [NetherNet Built-in Signaling-4-1/INFO]: [StunObserver] STUN":      "NetherNet Built-in Signaling-4-1",
		"[09:37:50] [a/b thread/WARN]: x":                                              "a/b thread",
		"[12:00:03 INFO]: console form, no thread":                                     "",
		"\tat a second line":                                      "",
		"[12:00:03] [Server thread/INFO] no colon":                "",
		"says [12:00:03] [Server thread/INFO]: later in the line": "",
	} {
		got, ok := LogThread(line)
		if got != want || ok != (want != "") {
			t.Errorf("LogThread(%q) = %q, %v, want %q", line, got, ok, want)
		}
	}
}
