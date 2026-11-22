#!/bin/bash

#  shellcheck disable=SC2329  ## check calls each one by name
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- The root steps of the hand tests, run as the operator inside the root-host container.
##		- root-host.bash copies this in and runs it.
##		- Each check prints PASS or FAIL with its ID; later ones lean on earlier ones.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later

set -uo pipefail

dir=/srv/tt-minecraftd/server
log="${dir}/logs/latest.log"
here=/usr/local/share/root-host
out="$(mktemp)"
failed=0

check() {
	local id="${1}" name="${2}"
	shift 2
	if "${@}" > "${out}" 2>&1; then
		printf 'PASS %s %s\n' "${id}" "${name}"
	else
		printf 'FAIL %s %s\n' "${id}" "${name}"
		tail -25 "${out}" | sed 's/^/    /'
		failed=1
	fi
}

# Native mode is off by a constant while it's deferred (backlog 2026111318002855),
# and --native shows in start's help only when it's on.
native() {
	if [[ "$(tt-minecraftd start --help 2>&1)" == *'--native '* ]]; then check "${@}"; else printf 'SKIP %s %s (native mode is off)\n' "${1}" "${2}"; fi
}

# Output that can run long goes to a file before the grep. A grep -q at the end
# of a pipe can exit while the writer still writes, and pipefail then fails the
# check on the writer's SIGPIPE.
say() { printf '%s\n' "${*}"; }
has() { grep -q -- "${1}" "${2}" || { say "no '${1}' in ${2}"; return 1; }; }
# A grep that can't read the file exits 2, which a bare ! reads as a pass.
lacks() {
	local rc=0
	grep -q "${@}" || rc=$?
	((rc == 1)) && return 0
	if ((rc == 0)); then say "'${*: -2:1}' in ${*: -1}"; else say "can't read ${*: -1}"; fi
	return 1
}
# The command ran and was refused. Podman exec's own failures are 125 to 127,
# so a missing container or command isn't a refusal.
refused() {
	local rc=0
	"${@}" || rc=$?
	((rc > 0 && rc < 125)) || { say "not refused, exit ${rc}: ${*}"; return 1; }
}
# Paper logs a console line on its next tick, tens of ms after the tool typed
# it and exited, so the log is read until it shows up.
logged() {
	local i
	for ((i = 0; i < 100; i++)); do
		sudo grep -q -- "${1}" "${2}" && return 0
		sleep 0.1
	done
	say "no '${1}' in ${2} after 10s"
	return 1
}
statusHas() {
	local rc=0
	tt-minecraftd status > status.out || rc=$?
	cat status.out
	((rc == 0)) && has "${1}" status.out
}
# The tool's own lines from the boot unit. The unit's journal also has the
# container's output, since Podman leaves conmon in the unit's cgroup.
toolJournal() { sudo journalctl --no-pager _SYSTEMD_UNIT=tt-minecraftd.service _COMM=tt-minecraftd | tail -20; }
# A failed nft or python step prints nothing, which -eq reads as 0, so both
# of these fail unless they have a number.
counter() {
	local n
	n="$(sudo nft -j list counter inet tt_minecraftd "${1}" | python3 -c 'import json,sys; print([c["counter"]["packets"] for c in json.load(sys.stdin)["nftables"] if "counter" in c][0])')"
	[[ "${n}" =~ ^(0|[1-9][0-9]*)$ ]] || { say "no packet count for counter ${1}: '${n}'" >&2; return 1; }
	say "${n}"
}
# True when the first line with ${1} comes before the first with ${2}.
before() {
	local a b
	a="$(grep -n -m 1 -- "${1}" "${3}" | cut -d: -f1)" b="$(grep -n -m 1 -- "${2}" "${3}" | cut -d: -f1)"
	[[ "${a}" =~ ^[0-9]+$ && "${b}" =~ ^[0-9]+$ ]] || { say "no '${1}' or no '${2}' in ${3}"; return 1; }
	((a < b)) || { say "'${1}' is after '${2}' in ${3}"; return 1; }
}
gateway() { ip -4 route show default | awk '{print $3; exit}'; }
# If cmd misses its answer, show what the log had.
answered() { has 'players online' cmd.out || { say "log:"; sudo tail -15 "${log}"; return 1; }; }

