#!/bin/bash
# Runs the hand tests that need root, against a real Paper server, in a
# throwaway Debian container with systemd and a real sudo: setup, --adopt,
# rootful Podman, native mode, and the firewall. See checks.bash.
# Needs docker that can run --privileged, and the internet.
#
# Usage: root-host.bash [--keep-temp]
set -euo pipefail

projectName=tt-minecraft-safeserver
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
keep=0
[[ "${1:-}" == --keep-temp ]] && keep=1

HOST_TEMP="$(realpath -m "${HOST_TEMP:-${TMPDIR:-/tmp}}")"
mkdir -p "${HOST_TEMP}/${projectName}"
stamp="$(date +%Y%m%d-%H%M%S)"
for ((n = 1; ; n++)); do
	runName="$(printf '%s_%03d' "${stamp}" "${n}")"
	runDir="${HOST_TEMP}/${projectName}/${runName}"
	mkdir "${runDir}" 2> /dev/null && break
done
echo $$ > "${runDir}/run.pid"
box="${projectName}-${runName}-root-host"

fSafeRm() {
	local p="${1:-}" real='' own="${HOST_TEMP:?}/${projectName:?}"
	if [[ "${#}" -ne 1 || -z "${p}" || "${p}" == *..* || "${p}" == *//* || "${p}" == *$'\n'* ]]; then
		printf 'safe-rm: refused, bad path: "%s"\n' "${p}" >&2; return 1
	fi
	p="${p%/}"
	if ! [[ "${p}" =~ ^/(.+/)?tmp/[^/]+(/[^/]+)*$ || "${p}" == "${own}"/?* ]]; then
		printf 'safe-rm: refused, not under a tmp folder: %s\n' "${p}" >&2; return 1
	fi
	if [[ "${p}" == "${HOST_TEMP}" || ( "${p}" == "${HOST_TEMP}"/* && "${p#"${HOST_TEMP}"/}" != */* ) ]]; then
		printf 'safe-rm: refused, a whole project temp folder: %s\n' "${p}" >&2; return 1
	fi
	if [[ -L "${p}" ]]; then printf 'safe-rm: refused, a link: %s\n' "${p}" >&2; return 1; fi
	real="$(realpath -e -- "${p}" 2> /dev/null)" || { printf 'safe-rm: refused, not there: %s\n' "${p}" >&2; return 1; }
	if [[ "${real}" != "${p}" ]]; then printf 'safe-rm: refused, goes through a link: %s\n' "${p}" >&2; return 1; fi
	rm -rf --one-file-system -- "${p}" && printf 'removed %s\n' "${p}"
}

fCleanup() {
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

cpus="$(nproc)"
jobs=$((cpus / 2))
(($(cut -d. -f1 /proc/loadavg) > cpus / 2)) && jobs=$((cpus / 4))
((jobs < 1)) && jobs=1

echo "Building tt-minecraftd"
(cd "${repo}" && CGO_ENABLED=0 GOFLAGS="-p=${jobs}" nice -n 19 go build -o "${runDir}/tt-minecraftd" ./source/cmd/tt-minecraftd)

tag="${projectName}-root-host:$(sha256sum "${here}/Dockerfile" | cut -c1-12)"
if ! docker image inspect "${tag}" > /dev/null 2>&1; then
	echo "Building ${tag}"
	nice -n 19 docker build -q --label "dev.project=${projectName}" -t "${tag}" "${here}" > /dev/null
fi

echo "Starting ${box}"
# Privileged for systemd, nftables, and Podman inside. Its own network
# namespace, so the rules never touch this host's.
docker run -d --name "${box}" --label "dev.project=${projectName}" --label "dev.run=${runName}" \
	--privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock \
	--tmpfs /var/lib/containers:size=4g,exec \
	--memory 6g --memory-swap 6g --oom-score-adj 1000 --cpus "${jobs}" --cpu-shares 256 \
	"${tag}" > /dev/null
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

echo "Running the checks; setup downloads Paper, Geyser, Podman, and the image"
docker exec -u op -e TERM=xterm -w /home/op "${box}" nice -n 10 bash /usr/local/share/root-host/checks.bash
