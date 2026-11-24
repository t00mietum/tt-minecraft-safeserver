#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Checks that every Go test has its own ID, and lists them.
##		- An ID is 7 characters of lowercase Crockford base 32 (0-9, a-z less i, l, o, u): the minutes
##		  since 2000-01-01 UTC when it was made, times 100, plus a counter from 00 to 99 that's 00
##		  unless that minute already has one, padded with leading zeros. The counter goes in at base
##		  10 so an ID still reads as one number. `--new` prints the next free one.
##		- A Go test has it as the first line of its body, `defer testid.Line(t, "<id>")`, or `f` in a
##		  fuzz target. A root host check has it as the first argument of a `check` call in
##		  cicd/root-host/checks.bash, or of a function there that hands its arguments to `check`
##		  or to another such function. A commented-out one doesn't count.
##	Syntax:
##		test-id.bash --check [-q] [--root DIR]
##		test-id.bash --list [--in PATH]... [--go-name REGEX] [--root DIR]
##		test-id.bash --new [--root DIR]
##		  --check     fail on a Go test with no ID, a malformed ID on any test, or an ID used twice
##		  --list      print "<id> <label>" for each test, once each
##		  --in PATH   with --list, only tests in files under PATH, from the repo root; give it
##		              again for more. cicd/root-host takes in the root host checks.
##		  --go-name REGEX
##		              with --list, only Go tests and fuzz targets whose name matches
##		  --new       print a free ID for a new test, from the clock
##		  -q          print findings only
##	Exit: 0 clean, 1 findings, 2 bad usage.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


set -Eeuo pipefail

meDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd -- "${meDir}/../.." && pwd)"
mode=""; quiet=0; inPaths=(); goName=.
while (($#)); do case "$1" in
	--check) mode=check; shift ;;
	--list)  mode=list; shift ;;
	--new)   mode=new; shift ;;
	-q)      quiet=1; shift ;;
	--root)  root="${2:?--root needs a folder}"; shift 2 ;;
	--in)    p="${2:?--in needs a path}"; p="${p#./}"; inPaths+=("${p%/}"); shift 2 ;;
	--go-name) goName="${2:?--go-name needs a regex}"; shift 2 ;;
	*) printf 'test-id: unknown option: %s\n' "$1" >&2; exit 2 ;;
esac; done
[[ -n "${mode}" ]] || { printf 'test-id: give --check, --list or --new\n' >&2; exit 2; }