fNewInstall() {
	# Install Podman, go ahead, accept the EULA.
	printf 'y\ny\ny\n' | ./tt-minecraftd setup --set heapMax=1GB | tee setup.out | grep -E '^(FIXED|FAILED|OK)' || true
	has 'repair --check does' setup.out || return 1
	lacks '^FAILED' setup.out || return 1
	id minecraftd && id -nG op | grep -qw minecraftd || return 1
	[[ "$(stat -c %U:%G "${dir}")" == op:minecraftd ]] || return 1
	[[ "$(readlink "${dir}/paper.jar")" == paper-*.jar ]] || return 1
	has 'eula=true' "${dir}/eula.txt" && has 'white-list=true' "${dir}/server.properties" || return 1
	# Only Geyser's and Floodgate's sources; nothing is pinned by hash (2026111914293176).
	[[ "$(grep -c 'source:' /etc/tt-minecraftd/plugins.shcl)" -eq 2 ]] && lacks 'sha256:' /etc/tt-minecraftd/plugins.shcl || return 1
	has 'runMode: podman' /etc/tt-minecraftd/config.shcl || return 1
	[[ -x /usr/local/bin/tt-minecraftd ]] && sudo podman image ls | grep -q eclipse-temurin || return 1
	command -v catatonit || [[ -x /usr/libexec/podman/catatonit ]]
}

fNftRestart() {
	sudo nft list table inet tt_minecraftd > /dev/null && systemctl is-enabled tt-minecraftd-firewall.service || return 1
	sudo systemctl restart nftables && sleep 1
	sudo nft list table inet tt_minecraftd > /dev/null
}

fPodmanStart() {
	! getent group docker || return 1
	# No terminal, as from the boot unit. Podman's run warned then.
	tt-minecraftd start < /dev/null 2>&1 | tee start.out || return 1
	lacks 'not a TTY' start.out || return 1
	sudo podman ps --format '{{.Names}}' | grep -qx tt-minecraftd
}

fJarsInContainer() {
	# A refusal means nothing unless the container is there to refuse it.
	sudo podman exec tt-minecraftd test -f "${dir}/paper.jar" -a -f "${dir}/plugins/Geyser-Spigot.jar" || { say "no jars to look at in the container"; return 1; }
	# Each must fail.
	refused sudo podman exec tt-minecraftd sh -c "echo x >> ${dir}/paper.jar" || return 1
	refused sudo podman exec tt-minecraftd mv "${dir}/plugins/Geyser-Spigot.jar" "${dir}/plugins/x.jar"
}

fStatusPodman() { statusHas 'running in podman mode, as minecraftd'; }

fCmd() { tt-minecraftd cmd list | tee cmd.out; answered; }

fConsole() {
	local word="console-${RANDOM}"
	python3 "${here}/console.py" "say ${word}" || return 1
	has "${word}" "${log}" && statusHas 'running in'
}

fStun() {
	# Geyser's STUN lookup gets an answer, and nothing of the server's own hit the loopback drop.
	local i n
	for ((i = 0; i < 40; i++)); do grep -q 'STUN discovery through .* maps this host' "${log}" && break; sleep 1; done
	has 'maps this host' "${log}" || return 1
	n="$(counter out_local)" || return 1
	((n == 0)) || { say "out_local ${n}"; return 1; }
}

fFirewallOut() {
	local t was now
	for t in out_local:127.0.0.1:80 out_lan:"$(gateway)":80 out_web:1.1.1.1:22; do
		was="$(counter "${t%%:*}")" || return 1
		! sudo -u minecraftd curl -s -m 3 "http://${t#*:}/" > /dev/null || { say "reached ${t#*:}"; return 1; }
		now="$(counter "${t%%:*}")" || return 1
		((now > was)) || { say "${t%%:*} didn't count"; return 1; }
	done
	sudo -u minecraftd curl -s -m 10 -o /dev/null https://fill.papermc.io/v3/projects/paper
}

