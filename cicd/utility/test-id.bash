#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Checks that every Go test has its own ID, and lists them.
##		- An ID is the whole minutes from 2000-01-01 UTC to when the test was written, in lowercase
##		  Crockford base 32 (0-9, a-z less i, l, o, u). A minute another test already has goes to the
##		  next free one.
##		- A Go test has it as the first line of its body, `defer testid.Line(t, "<id>")`. A root
##		  host check has it as the first argument of a `check` call in cicd/root-host/checks.bash.
##	Syntax:
##		test-id.bash --check [-q] [--root DIR]
##		test-id.bash --list [--root DIR]
##		  --check     fail on a Go test with no ID, a malformed ID, or an ID used twice
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
			if (match($0, /defer testid\.Line\(t, "[^"]*"\)/)) {
				id = substr($0, RSTART, RLENGTH); sub(/^.*, "/, "", id); sub(/".*$/, "", id)
				printf "%s\t%s\t%s\n", id, fileOf, name; name = ""; next }
			if (n >= 3) { printf "-\t%s\t%s\n", fileOf, name; name = "" } }
		FNR == 1 && name != "" { }
		END { if (name != "") printf "-\t%s\t%s\n", fileOf, name }'
}
## "<id>\t<name>" per root host check.
fRootChecks(){
	local f="${root}/cicd/root-host/checks.bash"
	[[ -f "${f}" ]] || return 0
	sed -nE 's/^[[:space:]]*check[[:space:]]+([0-9a-z]{5})[[:space:]]+"?([^"]*)"?[[:space:]].*$/\1\t\2/p' "${f}"
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
dups="$({ fGoTests | awk -F'\t' '$1 != "-" { print $1 }'; fRootChecks | cut -f1 | sort -u; } | sort | uniq -d)"
if [[ -n "${dups}" ]]; then printf 'ID used twice: %s\n' "${dups//$'\n'/ }"; bad=1; fi
((bad)) || { ((quiet)) || printf 'test IDs: every test has its own\n'; }
exit "${bad}"


##	History:
##		- 2026-11-12: Created.
