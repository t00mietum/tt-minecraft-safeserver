#!/usr/bin/env bash

#  shellcheck disable=2155  ## 'Declare and assign separately.' Cumbersome and unnecessary here.
#  shellcheck disable=2086  ## 'Double quote to prevent word splitting.' OK for integer flags.
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		Surface warnings from the newest cicd run log. cicd tees each run to
##		cicd/artifacts/lint/run_<ts>.log (gitignored); this greps out the warning /
##		vet / staticcheck / vuln lines so they can be addressed. Two modes: plain
##		(print warnings from the newest log) and --check (print only when that log
##		is newer than the one last recorded in a local marker, then record it -
##		meant for a per-session startup look that is a no-op until a new cicd run
##		has happened).
##	Syntax:
##		lint-report.bash [--check] [--force] [--no-mark] [--dir DIR] [--file LOG]
##		  --check     gate on the .lint-seen marker; print SEEN and stop if not newer
##		  --force     with --check, report even if already seen
##		  --no-mark   with --check, do not update the marker
##		  --dir DIR   log directory (default: cicd/artifacts/lint next to this script)
##		  --file LOG  report on this log instead of the newest in DIR
##	Exit: 0 normal, 2 skip (no dir / no logs).
##	History: At bottom of script.

##	Copyright (c) 2026 Bubbles
##	Licensed under The MIT License (MIT). Full text at:
##		https://mit-license.org/
##	SPDX-License-Identifier: MIT


if (( BASH_VERSINFO[0] * 100 + BASH_VERSINFO[1] < 404 )); then
	printf '%s\n' "${0##*/}: needs bash 4.4 or newer, and this is bash ${BASH_VERSION}. On macOS, install one with 'brew install bash' and put it first on PATH." >&2; exit 1
fi
set -Eeuo pipefail

meDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
dir="${meDir}/../artifacts/lint"        ## this script lives in cicd/utility -> cicd/artifacts/lint
file=""; check=0; force=0; noMark=0

while (($#)); do case "$1" in
	--check)    check=1; shift ;;
	--force)    force=1; shift ;;
	--no-mark)  noMark=1; shift ;;
	--dir)      dir="${2:-}"; shift 2 ;;
	--file)     file="${2:-}"; shift 2 ;;
	-h|--help)  grep -E '^##' "$0" | sed 's/^##\t\?//'; exit 0 ;;
	*) echo "lint-report: unknown option: $1" >&2; exit 2 ;;
esac; done

fSkip() { echo "lint-report: $1" >&2; exit 2; }   ## 2 = non-fatal skip (matches the cicd profiler stage)

##	Newest run_<ts>[_role].log by timestamp - the role suffix, if gfs rotation added
##	one, is ignored (the timestamp is stable).
fNewest() {
	local d="$1" best="" newest="" f b t
	for f in "${d}"/run_*.log; do
		[[ -e "${f}" ]] || continue
		b="$(basename "${f}")"; t="${b#run_}"; t="${t%%_*}"; t="${t%.log}"
		[[ "${t}" > "${best}" ]] && { best="${t}"; newest="${f}"; }
	done
	[[ -n "${newest}" ]] || return 1
	printf '%s\t%s\n' "${best}" "${newest}"
}

if [[ -n "${file}" ]]; then
	[[ -f "${file}" ]] || fSkip "no such file: ${file}"
	log="${file}"; b="$(basename "${file}")"; ts="${b#run_}"; ts="${ts%%_*}"; ts="${ts%.log}"
else
	[[ -d "${dir}" ]] || fSkip "no log dir: ${dir}"
	nb="$(fNewest "${dir}")" || fSkip "no run logs in ${dir}"
	ts="${nb%%$'\t'*}"; log="${nb#*$'\t'}"
fi

marker="${dir}/.lint-seen"
if ((check)) && ((! force)); then
	seen=""; [[ -f "${marker}" ]] && seen="$(tr -d '[:space:]' < "${marker}" 2>/dev/null)"
	if [[ -n "${ts}" && -n "${seen}" && ! "${ts}" > "${seen}" ]]; then
		echo "SEEN $(basename "${log}")  (nothing newer than ${seen})"; exit 0
	fi
fi

##	Record the marker now (before printing the body) so a caller that pipes stdout
##	to head/less and closes it early still records the look.
if ((check)) && ((! noMark)) && [[ -n "${ts}" ]]; then
	printf '%s\n' "${ts}" > "${marker}" 2>/dev/null || echo "lint-report: could not write marker: ${marker}" >&2
fi

##	Findings are matched by each tool's own output format: the engine's "WARNING:"
##	lines, shellcheck's SCxxxx, markdownlint's MDxxx/rule, the go tools' file.go:line
##	locus, govulncheck's GO-yyyy-n ids, PSScriptAnalyzer's rule and severity columns,
##	and py_compile's traceback. A hard error only shows in a failed run's log (a
##	passing run aborts on the first error).
##	Matching the words "warning" and "error" reported findings that did not exist,
##	twice: the suites' check labels, then the publish stage's archive listing of
##	errors.go. A report that cries wolf gets ignored, and this one sat unread for
##	three weeks. The suites' "  PASS <id> <label>" lines and the rest are still
##	dropped, since a label can quote any of these forms.
warns="$(grep -nE 'WARNING:|\bSC[0-9]{4}\b|\bMD[0-9]{3}/[a-z]|\.go:[0-9]+(:[0-9]+)?: |\bGO-[0-9]{4}-[0-9]+|\bPS[A-Z][A-Za-z]+ +(ParseError|Error|Warning|Information)\b|\.py", line [0-9]+' "${log}" 2>/dev/null \
	| grep -vE '^[0-9]+:[[:space:]]*((PASS|FAIL|SKIP)[: ]|ok:)' || true)"
if [[ -n "${warns}" ]]; then n=$(printf '%s\n' "${warns}" | grep -c .); else n=0; fi

tag="FLAG"; ((check)) && tag="NEW"
if ((n)); then
	echo "${tag} $(basename "${log}")  (${n} warning line(s))"
	echo
	printf '%s\n' "${warns}"
else
	echo "CLEAN $(basename "${log}")  (0 warnings)"
fi


##	Script history:
##		- 20260709: Created.
##		- 20260915: Matches each tool's output format rather than the words warning and error, which matched a file named errors.go in the archive listing.
##		- 20261004 JC: Every expansion braced, and shellcheck enforces it.
##		- 20261026 JC: Drops the suites' PASS, FAIL and SKIP lines.