fStopCancel() {
	local pid rc=0
	tt-minecraftd stop --warn 60 > cancel.out 2>&1 &
	pid=$!
	for _ in $(seq 30); do
		grep -q 'The server stops in 60 seconds' "${log}" && break
		sleep 1
	done
	# As Ctrl-C. The countdown's first wait is 30 seconds.
	kill -INT "${pid}"
	wait "${pid}" || rc=$?
	cat cancel.out
	((rc == 1)) || { say "stop exited ${rc}"; return 1; }
	logged 'The stop is off' "${log}" && has 'interrupted' cancel.out || return 1
	# The server still runs, so its log takes sudo to read, as in logged.
	sudo cat "${log}" | tee cancel-log.out > /dev/null || { say "can't read ${log}"; return 1; }
	lacks 'Saving worlds' cancel-log.out || return 1
	sudo podman ps --format '{{.Names}}' | grep -qx tt-minecraftd
}

fStopWarn() {
	tt-minecraftd stop --warn 10 || return 1
	has 'The server stops in 1 second' "${log}" && has 'Saving worlds' "${log}" || return 1
	[[ "$(stat -c %U "${dir}/world")" == op ]] || return 1
	# The stopped container stays, for logs, and isn't a fault.
	tt-minecraftd repair --check
}

fSameLogin() {
	# The group setup added isn't in this login yet.
	! tt-minecraftd start --native --dry-run > same.out 2>&1 || return 1
	has 'log in again' same.out
}

# Commented out by 2026111914334569: start no longer refuses a changed jar, since plugin approval went.
# fRefuseChanged() {
# 	local jar
# 	jar="${dir}/$(readlink "${dir}/paper.jar")"
# 	cp "${jar}" jar.keep && echo x >> "${jar}" || return 1
# 	! tt-minecraftd start > start.out 2>&1
# 	cp jar.keep "${jar}" && has 'changed since it was approved' start.out
# }

fNativeStart() {
	tt-minecraftd start --native || return 1
	systemctl show tt-minecraftd-server -p ProtectSystem,ProtectHome,PrivateTmp,PrivateDevices,NoNewPrivileges,ReadOnlyPaths > unit.out
	has 'ProtectSystem=strict' unit.out && has 'NoNewPrivileges=yes' unit.out && has 'PrivateDevices=yes' unit.out || return 1
	has 'ReadOnlyPaths=.*Geyser-Spigot.jar' unit.out || return 1
	[[ "$(stat -c %U "$(readlink -f "${dir}/paper.jar")")" == root ]] || return 1
	# As the server, in its own mount view.
	local pid as
	pid="$(pgrep -u minecraftd -f 'paper.*\.jar' | head -1)"
	[[ "${pid}" =~ ^[1-9][0-9]*$ ]] || { say "no server process"; return 1; }
	as=(sudo nsenter -t "${pid}" -m -S "$(id -u minecraftd)" -G "$(id -g minecraftd)")
	# nsenter fails with 1 too, so it has to be seen working first.
	"${as[@]}" test -f "${dir}/paper.jar" -a -f "${dir}/plugins/Geyser-Spigot.jar" || { say "can't look as the server"; return 1; }
	refused "${as[@]}" sh -c "echo x >> ${dir}/paper.jar" || return 1
	refused "${as[@]}" mv "${dir}/plugins/Geyser-Spigot.jar" "${dir}/plugins/x.jar"
}

fNativeFollow() {
	# runMode still says podman, as after start --native.
	statusHas 'running in native mode' || return 1
	tt-minecraftd cmd list | tee cmd.out
	answered && has 'runs in native mode' cmd.out || return 1
	fConsole
}

fNativeStop() {
	tt-minecraftd stop --warn 10 | tee stop.out || return 1
	has 'runs in native mode' stop.out || return 1
	! systemctl is-active tt-minecraftd-server && [[ "$(stat -c %U "${dir}/world")" == op ]]
}

# A failed boot check would leave the unit enabled and the server up, and the
# checks after it would fail on that.
fBoot() {
	fBootSteps && return 0
	tt-minecraftd stop > /dev/null 2>&1
	sudo sed -i '/^startAtBoot:/d; /^startTimeout:/d' /etc/tt-minecraftd/config.shcl
	tt-minecraftd repair > /dev/null 2>&1
	return 1
}

