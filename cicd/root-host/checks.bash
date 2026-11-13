#!/bin/bash
# shellcheck disable=SC2329 # check calls each one by name
# The root steps of the hand tests, run as the operator inside the root-host
# container. root-host.bash copies this in and runs it. Each check prints
# PASS or FAIL with its ID; later ones lean on earlier ones.
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

say() { printf '%s\n' "${*}"; }
has() { grep -q -- "${1}" "${2}" || { say "no '${1}' in ${2}"; return 1; }; }
counter() { sudo nft -j list counter inet tt_minecraftd "${1}" | python3 -c 'import json,sys; print([c["counter"]["packets"] for c in json.load(sys.stdin)["nftables"] if "counter" in c][0])'; }
gateway() { ip -4 route show default | awk '{print $3; exit}'; }
# If cmd misses its answer, show what the log had.
answered() { has 'players online' cmd.out || { say "log:"; sudo tail -15 "${log}"; return 1; }; }

fNewInstall() {
	# Install Podman, go ahead, accept the EULA.
	printf 'y\ny\ny\n' | ./tt-minecraftd setup --set heapMax=1GB | tee setup.out | grep -E '^(FIXED|FAILED|OK)' || true
	has 'repair --check does' setup.out || return 1
	! grep -q '^FAILED' setup.out || return 1
	id minecraftd && id -nG op | grep -qw minecraftd || return 1
	[[ "$(stat -c %U:%G "${dir}")" == op:minecraftd ]] || return 1
	[[ "$(readlink "${dir}/paper.jar")" == paper-*.jar ]] || return 1
	has 'eula=true' "${dir}/eula.txt" && has 'white-list=true' "${dir}/server.properties" || return 1
	[[ "$(grep -c 'sha256:' /etc/tt-minecraftd/plugins.shcl)" -eq 3 ]] || return 1
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
	! grep -q 'not a TTY' start.out || return 1
	sudo podman ps --format '{{.Names}}' | grep -qx tt-minecraftd
}

fJarsInContainer() {
	# Each must fail.
	! sudo podman exec tt-minecraftd sh -c "echo x >> ${dir}/paper.jar" || return 1
	! sudo podman exec tt-minecraftd mv "${dir}/plugins/Geyser-Spigot.jar" "${dir}/plugins/x.jar"
}

fStatusPodman() { tt-minecraftd status | tee /dev/stderr | grep -q 'running in podman mode, as minecraftd'; }

fCmd() { tt-minecraftd cmd list | tee cmd.out; answered; }

fConsole() {
	local word="console-${RANDOM}"
	python3 "${here}/console.py" "say ${word}" || return 1
	has "${word}" "${log}" && tt-minecraftd status | grep -q 'running in'
}

fStun() {
	# Geyser's STUN lookup gets an answer, and nothing of the server's own hit the loopback drop.
	local i
	for ((i = 0; i < 40; i++)); do grep -q 'STUN discovery through .* maps this host' "${log}" && break; sleep 1; done
	has 'maps this host' "${log}" || return 1
	[[ "$(counter out_local)" -eq 0 ]] || { say "out_local $(counter out_local)"; return 1; }
}

