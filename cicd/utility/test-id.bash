#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Checks that every Go test has its own ID, and lists them.
##		- An ID is a number of minutes since 2000-01-01 UTC in lowercase Crockford base 32 (0-9, a-z
##		  less i, l, o, u). A new test takes the next free one after the highest in use, not the
##		  clock: find the highest from the `testid.Line` calls under source and the `check` calls in
##		  cicd/root-host/checks.bash, and add 1.
##		- A Go test has it as the first line of its body, `defer testid.Line(t, "<id>")`, or `f` in a
##		  fuzz target. A root host check has it as the first argument of a `check` call in
##		  cicd/root-host/checks.bash, or of a function there that hands its arguments to `check`.
##	Syntax:
##		test-id.bash --check [-q] [--root DIR]
##		test-id.bash --list [--root DIR]
##		  --check     fail on a Go test with no ID, a malformed ID, or an ID used twice by any test
##		  --list      print "<id> <label>" for each test, once each
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
mode=""; quiet=0
while (($#)); do case "$1" in
	--check) mode=check; shift ;;
	--list)  mode=list; shift ;;
	-q)      quiet=1; shift ;;
	--root)  root="${2:?--root needs a folder}"; shift 2 ;;
	*) printf 'test-id: unknown option: %s\n' "$1" >&2; exit 2 ;;
esac; done
[[ -n "${mode}" ]] || { printf 'test-id: give --check or --list\n' >&2; exit 2; }

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
		FNR == 1 && name != "" { }
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
	sed -nE "s/^[[:space:]]*(${names})[[:space:]]+([0-9a-z]{5})[[:space:]]+\"?([^\"]*)\"?[[:space:]].*\$/\2\t\3/p" "${f}"
}

if [[ "${mode}" == list ]]; then
	{ fGoTests | awk -F'\t' '$1 != "-" { printf "%s\t%s\n", $1, $3 }'; fRootChecks; } | sort -u | tr '\t' ' '
	exit 0
fi

bad=0
re='^[0-9a-hjkmnp-tv-z]{5}$'
while IFS=$'\t' read -r id file name; do
	rel="${file#"${root}"/}"
	if [[ "${id}" == - ]]; then printf 'no ID: %s %s\n' "${rel}" "${name}"; bad=1
	elif [[ ! "${id}" =~ ${re} ]]; then printf 'bad ID "%s": %s %s\n' "${id}" "${rel}" "${name}"; bad=1; fi
done < <(fGoTests)
dups="$({ fGoTests | awk -F'\t' '$1 != "-" { print $1 }'; fRootChecks | cut -f1; } | sort | uniq -d)"
if [[ -n "${dups}" ]]; then printf 'ID used twice: %s\n' "${dups//$'\n'/ }"; bad=1; fi
((bad)) || { ((quiet)) || printf 'test IDs: every test has its own\n'; }
exit "${bad}"


##	History:
##		- 2026-11-12: Created.
##		- 2026-11-13: Fuzz targets take their ID the same way, from `f`.
##		- 2026-11-19: Root host checks run through `native`, or any function that hands its
##		  arguments to `check`, count too, and a check ID used twice in checks.bash is a repeat.