fBootSteps() {
	# Paper takes longer than startTimeout, as a big world can at boot.
	printf 'startAtBoot: true\nstartTimeout: 1\n' | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
	tt-minecraftd repair | tee boot-on.out | grep -E '^(FIXED|FAILED)'
	has 'tt-minecraftd.service enabled' boot-on.out && systemctl is-enabled tt-minecraftd.service || return 1
	# repair never starts it.
	! systemctl is-active tt-minecraftd.service || return 1
	touch boot.mark
	tt-minecraftd start < /dev/null | tee boot-start.out || return 1
	has 'starts the server' boot-start.out || return 1
	# Paper starts a new latest.log.
	for _ in $(seq 120); do
		sudo find "${log}" -newer boot.mark | grep -q . && sudo grep -q 'Done (.*)! For help' "${log}" && break
		sleep 1
	done
	if ! systemctl is-active tt-minecraftd.service || ! sudo podman ps --format '{{.Names}}' | grep -qx tt-minecraftd; then
		toolJournal
		return 1
	fi
	statusHas 'running in podman mode' || return 1
	#  shellcheck disable=SC2024  ## The file is meant to be op's.
	sudo journalctl --no-pager -u tt-minecraftd.service > boot-journal.out
	has "warning: the server didn't finish loading" boot-journal.out || { say "the load wasn't slow"; toolJournal; return 1; }
	tt-minecraftd stop --warn 2 | tee boot-stop.out || return 1
	sudo grep -q 'Saving chunks' "${log}" || { say "no save in the log"; return 1; }
	# start --foreground exits with the server's code, which is 0 after stop.
	if systemctl is-active tt-minecraftd.service || systemctl is-failed --quiet tt-minecraftd.service || [[ "$(stat -c %U "${dir}/world")" != op ]]; then
		toolJournal
		return 1
	fi
	sudo sed -i '/^startAtBoot:/d; /^startTimeout:/d' /etc/tt-minecraftd/config.shcl
	tt-minecraftd repair | tee boot-off.out | grep -E '^(FIXED|FAILED)'
	has 'disabled and removed' boot-off.out && [[ ! -e /etc/systemd/system/tt-minecraftd.service ]]
}

fAdopt() {
	sudo mv /etc/tt-minecraftd/config.shcl /etc/tt-minecraftd/plugins.shcl "${HOME}/" || return 1
	sudo mkdir -p /srv/mc && sudo cp -a "${dir}" /srv/mc/adopted && sudo chown -R root:root /srv/mc/adopted || return 1
	printf 'y\n' | tt-minecraftd setup --adopt /srv/mc/adopted --set heapMax=1GB | tee adopt.out | grep -E '^(FIXED|FAILED)'
	has 'serverDir: /srv/mc/adopted' /etc/tt-minecraftd/config.shcl || return 1
	[[ "$(stat -c %U:%G /srv/mc/adopted/world)" == op:minecraftd ]] || return 1
	# The jars that get a source are listed before the question. They were listed with hashes until 2026111914334569.
	before '  source: ' 'Go ahead' adopt.out || return 1
	# The adopted Geyser and Floodgate keep following GeyserMC.
	for p in geyser floodgate; do
		has "source: \"\\?https://download.geysermc.org/v2/projects/${p}/" /etc/tt-minecraftd/plugins.shcl || { say "no GeyserMC source for ${p}"; return 1; }
	done
	tt-minecraftd repair --check
}

fFirewallOff() {
	echo 'firewall: off' | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
	tt-minecraftd repair | tee repair.out | grep -E '^(FIXED|FAILED)'
	has 'its table removed' repair.out || return 1
	sudo nft list tables | tee tables.out > /dev/null || { say "nft can't list tables"; return 1; }
	lacks -x 'table inet tt_minecraftd' tables.out && ! systemctl is-enabled tt-minecraftd-firewall.service
}

