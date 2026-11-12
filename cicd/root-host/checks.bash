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

fAdopt() {
	sudo mv /etc/tt-minecraftd/config.shcl /etc/tt-minecraftd/plugins.shcl "${HOME}/" || return 1
	sudo mkdir -p /srv/mc && sudo cp -a "${dir}" /srv/mc/adopted && sudo chown -R root:root /srv/mc/adopted || return 1
	printf 'y\n' | tt-minecraftd setup --adopt /srv/mc/adopted --set heapMax=1GB | tee adopt.out | grep -E '^(FIXED|FAILED)'
	has 'serverDir: /srv/mc/adopted' /etc/tt-minecraftd/config.shcl || return 1
	[[ "$(stat -c %U:%G /srv/mc/adopted/world)" == op:minecraftd ]] || return 1
	[[ "$(grep -n 'sha256: ' adopt.out | head -1 | cut -d: -f1)" -lt "$(grep -n 'Go ahead' adopt.out | cut -d: -f1)" ]] || return 1
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
	! compgen -G '/tmp/tt-minecraftd-agent-*' || { say "the answer file is left over"; return 1; }
	sudo tt-minecraftd update --plugins --dry-run < /dev/null | tee agent-root.out
	has "isn't asked when update starts as root" agent-root.out
}

cd "${HOME}" || exit 1
if [[ "${1:-}" != after-login ]]; then
	check df680 "setup installs from scratch" fNewInstall
	check df68j "native start in setup's login says to log in again" fSameLogin
	# The group setup added counts from the next login, as setup says.
	exec sudo -u op TERM="${TERM:-xterm}" bash "${0}" after-login "${failed}"
fi
failed="${2:-0}"
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
check df68e "setup --adopt a copy of the tree" fAdopt
check df68f "firewall off removes the unit and table" fFirewallOff
check df76n "update asks the agent as the operator, and hands its answer through sudo" fAgentHandOff
exit "${failed}"