## "<id>\t<file>\t<name>" per Go test with an ID, and "-\t<file>\t<name>" per test without one.
## Only a live `defer testid.Line` counts: one that's commented out, or inside a /* */ block, doesn't.
# shellcheck disable=SC2016  ## the awk program is single-quoted on purpose
fGoTests(){
	find "${root}/source" -name '*_test.go' -type f -print0 | sort -z | xargs -0 awk '
		FNR == 1 { inBlock = 0 }
		inBlock { if (index($0, "*/")) inBlock = 0; next }
		/^[ \t]*\/\*/ { if (!index(substr($0, index($0, "/*") + 2), "*/")) inBlock = 1; next }
		/^func (Test|Fuzz)[A-Za-z0-9_]*\(/ && !/^func TestMain\(/ {
			if (name != "") printf "-\t%s\t%s\n", fileOf, name
			name = $2; sub(/\(.*/, "", name); fileOf = FILENAME; n = 0; next }
		name != "" { n++
			if (match($0, /^[ \t]*defer testid\.Line\([tf], "[^"]*"\)/)) {
				id = substr($0, RSTART, RLENGTH); sub(/^.*, "/, "", id); sub(/".*$/, "", id)
				printf "%s\t%s\t%s\n", id, fileOf, name; name = ""; next }
			if (n >= 3) { printf "-\t%s\t%s\n", fileOf, name; name = "" } }
		END { if (name != "") printf "-\t%s\t%s\n", fileOf, name }'
}
## "<id>\t<name>" per root host check, once per call, so a repeat shows. A check runs through
## `check` or through a function that hands its arguments to it, as `native` does, or to another
## such function, so those names come from the file rather than a list here. A call is a name in
## command position anywhere on a line, after `&&` or `;` too. Its first word is the ID as written,
## quotes and all, so an ID it can't read fails the form check rather than going unseen. A call
## in a comment or a quoted string doesn't count.
# shellcheck disable=SC2016  ## the awk program is single-quoted on purpose
fRootChecks(){
	local f="${root}/cicd/root-host/checks.bash"
	[[ -f "${f}" ]] || return 0
	awk -v sq="'" '
		# The line with quoted text as x and any comment gone, the same length up to the
		# comment, so a match in it is at the same place in the line.
		function code(s,   o, i, c, q, len) {
			o = ""; q = ""; len = length(s)
			for (i = 1; i <= len; i++) {
				c = substr(s, i, 1)
				if (q == sq) { o = o (c == sq ? c : "x"); if (c == sq) q = ""; continue }
				if (q == "\"") {
					if (c == "\\" && i < len) { o = o "xx"; i++; continue }
					o = o (c == "\"" ? c : "x"); if (c == "\"") q = ""; continue }
				if (c == "\\" && i < len) { o = o "xx"; i++; continue }
				if (c == "#" && (i == 1 || substr(s, i - 1, 1) ~ /[ \t;&|(]/)) break
				if (c == sq || c == "\"") q = c
				o = o c }
			return o }
		# Change in block brace depth over a line of code, leaving out ${...}.
		function braces(m,   i, c, st, d, top) {
			d = 0; top = 0
			for (i = 1; i <= length(m); i++) {
				c = substr(m, i, 1)
				if (c == "{") { st[++top] = (i > 1 && substr(m, i - 1, 1) == "$") ? "v" : "b"; if (st[top] == "b") { d++; opened = 1 } }
				else if (c == "}") { if (top > 0) { if (st[top] == "b") d--; top-- } else d-- } }
			return d }
		# The first word at or after i, and where it ends, in end.
		function word(s, i,   j) {
			while (i <= length(s) && substr(s, i, 1) ~ /[ \t]/) i++
			j = i; while (j <= length(s) && substr(s, j, 1) !~ /[ \t;&|)<>]/) j++
			end = j; return substr(s, i, j - i) }
		# Where each call of nm on line k starts its arguments, as "p1 p2 ...".
		function calls(k, nm,   m, at, from, p, pre, r, nx) {
			m = msk[k]; r = ""; from = 1
			while ((at = index(substr(m, from), nm)) > 0) {
				p = from + at - 1; from = p + length(nm)
				if (p > 1 && substr(m, p - 1, 1) ~ /[A-Za-z0-9_$-]/) continue
				nx = substr(m, from, 1); if (nx != "" && nx !~ /[ \t]/) continue
				if (substr(m, from) ~ /^[ \t]*\(\)/) continue
				pre = substr(m, 1, p - 1); sub(/[ \t]+$/, "", pre)
				if (pre == "" || pre ~ /[;&|({!]$/ || pre ~ /(^|[ \t;])(then|do|else|time)$/) r = r " " from }
			return r }
		{ line[NR] = $0; msk[NR] = code($0); fnOf[NR] = fn
			m = msk[NR]
			if (fn == "" && (m ~ /^[ \t]*[A-Za-z_][A-Za-z0-9_]*[ \t]*\(\)/ || m ~ /^[ \t]*function[ \t]+[A-Za-z_]/)) {
				fn = m; sub(/^[ \t]*(function[ \t]+)?/, "", fn); sub(/[^A-Za-z0-9_].*$/, "", fn)
				fnOf[NR] = fn; depth = 0; opened = 0 }
			if (fn != "") { depth += braces(m); if (opened && depth <= 0) fn = "" } }
		END {
			isName["check"] = 1
			do { grew = 0
				for (k = 1; k <= NR; k++) {
					if (fnOf[k] == "" || (fnOf[k] in isName)) continue
					for (nm in isName) {
						n = split(calls(k, nm), ps, " ")
						for (i = 1; i <= n; i++) { a = word(line[k], ps[i] + 0); if (a == "\"${@}\"" || a == "\"$@\"") { isName[fnOf[k]] = 1; grew = 1 } } } }
			} while (grew)
			for (k = 1; k <= NR; k++) for (nm in isName) {
				n = split(calls(k, nm), ps, " ")
				for (i = 1; i <= n; i++) {
					id = word(line[k], ps[i] + 0)
					if (fnOf[k] != "" && (id == "\"${@}\"" || id == "\"$@\"")) continue
					if (id == "") id = "(none)"
					rest = substr(line[k], end); sub(/^[ \t]+/, "", rest)
					label = rest
					if (substr(rest, 1, 1) == "\"" || substr(rest, 1, 1) == sq) { q = substr(rest, 1, 1); label = substr(rest, 2); sub(q ".*$", "", label) }
					else sub(/[ \t].*$/, "", label)
					printf "%s\t%s\n", id, label } } }' "${f}"
}

## A file, from the repo root, under one of the --in paths, or any file without one.
fIn(){
	local p
	((${#inPaths[@]})) || return 0
	for p in "${inPaths[@]}"; do [[ "$1" != "${p}" && "$1" != "${p}"/* ]] || return 0; done
	return 1
}

if [[ "${mode}" == list ]]; then
	{
		while IFS=$'\t' read -r id file name; do
			if [[ "${id}" != - && "${name}" =~ ${goName} ]] && fIn "${file#"${root}"/}"; then printf '%s\t%s\n' "${id}" "${name}"; fi
		done < <(fGoTests)
		if fIn cicd/root-host/checks.bash; then fRootChecks; fi
	} | sort -u | tr '\t' ' '
	exit 0
fi

digits=0123456789abcdefghjkmnpqrstvwxyz

if [[ "${mode}" == new ]]; then
	min=$(( ($(date -u +%s) - 946684800) / 60 )); base=$((min * 100))
	used="$({ fGoTests | cut -f1; fRootChecks | cut -f1; } || true)"
	for ((n = 0; n < 100; n++)); do
		v=$((base + n)); id=""
		for _ in 1 2 3 4 5 6 7; do id="${digits:v%32:1}${id}"; v=$((v / 32)); done
		grep -qx "${id}" <<<"${used}" || { printf '%s\n' "${id}"; exit 0; }
	done
	printf 'test-id: no free ID this minute\n' >&2; exit 1
fi

bad=0
re='^[0-9a-hjkmnp-tv-z]{7}$'
goTests="$(fGoTests)"; rootChecks="$(fRootChecks)"
while IFS=$'\t' read -r id file name; do
	[[ -n "${id}" ]] || continue
	rel="${file#"${root}"/}"
	if [[ "${id}" == - ]]; then printf 'no ID: %s %s\n' "${rel}" "${name}"; bad=1
	elif [[ ! "${id}" =~ ${re} ]]; then printf 'bad ID "%s": %s %s\n' "${id}" "${rel}" "${name}"; bad=1; fi
done <<< "${goTests}"
while IFS=$'\t' read -r id name; do
	[[ -n "${id}" ]] || continue
	[[ "${id}" =~ ${re} ]] || { printf 'bad ID "%s": cicd/root-host/checks.bash %s\n' "${id}" "${name}"; bad=1; }
done <<< "${rootChecks}"
dups="$({ awk -F'\t' '$1 != "-" { print $1 }' <<< "${goTests}"; cut -f1 <<< "${rootChecks}"; } | sed '/^$/d' | sort | uniq -d)"
if [[ -n "${dups}" ]]; then printf 'ID used twice: %s\n' "${dups//$'\n'/ }"; bad=1; fi
((bad)) || { ((quiet)) || printf 'test IDs: every test has its own\n'; }
exit "${bad}"


##	History:
##		- 2026-11-12: Created.
##		- 2026-11-13: Fuzz targets take their ID the same way, from `f`.
##		- 2026-11-19: Root host checks run through `native`, or any function that hands its
##		  arguments to `check`, count too, and a check ID used twice in checks.bash is a repeat.
##		- 2026-11-19: IDs are the clock's minute plus a 2-character counter, 7 characters in all, and
##		  `--new` prints one. A root host check's ID is checked for form too.
##		- 2026-11-19: The counter goes in at base 10, before the Crockford step: an ID is the minute
##		  times 100 plus the counter. Every existing ID was re-encoded the same way.
##		- 2026-11-20: --check reads the tests once.
##		- 2026-11-22: --list takes --in and --go-name, so it can list only what a run would test.
##		- 2026-11-22: A commented-out testid.Line doesn't count. A root host call counts anywhere on
##		  a line, through a wrapper written any way, or a wrapper of one, and its ID is checked as
##		  written, so a quoted or uppercase one fails.