# What install.bash runs through sudo before it swaps a build in: a copy of
# the old one goes to a root-owned old-builds folder, and the build is left in
# place for the rename. A second one from the same second gets a suffix, and
# the folder is taken as it is.
fOldBuild() {
	local bin=/usr/local/bin/tt-minecraftd kept=/var/lib/tt-minecraftd/old-builds
	sudo cp -p "${bin}" "${HOME}/keep.bin" || return 1
	sudo "${bin}" retire-build "${bin}" | tee retire.out || return 1
	has "^Kept a copy of ${bin} as ${kept}/tt-minecraftd\.[0-9]\{8\}-[0-9]\{6\}\.$" retire.out || return 1
	cmp -s "${bin}" "${HOME}/keep.bin" || { say "the build isn't left in place"; return 1; }
	[[ "$(stat -c '%U:%G %a' "${kept}" "${kept}/..")" == $'root:root 755\nroot:root 755' ]] || { stat -c '%n %U:%G %a' "${kept}" "${kept}/.."; return 1; }
	# The same mtime as the copy just kept, so the next one needs the suffix.
	sudo touch -r "${HOME}/keep.bin" "${bin}" || return 1
	sudo "${bin}" retire-build "${bin}" | tee retire.out || return 1
	cmp -s "${bin}" "${HOME}/keep.bin" || { say "the build isn't left in place"; return 1; }
	ls -l "${kept}"
	[[ "$(find "${kept}" -name 'tt-minecraftd.*' | wc -l)" -eq 2 ]] && has '\.2\.$' retire.out
}

fAgentHandOff() {
	# A jar with no source, and an agent that has nothing for it.
	printf 'thing' > /srv/mc/adopted/plugins/Thing.jar || return 1
	# It was approved through repair until 2026111914334569. Being in plugins/ with no source is what sends it to the agent.
	cat > ask <<-'EOF'
		#!/bin/sh
		cat > /dev/null
		echo '{"jars": []}'
	EOF
	chmod 700 ask
	printf 'agent: command\nagentCommand: [%s/ask]\n' "${HOME}" | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
	tt-minecraftd update --plugins --yes < /dev/null | tee agent.out || return 1
	has 'Asking the update agent about' agent.out && has 'plugins/Thing.jar: no answer from the update agent' agent.out || return 1
	# Paper's builds are --paper's.
	lacks 'paper-.*update agent' agent.out || return 1
	# So are Geyser's and Floodgate's, through their GeyserMC source.
	lacks -i 'geyser-spigot.jar: no answer\|floodgate-spigot.jar: no answer' agent.out || return 1
	! compgen -G '/tmp/tt-minecraftd-agent-*' || { say "the answer file is left over"; return 1; }
	sudo tt-minecraftd update --plugins --dry-run < /dev/null | tee agent-root.out
	has "isn't asked when update starts as root" agent-root.out
}

# ZFS: a throwaway file-backed pool, from root-host.bash --zfs. It lives in the
# host's kernel, so it's destroyed on the way out whatever happens.
pool="${ZFS_POOL:-}"
ds="${pool}/mc"
# The privileged container sees every pool on the host, so only a name
# root-host.bash makes is ever touched, whoever set ZFS_POOL.
fZfsOurs() { [[ "${pool}" == ttmcss-test-* ]] || { say "refused: pool '${pool}' isn't a ttmcss-test-* one"; return 1; }; }
[[ -z "${pool}" ]] || fZfsOurs || exit 2
snaps() { sudo zfs list -H -o name -t snapshot -d 1 "${ds}" | sed 's/.*@//'; }
# No snapshots, from a zfs list that worked. A failed one prints nothing too.
none() {
	local s
	s="$(snaps)" || { say "zfs list failed"; return 1; }
	[[ -z "${s}" ]] || { say "snapshots:"; say "${s}"; return 1; }
}
# The pool's history lines for one zfs subcommand, with who ran it.
hist() { sudo zpool history -l "${pool}" | grep -E "^[^ ]+ zfs ${1} " || true; }
newest() { snaps | grep -E "_${1}\$" | tail -1; }

fZfsGone() {
	[[ -n "${pool}" ]] && fZfsOurs && sudo zpool list -H -o name "${pool}" > /dev/null 2>&1 || return 0
	tt-minecraftd stop > /dev/null 2>&1
	sudo zpool destroy -f "${pool}" && echo "destroyed pool ${pool}"
}

