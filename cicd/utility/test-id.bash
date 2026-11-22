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
##		  cicd/root-host/checks.bash, or of a function there that hands its arguments to `check`.
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
# shellcheck disable=SC2016  ## the awk program is single-quoted on purpose
fGoTests(){
	find "${root}/source" -name '*_test.go' -type f -print0 | sort -z | xargs -0 awk '
		/^func (Test|Fuzz)[A-Za-z0-9_]*\(/ && !/^func TestMain\(/ {
			if (name != "") printf "-\t%s\t%s\n", fileOf, name
			name = $2; sub(/\(.*/, "", name); fileOf = FILENAME; n = 0; next }
		name != "" { n++
			if (match($0, /defer testid\.Line\([tf], "[^"]*"\)/)) {
				id = substr($0, RSTART, RLENGTH); sub(/^.*, "/, "", id); sub(/".*$/, "", id)
				printf "%s\t%s\t%s\n", id, fileOf, name; name = ""; next }
			if (n >= 3) { printf "-\t%s\t%s\n", fileOf, name; name = "" } }
		END { if (name != "") printf "-\t%s\t%s\n", fileOf, name }'
}
## "<id>\t<name>" per root host check, once per call, so a repeat shows. A check runs through
## `check` or through a function that hands its arguments to it, as `native` does, so those
## names come from the file rather than a list here. A commented-out call doesn't count.
# shellcheck disable=SC2016  ## the awk program is single-quoted on purpose
fRootChecks(){
	local f="${root}/cicd/root-host/checks.bash" names
	[[ -f "${f}" ]] || return 0
	names="$(awk '
		match($0, /^[A-Za-z_][A-Za-z0-9_]*\(\)/) { fn = substr($0, 1, RLENGTH - 2) }
		fn != "" && /(^|[^A-Za-z0-9_])check "\$(\{@\}|@)"/ { print fn }
		/^}/ { fn = "" }' "${f}" | sort -u | paste -sd'|' -)"
	names="check${names:+|${names}}"
	sed -nE "s/^[[:space:]]*(${names})[[:space:]]+([0-9a-z]+)[[:space:]]+\"?([^\"]*)\"?[[:space:]].*\$/\2\t\3/p" "${f}"
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
