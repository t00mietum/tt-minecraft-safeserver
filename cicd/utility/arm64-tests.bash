#!/usr/bin/env bash

#  shellcheck disable=1091  ## 'source is valid here, but shellcheck doesn't know the path to it.'
#  shellcheck disable=2087  ## 'Quote the here document.' The removal guard's text is filled in here on purpose; the rest is escaped.
#  shellcheck disable=2329  ## 'Function never invoked.' fDone runs from the EXIT trap.
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Runs the linux/arm64 test binaries on the arm64 test box, for cicd.bash's arm64 step.
##		- Meant to run inside the host lock's wrap, which wakes the box first and puts it back after.
##		- The box gets the repo's source and project folders too, since tests read files beside them.
##		  Its own work folder is ${HOST_TEMP:-/tmp}/<app>/<run name>, removed at the end, on failure
##		  and Ctrl+C too. Stale ones from earlier runs go the same way.
##		- On the box, tests run one package at a time at low priority, with half its cores, or a
##		  quarter when its load is over half of them.
##	Syntax:
##		arm64-tests.bash SSH_TARGET BIN_DIR RUN_NAME STATE_DIR [VAR=value...]
##		  BIN_DIR    holds the <package>.test binaries and a pkgdirs file of "<binary> <folder>" lines
##		  STATE_DIR  gets "reached" once the box answers, and "rc" with this script's exit status
##		  VAR=value  passed to every test binary
##	Exit: 0 every test passed, 1 a test failed or the box broke off partway, 3 the box didn't answer.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


set -Eeuo pipefail

meDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd -- "${meDir}/../.." && pwd)"
source "${meDir}/../config.bash"
source "${meDir}/include/safe-rm.bash"