fZfsPool() {
	local file="${ZFS_FILE:?}"
	fZfsOurs || return 1
	[[ "${file}" == */tmp/*/zpool.img ]] || { say "bad file ${file}"; return 1; }
	# The run folder is the host user's, not op's.
	sudo truncate -s 512M "${file}" || return 1
	sudo zpool create -o cachefile=none -m none "${pool}" "${file}" || return 1
	# The server's tree moves onto the dataset, as on a real host.
	sudo mv "${dir}" "${dir}.pre-zfs" && sudo zfs create -o mountpoint="${dir}" "${ds}" || return 1
	sudo cp -a "${dir}.pre-zfs/." "${dir}/" && sudo chown --reference="${dir}.pre-zfs" "${dir}" && sudo chmod --reference="${dir}.pre-zfs" "${dir}" || return 1
	printf 'zfsDataset: %s\n' "${ds}" | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
	none
}

# Only update --mc-version snapshots now, which needs a real Paper move.
fZfsNone() {
	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
	none
}

# Start and stop snapshots went (backlog 2026111318002875).
# fZfsStart() {
# 	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
# 	local s
# 	s="$(newest pre_tt-minecraftd-start)"
# 	[[ "${s}" =~ ^[0-9]{8}-[0-9]{6}_pre_tt-minecraftd-start$ ]] || { say "no start snapshot"; snaps; return 1; }
# 	# No zfs allow yet, so it went through sudo.
# 	hist snapshot | tail -1 | tee /dev/stderr | grep -qF "@${s} [user 0 "
# }
#
# fZfsStop() {
# 	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
# 	local s owner
# 	s="$(newest post_tt-minecraftd-stop)"
# 	[[ -n "${s}" ]] || { say "no stop snapshot"; snaps; return 1; }
# 	has 'Saving worlds' "${log}" || return 1
# 	# Taken once the worlds were saved and before the trees went back. A clone,
# 	# since .zfs automounts from the host's mount namespace.
# 	sudo zfs clone -o mountpoint=/mnt/look -o readonly=on "${ds}@${s}" "${pool}/look" || return 1
# 	sudo cmp "${dir}/world/level.dat" /mnt/look/world/level.dat && owner="$(sudo stat -c %U /mnt/look/world)"
# 	sudo zfs destroy "${pool}/look" || return 1
# 	[[ "${owner:-}" == minecraftd ]] || { say "snapshot's world is ${owner:-unreadable}, not the server's"; return 1; }
# 	[[ "$(stat -c %U "${dir}/world")" == op ]]
# }
#
# fZfsPrune() {
# 	# Old ones of the tool's, and others it must leave. By the schedule, only
# 	# 20250102-01 to -04 go: the first, and the last of 2025-01-02's day, week
# 	# and month stay.
# 	local h gone=() n
# 	for h in 01 02 03 04; do gone+=("20250102-${h}0000_pre_tt-minecraftd-start"); done
# 	# One fs per zfs snapshot.
# 	for n in "${gone[@]}" 20250102-050000_pre_tt-minecraftd-start 20250101-000000_post_tt-minecraftd-stop keepme 20250102-030000_pre_tt-minecraftd-start-x; do
# 		sudo zfs snapshot "${ds}@${n}" || return 1
# 	done
# 	hist destroy > before.out
# 	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
# 	hist destroy | grep -vxF -f before.out | tee destroy.out
# 	[[ "$(wc -l < destroy.out)" -eq 1 ]] || { say "not one destroy"; return 1; }
# 	has "zfs destroy -- ${ds}@$(IFS=,; echo "${gone[*]}") " destroy.out || return 1
# 	snaps | tee kept.out
# 	for n in 20250101-000000_post_tt-minecraftd-stop 20250102-050000_pre_tt-minecraftd-start keepme 20250102-030000_pre_tt-minecraftd-start-x; do
# 		grep -qx "${n}" kept.out || { say "${n} went"; return 1; }
# 	done
# 	for n in "${gone[@]}"; do ! grep -qx "${n}" kept.out || { say "${n} stayed"; return 1; }; done
# 	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
# }
#
# fZfsAllow() {
# 	local s p me
# 	me="[user $(id -u) "
# 	sudo zfs allow -u op snapshot,destroy,mount "${ds}" || return 1
# 	hist destroy > before.out
# 	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
# 	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
# 	s="$(newest pre_tt-minecraftd-start)" p="$(newest post_tt-minecraftd-stop)"
# 	hist snapshot | grep -F -e "@${s} " -e "@${p} " | tee allow.out
# 	[[ "$(grep -cF "${me}" allow.out)" -eq 2 ]] || { say "history:"; sudo zpool history -l "${pool}" | tail -12; return 1; }
# 	# Each prune drops an older one of today's, as op too.
# 	hist destroy | grep -vxF -f before.out | tee destroy.out
# 	[[ -s destroy.out ]] || { say "no destroy"; return 1; }
# 	! grep -vqF "${me}" destroy.out
# }

cd "${HOME}" || exit 1
if [[ "${1:-}" != after-login ]]; then
	check 1a3fh00 "setup installs from scratch" fNewInstall
	native 1a3fjr8 "native start in setup's login says to log in again" fSameLogin
	# The group setup added counts from the next login, as setup says.
	exec sudo -u op TERM="${TERM:-xterm}" ZFS_POOL="${pool}" ZFS_FILE="${ZFS_FILE:-}" bash "${0}" after-login "${failed}"
fi
failed="${2:-0}"
if [[ -n "${pool}" ]]; then
	trap fZfsGone EXIT
	check 1a3jn00 "a throwaway pool has the server's tree" fZfsPool
	if ((failed == 0)); then
		check 1a3jqxr "start and stop take no snapshot" fZfsNone
		# Start and stop snapshots, the prune, and zfs allow went (backlog 2026111318002875).
		# check 1a3jn34 "start snapshots the dataset" fZfsStart
		# check 1a3jn68 "stop snapshots after the save, before the hand-back" fZfsStop
		# check 1a3jn9c "a prune destroys only the tool's own, in one destroy" fZfsPrune
		# check 1a3jncg "with zfs allow the operator snapshots and prunes without sudo" fZfsAllow
	fi
	fZfsGone
	exit "${failed}"
fi
check 1a3fh34 "firewall table survives an nftables restart" fNftRestart
check 1a3fh68 "rootful Podman start without the docker group" fPodmanStart
check 1a3fh9c "jars read-only in the container" fJarsInContainer
check 1a3fhcg "status without sudo names podman mode" fStatusPodman
check 1a3fhfm "cmd list in podman mode" fCmd
check 1a3fhjr "console line and Ctrl-C detach in podman mode" fConsole
check 1a3fhnw "Geyser STUN answers behind the firewall" fStun
check 1a3fhs0 "firewall drops and counts the account's local, LAN, and non-web connections" fFirewallOut
check 1a3jwt0 "Ctrl-C in stop --warn's countdown calls the stop off" fStopCancel
check 1a3fhw4 "stop --warn 10 saves and hands the trees back" fStopWarn
# Commented out by 2026111914334569, with fRefuseChanged.
# check 1a3fhz8 "start refuses a changed jar" fRefuseChanged
native 1a3fj2c "native start, sandbox, and locked jars" fNativeStart
native 1a3fj5g "status, cmd, and console follow a native server" fNativeFollow
native 1a3fj8m "stop follows a native server" fNativeStop
check 1a3jqtm "startAtBoot: repair enables the unit, and start and stop go through it" fBoot
check 1a3fjbr "setup --adopt a copy of the tree" fAdopt
check 1a3fjew "firewall off removes the unit and table" fFirewallOff
check 1a3jgsm "update asks the agent as the operator, and hands its answer through sudo" fAgentHandOff
check 1a3jztw "a replaced build is kept in a root-owned old-builds folder" fOldBuild
exit "${failed}"


##	History:
##		- 2026-11-12: Created.
##		- 2026-11-12: Agent hand-off and ZFS snapshot checks.
##		- 2026-11-13: Boot unit checks. Native checks skip while native mode is off.
##		- 2026-11-17: Ctrl-C in the stop countdown. The boot check loads past startTimeout, and checks the unit didn't fail.
##		- 2026-11-18: Refuses a ZFS_POOL that isn't a ttmcss-test-* name, before anything runs and again before the destroy.
##		- 2026-11-18: Waits for Paper to log the stop-off line. Long output goes to a file before its grep. A failed boot check stops the server and turns the unit off.
##		- 2026-11-19: A replaced build is kept in /var/lib/tt-minecraftd/old-builds.
##		- 2026-11-20: A firewall counter or line number that isn't a number fails its check.
##		- 2026-11-22: A check that expects a refusal, or no match, fails unless the look itself
##		  worked: the container or server process is there, zfs list and nft ran, the file was read.
##		- 2026-11-22: retire-build keeps a copy and leaves the build in place.