fFirewallOut() {
	local t was
	for t in out_local:127.0.0.1:80 out_lan:"$(gateway)":80 out_web:1.1.1.1:22; do
		was="$(counter "${t%%:*}")"
		! sudo -u minecraftd curl -s -m 3 "http://${t#*:}/" > /dev/null || { say "reached ${t#*:}"; return 1; }
		[[ "$(counter "${t%%:*}")" -gt "${was}" ]] || { say "${t%%:*} didn't count"; return 1; }
	done
	sudo -u minecraftd curl -s -m 10 -o /dev/null https://fill.papermc.io/v3/projects/paper
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

fRefuseChanged() {
	local jar
	jar="${dir}/$(readlink "${dir}/paper.jar")"
	cp "${jar}" jar.keep && echo x >> "${jar}" || return 1
	! tt-minecraftd start > start.out 2>&1
	cp jar.keep "${jar}" && has 'changed since it was approved' start.out
}

fNativeStart() {
	tt-minecraftd start --native || return 1
	systemctl show tt-minecraftd-server -p ProtectSystem,ProtectHome,PrivateTmp,PrivateDevices,NoNewPrivileges,ReadOnlyPaths > unit.out
	has 'ProtectSystem=strict' unit.out && has 'NoNewPrivileges=yes' unit.out && has 'PrivateDevices=yes' unit.out || return 1
	has 'ReadOnlyPaths=.*Geyser-Spigot.jar' unit.out || return 1
	[[ "$(stat -c %U "$(readlink -f "${dir}/paper.jar")")" == root ]] || return 1
	# As the server, in its own mount view.
	local as
	as=(sudo nsenter -t "$(pgrep -u minecraftd -f 'paper.*\.jar' | head -1)" -m -S "$(id -u minecraftd)" -G "$(id -g minecraftd)")
	! "${as[@]}" sh -c "echo x >> ${dir}/paper.jar" || return 1
	! "${as[@]}" mv "${dir}/plugins/Geyser-Spigot.jar" "${dir}/plugins/x.jar"
}

fNativeFollow() {
	# runMode still says podman, as after start --native.
	tt-minecraftd status | grep -q 'running in native mode' || return 1
	tt-minecraftd cmd list | tee cmd.out
	answered && has 'runs in native mode' cmd.out || return 1
	fConsole
}

fNativeStop() {
	tt-minecraftd stop --warn 10 | tee stop.out || return 1
	has 'runs in native mode' stop.out || return 1
	! systemctl is-active tt-minecraftd-server && [[ "$(stat -c %U "${dir}/world")" == op ]]
}

fBoot() {
	echo 'startAtBoot: true' | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
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
		sudo journalctl -u tt-minecraftd.service | tail -20
		return 1
	fi
	tt-minecraftd status | grep -q 'running in podman mode' || return 1
	tt-minecraftd stop --warn 2 | tee boot-stop.out || return 1
	sudo grep -q 'Saving chunks' "${log}" || { say "no save in the log"; return 1; }
	if systemctl is-active tt-minecraftd.service || [[ "$(stat -c %U "${dir}/world")" != op ]]; then
		sudo journalctl -u tt-minecraftd.service | tail -20
		return 1
	fi
	sudo sed -i '/^startAtBoot:/d' /etc/tt-minecraftd/config.shcl
	tt-minecraftd repair | tee boot-off.out | grep -E '^(FIXED|FAILED)'
	has 'disabled and removed' boot-off.out && [[ ! -e /etc/systemd/system/tt-minecraftd.service ]]
}

fAdopt() {
	sudo mv /etc/tt-minecraftd/config.shcl /etc/tt-minecraftd/plugins.shcl "${HOME}/" || return 1
	sudo mkdir -p /srv/mc && sudo cp -a "${dir}" /srv/mc/adopted && sudo chown -R root:root /srv/mc/adopted || return 1
	printf 'y\n' | tt-minecraftd setup --adopt /srv/mc/adopted --set heapMax=1GB | tee adopt.out | grep -E '^(FIXED|FAILED)'
	has 'serverDir: /srv/mc/adopted' /etc/tt-minecraftd/config.shcl || return 1
	[[ "$(stat -c %U:%G /srv/mc/adopted/world)" == op:minecraftd ]] || return 1
	[[ "$(grep -n 'sha256: ' adopt.out | head -1 | cut -d: -f1)" -lt "$(grep -n 'Go ahead' adopt.out | cut -d: -f1)" ]] || return 1
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
	! sudo nft list table inet tt_minecraftd > /dev/null 2>&1 && ! systemctl is-enabled tt-minecraftd-firewall.service
}

fAgentHandOff() {
	# A jar with no source, and an agent that has nothing for it.
	printf 'thing' > /srv/mc/adopted/plugins/Thing.jar || return 1
	printf 'y\n' | tt-minecraftd repair --approve-plugins > approve.out || { cat approve.out; return 1; }
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
	! grep -q 'paper-.*update agent' agent.out || return 1
	# So are Geyser's and Floodgate's, through their GeyserMC source.
	! grep -qi 'geyser-spigot.jar: no answer\|floodgate-spigot.jar: no answer' agent.out || return 1
	! compgen -G '/tmp/tt-minecraftd-agent-*' || { say "the answer file is left over"; return 1; }
	sudo tt-minecraftd update --plugins --dry-run < /dev/null | tee agent-root.out
	has "isn't asked when update starts as root" agent-root.out
}

# ZFS: a throwaway file-backed pool, from root-host.bash --zfs. It lives in the
# host's kernel, so it's destroyed on the way out whatever happens.
pool="${ZFS_POOL:-}"
ds="${pool}/mc"
snaps() { sudo zfs list -H -o name -t snapshot -d 1 "${ds}" | sed 's/.*@//'; }
# The pool's history lines for one zfs subcommand, with who ran it.
hist() { sudo zpool history -l "${pool}" | grep -E "^[^ ]+ zfs ${1} " || true; }
newest() { snaps | grep -E "_${1}\$" | tail -1; }

fZfsGone() {
	[[ -n "${pool}" ]] && sudo zpool list -H -o name "${pool}" > /dev/null 2>&1 || return 0
	tt-minecraftd stop > /dev/null 2>&1
	sudo zpool destroy -f "${pool}" && echo "destroyed pool ${pool}"
}

fZfsPool() {
	local file="${ZFS_FILE:?}"
	[[ "${pool}" == ttmcss-test-* && "${file}" == */tmp/*/zpool.img ]] || { say "bad pool ${pool} or file ${file}"; return 1; }
	# The run folder is the host user's, not op's.
	sudo truncate -s 512M "${file}" || return 1
	sudo zpool create -o cachefile=none -m none "${pool}" "${file}" || return 1
	# The server's tree moves onto the dataset, as on a real host.
	sudo mv "${dir}" "${dir}.pre-zfs" && sudo zfs create -o mountpoint="${dir}" "${ds}" || return 1
	sudo cp -a "${dir}.pre-zfs/." "${dir}/" && sudo chown --reference="${dir}.pre-zfs" "${dir}" && sudo chmod --reference="${dir}.pre-zfs" "${dir}" || return 1
	printf 'zfsDataset: %s\n' "${ds}" | sudo tee -a /etc/tt-minecraftd/config.shcl > /dev/null
	[[ -z "$(snaps)" ]]
}

fZfsStart() {
	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
	local s
	s="$(newest pre_tt-minecraftd-start)"
	[[ "${s}" =~ ^[0-9]{8}-[0-9]{6}_pre_tt-minecraftd-start$ ]] || { say "no start snapshot"; snaps; return 1; }
	# No zfs allow yet, so it went through sudo.
	hist snapshot | tail -1 | tee /dev/stderr | grep -qF "@${s} [user 0 "
}

fZfsStop() {
	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
	local s owner
	s="$(newest post_tt-minecraftd-stop)"
	[[ -n "${s}" ]] || { say "no stop snapshot"; snaps; return 1; }
	has 'Saving worlds' "${log}" || return 1
	# Taken once the worlds were saved and before the trees went back. A clone,
	# since .zfs automounts from the host's mount namespace.
	sudo zfs clone -o mountpoint=/mnt/look -o readonly=on "${ds}@${s}" "${pool}/look" || return 1
	sudo cmp "${dir}/world/level.dat" /mnt/look/world/level.dat && owner="$(sudo stat -c %U /mnt/look/world)"
	sudo zfs destroy "${pool}/look" || return 1
	[[ "${owner:-}" == minecraftd ]] || { say "snapshot's world is ${owner:-unreadable}, not the server's"; return 1; }
	[[ "$(stat -c %U "${dir}/world")" == op ]]
}

fZfsPrune() {
	# Old ones of the tool's, and others it must leave. By the schedule, only
	# 20250102-01 to -04 go: the first, and the last of 2025-01-02's day, week
	# and month stay.
	local h gone=() n
	for h in 01 02 03 04; do gone+=("20250102-${h}0000_pre_tt-minecraftd-start"); done
	# One fs per zfs snapshot.
	for n in "${gone[@]}" 20250102-050000_pre_tt-minecraftd-start 20250101-000000_post_tt-minecraftd-stop keepme 20250102-030000_pre_tt-minecraftd-start-x; do
		sudo zfs snapshot "${ds}@${n}" || return 1
	done
	hist destroy > before.out
	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
	hist destroy | grep -vxF -f before.out | tee destroy.out
	[[ "$(wc -l < destroy.out)" -eq 1 ]] || { say "not one destroy"; return 1; }
	has "zfs destroy -- ${ds}@$(IFS=,; echo "${gone[*]}") " destroy.out || return 1
	snaps | tee kept.out
	for n in 20250101-000000_post_tt-minecraftd-stop 20250102-050000_pre_tt-minecraftd-start keepme 20250102-030000_pre_tt-minecraftd-start-x; do
		grep -qx "${n}" kept.out || { say "${n} went"; return 1; }
	done
	for n in "${gone[@]}"; do ! grep -qx "${n}" kept.out || { say "${n} stayed"; return 1; }; done
	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
}

fZfsAllow() {
	local s p me
	me="[user $(id -u) "
	sudo zfs allow -u op snapshot,destroy,mount "${ds}" || return 1
	hist destroy > before.out
	tt-minecraftd start < /dev/null > start.out 2>&1 || { cat start.out; return 1; }
	tt-minecraftd stop > stop.out 2>&1 || { cat stop.out; return 1; }
	s="$(newest pre_tt-minecraftd-start)" p="$(newest post_tt-minecraftd-stop)"
	hist snapshot | grep -F -e "@${s} " -e "@${p} " | tee allow.out
	[[ "$(grep -cF "${me}" allow.out)" -eq 2 ]] || { say "history:"; sudo zpool history -l "${pool}" | tail -12; return 1; }
	# Each prune drops an older one of today's, as op too.
	hist destroy | grep -vxF -f before.out | tee destroy.out
	[[ -s destroy.out ]] || { say "no destroy"; return 1; }
	! grep -vqF "${me}" destroy.out
}

cd "${HOME}" || exit 1
if [[ "${1:-}" != after-login ]]; then
	check df680 "setup installs from scratch" fNewInstall
	check df68j "native start in setup's login says to log in again" fSameLogin
	# The group setup added counts from the next login, as setup says.
	exec sudo -u op TERM="${TERM:-xterm}" ZFS_POOL="${pool}" ZFS_FILE="${ZFS_FILE:-}" bash "${0}" after-login "${failed}"
fi
failed="${2:-0}"
if [[ -n "${pool}" ]]; then
	trap fZfsGone EXIT
	check df780 "a throwaway pool holds the server's tree" fZfsPool
	if ((failed == 0)); then
		check df781 "start snapshots the dataset" fZfsStart
		check df782 "stop snapshots after the save, before the hand-back" fZfsStop
		check df783 "a prune destroys only the tool's own, in one destroy" fZfsPrune
		check df784 "with zfs allow the operator snapshots and prunes without sudo" fZfsAllow
	fi
	fZfsGone
	exit "${failed}"
fi
check df681 "firewall table survives an nftables restart" fNftRestart
check df682 "rootful Podman start without the docker group" fPodmanStart
check df683 "jars read-only in the container" fJarsInContainer
check df684 "status without sudo names podman mode" fStatusPodman
check df685 "cmd list in podman mode" fCmd
check df686 "console line and Ctrl-C detach in podman mode" fConsole
check df687 "Geyser STUN answers behind the firewall" fStun
check df688 "firewall drops and counts the account's local, LAN, and non-web connections" fFirewallOut
check df689 "stop --warn 10 saves and hands the trees back" fStopWarn
check df68a "start refuses a changed jar" fRefuseChanged
check df68b "native start, sandbox, and locked jars" fNativeStart
check df68c "status, cmd, and console follow a native server" fNativeFollow
check df68d "stop follows a native server" fNativeStop
check df78x "startAtBoot: repair enables the unit, and start and stop go through it" fBoot
check df68e "setup --adopt a copy of the tree" fAdopt
check df68f "firewall off removes the unit and table" fFirewallOff
check df76n "update asks the agent as the operator, and hands its answer through sudo" fAgentHandOff
exit "${failed}"
