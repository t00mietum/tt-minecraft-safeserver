#!/bin/bash

#  shellcheck disable=1091  ## 'source is valid here, but shellcheck doesn't know the path to it.'
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Runs the hand tests that need root, against a real Paper server, in a throwaway Debian
##		  container with systemd and a real sudo: setup, --adopt, rootful Podman, native mode when
##		  it's on, and the firewall. See checks.bash.
##		- Needs docker that can run --privileged, and the internet.
##		- --zfs runs setup and then the ZFS snapshot checks instead, on a throwaway pool backed by a
##		  file in the run's folder. The container shares this host's kernel, so the pool shows in its
##		  zpool list until the run destroys it.
##		- ROOT_HOST_BIN names a linux/amd64 build to check, such as cicd.bash's release build, which
##		  comes from the pinned Go. Without it, this box's go builds one.
##	Syntax:
##		root-host.bash [--keep-temp] [--zfs]
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

projectName=tt-minecraft-safeserver
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${here}/../config.bash"
source "${here}/../utility/include/safe-rm.bash"
repo="$(cd "${here}/../.." && pwd)"
keep=0
pool=''
for a in "${@}"; do
	case "${a}" in
		--keep-temp) keep=1 ;;
		--zfs) pool="ttmcss-test-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')" ;;
		*) echo "unknown option: ${a}" >&2; exit 2 ;;
	esac
done

HOST_TEMP="$(realpath -m "${HOST_TEMP:-${TMPDIR:-/tmp}}")"
mkdir -p "${HOST_TEMP}/${projectName}"
stamp="$(date +%Y%m%d-%H%M%S)"
for ((n = 1; n < 1000; n++)); do
	runName="$(printf '%s_%03d' "${stamp}" "${n}")"
	runDir="${HOST_TEMP}/${projectName}/${runName}"
	mkdir "${runDir}" 2> /dev/null && break
done
[[ -d "${runDir}" ]] || { echo "can't make a run folder in ${HOST_TEMP}/${projectName}" >&2; exit 1; }
echo $$ > "${runDir}/run.pid"
box="${projectName}-${runName}-root-host"


# Only this run's pool, by the name it made. Its server goes first, since
# Podman's mounts of the dataset keep it busy.
fPoolGone() {
	[[ "${pool}" == ttmcss-test-* ]] && zpool list -H -o name "${pool}" > /dev/null 2>&1 || return 0
	docker exec "${box}" zpool destroy -f "${pool}" 2> /dev/null && return 0
	docker exec "${box}" podman stop -a -t 20 > /dev/null 2>&1
	docker exec "${box}" systemctl stop tt-minecraftd-server > /dev/null 2>&1
	docker exec "${box}" zpool destroy -f "${pool}"
}

fCleanup() {
	if [[ -n "${pool}" ]]; then
		fPoolGone || true
		if zpool list -H -o name "${pool}" > /dev/null 2>&1; then
			echo "FAIL pool ${pool} is still imported; destroy it as root, then remove ${runDir}" >&2
			keep=1
		else
			echo "pool ${pool} is gone"
		fi
	fi
	if ((keep)); then
		echo "kept container ${box} and ${runDir}"
		return
	fi
	local ids
	ids="$(docker ps -aq --filter "label=dev.run=${runName}")"
	if [[ -n "${ids}" ]]; then
		# shellcheck disable=SC2086 # one ID per word
		docker stop -t 20 ${ids} > /dev/null && docker rm ${ids} > /dev/null
		echo "removed container ${box}"
	fi
	fSafeRm "${runDir}"
}
trap fCleanup EXIT

jobs="${BUILD_JOBS}"

if [[ -n "${ROOT_HOST_BIN:-}" ]]; then
	[[ -f "${ROOT_HOST_BIN}" && -x "${ROOT_HOST_BIN}" ]] || { echo "ROOT_HOST_BIN isn't a program: ${ROOT_HOST_BIN}" >&2; exit 2; }
	echo "Checking ${ROOT_HOST_BIN}"
	cp -- "${ROOT_HOST_BIN}" "${runDir}/tt-minecraftd"
else
	echo "Building tt-minecraftd"
	(cd "${repo}" && CGO_ENABLED=0 GOFLAGS="-p=${jobs}" nice -n 19 go build -o "${runDir}/tt-minecraftd" ./source/cmd/tt-minecraftd)
fi

tag="${projectName}-root-host:$(sha256sum "${here}/Dockerfile" | cut -c1-12)"
if ! docker image inspect "${tag}" > /dev/null 2>&1; then
	echo "Building ${tag}"
	nice -n 19 docker build -q --label "dev.project=${projectName}" --label "dev.run=${runName}" -t "${tag}" "${here}" > /dev/null
fi

echo "Starting ${box}"
# Privileged for systemd, nftables, and Podman inside. Its own network
# namespace, so the rules never touch this host's.
docker run -d --name "${box}" --label "dev.project=${projectName}" --label "dev.run=${runName}" \
	--privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock \
	--tmpfs /var/lib/containers:size=4g,exec \
	--memory 6g --memory-swap 6g --oom-score-adj 1000 --cpus "${jobs}" --cpu-shares 256 \
	${pool:+-v "${runDir}:${runDir}"} "${tag}" > /dev/null
for ((i = 0; i < 60; i++)); do
	state="$(docker exec "${box}" systemctl is-system-running 2> /dev/null || true)"
	[[ "${state}" == running || "${state}" == degraded ]] && break
	sleep 1
done
docker exec "${box}" mkdir -p /usr/local/share/root-host
docker cp "${runDir}/tt-minecraftd" "${box}:/home/op/tt-minecraftd"
docker cp "${here}/checks.bash" "${box}:/usr/local/share/root-host/checks.bash"
docker cp "${here}/console.py" "${box}:/usr/local/share/root-host/console.py"
docker exec "${box}" chown op: /home/op/tt-minecraftd

if [[ -n "${pool}" ]]; then
	echo "Installing zfsutils-linux for pool ${pool}"
	docker cp "${here}/zfs-tools.bash" "${box}:/usr/local/share/root-host/zfs-tools.bash"
	docker exec "${box}" bash /usr/local/share/root-host/zfs-tools.bash
fi

echo "Running the checks; setup downloads Paper, Geyser, Podman, and the image"
docker exec -u op -e TERM=xterm -e ZFS_POOL="${pool}" -e ZFS_FILE="${pool:+${runDir}/zpool.img}" -w /home/op "${box}" \
	nice -n 10 bash /usr/local/share/root-host/checks.bash


##	History:
##		- 2026-11-12: Created, with fixes from its first runs.
##		- 2026-11-12: --zfs: snapshot checks on a throwaway pool.
##		- 2026-11-13: Native checks skip while native mode is off.
##		- 2026-11-19: ROOT_HOST_BIN checks a given build, and the image gets the run's label.
##		- 2026-11-20: Shared removal guard and job count.