(($# >= 4)) || { sed -n '/^##	Syntax:/,/^##	Exit:/p' "${BASH_SOURCE[0]}" | sed 's/^##	\{0,1\}//' >&2; exit 2; }
target="$1" binDir="$2" runName="$3" stateDir="$4"; shift 4
envs=("$@")
[[ "${runName}" =~ ^[0-9]{8}-[0-9]{6}_[0-9]{3}$ ]] || { echo "arm64-tests: bad run name: ${runName}" >&2; exit 2; }
[[ -f "${binDir}/pkgdirs" && -d "${stateDir}" ]] || { echo "arm64-tests: no pkgdirs in ${binDir}, or no ${stateDir}" >&2; exit 2; }
sshOpts=(-o BatchMode=yes -o ConnectTimeout=30 -o ServerAliveInterval=30 -o ServerAliveCountMax=6)
remoteDir=""
rc=0

fDone(){
	rc="${1:-${rc}}"
	trap '' INT TERM
	## A dropped ssh doesn't stop what it started there, so stop the run and its test binaries first.
	## Then the box's own copy of the removal guard, with its own HOST_TEMP.
	if [[ -n "${remoteDir}" ]]; then
		ssh "${sshOpts[@]}" "${target}" bash -s -- "${APP_NAME}" "${remoteDir}" <<-EOF_RM || echo "arm64-tests: could not remove ${remoteDir} on ${target}" >&2
			set -u
			APP_NAME="\$1"; HOST_TEMP="\$(realpath -e -- "\${HOST_TEMP:-\${TMPDIR:-/tmp}}")"
			$(declare -f fSafeRm)
			pid=""; read -r pid < "\$2/run.pid" 2> /dev/null || true
			if [[ "\${pid}" =~ ^[0-9]+\$ && -r "/proc/\${pid}/cmdline" ]] && tr '\\0' ' ' < "/proc/\${pid}/cmdline" 2> /dev/null | grep -qF -- "\$2"; then
				kill -TERM "\${pid}" 2> /dev/null && echo "stopped the run on the box"
			fi
			pkill -TERM -f -- "\$2/" && sleep 1
			fSafeRm "\$2"
		EOF_RM
	fi
	echo "${rc}" > "${stateDir}/rc"
	exit "${rc}"
}
trap 'fDone $?' EXIT
trap 'exit 130' INT; trap 'exit 143' TERM

## Make the work folder, after removing this project's stale ones: over 2 days old, with no live run.
remoteDir="$(ssh "${sshOpts[@]}" "${target}" bash -s -- "${APP_NAME}" "${runName}" <<-EOF_SETUP
	set -u
	APP_NAME="\$1"; HOST_TEMP="\$(realpath -e -- "\${HOST_TEMP:-\${TMPDIR:-/tmp}}")" || exit 1
	$(declare -f fSafeRm)
	own="\${HOST_TEMP}/\${APP_NAME}"
	mkdir -p -- "\${own}" || exit 1
	while IFS= read -r d; do
		[[ "\${d##*/}" =~ ^[0-9]{8}-[0-9]{6}_[0-9]{3}\$ && -f "\${d}/run.pid" && ! -L "\${d}" ]] || continue
		pid=""; read -r pid < "\${d}/run.pid" || true
		if [[ "\${pid}" =~ ^[0-9]+\$ ]] && kill -0 "\${pid}" 2> /dev/null; then continue; fi
		fSafeRm "\${d}" > /dev/null || true
	done < <(find "\${own}" -mindepth 1 -maxdepth 1 -type d -mtime +1 2> /dev/null)
	mkdir -- "\${own}/\$2" || exit 1
	echo "\$\$" > "\${own}/\$2/run.pid"
	echo "\${own}/\$2"
EOF_SETUP
)" || { remoteDir=""; echo "arm64-tests: ${target} didn't answer" >&2; exit 3; }
[[ "${remoteDir}" == /*/"${APP_NAME}/${runName}" ]] || { echo "arm64-tests: unexpected work folder from ${target}: ${remoteDir}" >&2; remoteDir=""; exit 3; }
touch "${stateDir}/reached"
echo "work folder: ${target}:${remoteDir}"

tar -c -C "${root}" source project -C "$(dirname -- "${binDir}")" "$(basename -- "${binDir}")" \
	| ssh "${sshOpts[@]}" "${target}" tar -x -C "${remoteDir}" || { echo "arm64-tests: copy to ${target} failed" >&2; exit 1; }

## One package at a time, from its own folder, as go test runs it.
ssh "${sshOpts[@]}" "${target}" bash -s -- "${remoteDir}" "$(basename -- "${binDir}")" "${envs[@]}" <<-'EOF_RUN' || rc=$?
	set -u
	dir="$1" bin="$2"; shift 2
	cores="$(nproc 2> /dev/null || echo 1)"
	read -r load _ < /proc/loadavg || load=0
	jobs=$((cores / 2)); awk -v l="${load}" -v c="${cores}" 'BEGIN { exit !(l * 2 > c) }' && jobs=$((cores / 4))
	((jobs >= 1)) || jobs=1
	low=(nice -n 19); command -v ionice > /dev/null 2>&1 && low+=(ionice -c 3)
	echo "box: $(uname -m), ${cores} cores, load ${load}, ${jobs} job(s), at low priority"
	echo "$$" > "${dir}/run.pid"
	mkdir -- "${dir}/tmp"
	fail=0
	while read -r exe pkg; do
		echo "=== package ${pkg}"
		( cd -- "${dir}/${pkg}" && env "$@" GOMAXPROCS="${jobs}" TMPDIR="${dir}/tmp" "${low[@]}" \
			"${dir}/${bin}/${exe}" -test.v -test.count=1 -test.parallel="${jobs}" -test.timeout=20m ) || { echo "FAILED package ${pkg}"; fail=1; }
	done < "${dir}/${bin}/pkgdirs"
	exit "${fail}"
EOF_RUN
((rc == 0)) || rc=1
exit "${rc}"


##	History:
##		- 2026-11-13: Created.
