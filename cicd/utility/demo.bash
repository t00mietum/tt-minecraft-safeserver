#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Records the README's CLI demo: a short session with the built tool, typed out and rendered
##		  to a gif. For cicd.bash's demo step, which runs it as root in a throwaway container with
##		  no network, since it writes a made-up install under /etc and /srv.
##		- Everything in it is made up: the players, the host name, and the plugin. ss and systemctl
##		  and nft are stand-ins for the sockets, the boot unit and the firewall counters.
##		- demo/gen-demo-gif.py types and runs the steps in demo/scenario.toml and draws the gif,
##		  with Pillow and gifsicle, so no window or display is used. demo/script.txt is the plan.
##	Syntax:
##		demo.bash EXE WORK_DIR OUT_GIF
##		  WORK_DIR must be empty or missing. OWNER=UID:GID hands WORK_DIR and OUT_GIF to that account.
##	Exit: 0 the gif is there, 1 a step failed, 2 bad arguments or not in a throwaway container.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


set -Eeuo pipefail

(($# == 3)) && [[ "$1" == /?* && "$2" == /?* && "$3" == /?*.gif ]] || { sed -n '/^##	Syntax:/,/^##	Exit:/p' "${BASH_SOURCE[0]}" | sed 's/^##	\{0,1\}//' >&2; exit 2; }
exe="$1"; work="${2%/}"; outGif="$3"
## It writes into /etc and /srv, so only where those are thrown away after.
if ! [[ -f /.dockerenv || -f /run/.containerenv ]] || ((EUID != 0)); then echo "demo: runs only as root in a throwaway container" >&2; exit 2; fi
[[ -x "${exe}" ]] || { echo "demo: no program at ${exe}" >&2; exit 2; }
for t in python3 fc-match gifsicle; do command -v "${t}" >/dev/null 2>&1 || { echo "demo: needs ${t}" >&2; exit 1; }; done
mkdir -p -- "${work}"
[[ -z "$(ls -A -- "${work}")" ]] || { echo "demo: ${work} isn't empty" >&2; exit 2; }
## Handed back on failure too, or the caller can't remove its own run folder.
trap '[[ -z "${OWNER:-}" ]] || chown -R -- "${OWNER}" "${work}" "${outGif}" 2>/dev/null || true' EXIT

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

## The made-up install.
install -D -m 0755 -- "${exe}" /usr/local/bin/tt-minecraftd
mkdir -p /etc/tt-minecraftd /srv/tt-minecraftd/server/logs /srv/tt-minecraftd/server/plugins /srv/tt-minecraftd/server/world "${work}/bin"
cat > /etc/tt-minecraftd/config.shcl <<'EOF'
runMode: podman
heapMax: 4GB
memCap: 6GB
bedrock: false
EOF
printf 'demo jar\n' > /srv/tt-minecraftd/server/plugins/LuckyChest.jar
printf 'demo jar\n' > /srv/tt-minecraftd/server/paper-26.3-50.jar
ln -s paper-26.3-50.jar /srv/tt-minecraftd/server/paper.jar
cat > /srv/tt-minecraftd/server/whitelist.json <<'EOF'
[
  {"uuid": "6a1e3f0c-2b4d-4c8e-9f1a-0d3b5c7e9a11", "name": "PebbleFox"},
  {"uuid": "0b9d2e4f-6a8c-4e1b-8d3f-5a7c9e1b3d22", "name": "MossyTurtle"},
  {"uuid": "c4e6a8b0-1d3f-4a5c-9e7b-2f4d6a8c0e33", "name": "CloudBaker"}
]
EOF
cat > /srv/tt-minecraftd/server/ops.json <<'EOF'
[{"uuid": "6a1e3f0c-2b4d-4c8e-9f1a-0d3b5c7e9a11", "name": "PebbleFox", "level": 4, "bypassesPlayerLimit": false}]
EOF
cat > /srv/tt-minecraftd/server/server.properties <<'EOF'
motd=Saturday server
server-port=25565
white-list=false
online-mode=true
EOF
cat > /srv/tt-minecraftd/server/logs/latest.log <<'EOF'
[19:02:11] [Server thread/INFO]: PebbleFox joined the game
[19:40:57] [Server thread/INFO]: MossyTurtle joined the game
[21:15:03] [Server thread/INFO]: Stopping the server
EOF
groupadd -r minecraftd; useradd -r -g minecraftd -d /srv/tt-minecraftd -s /usr/sbin/nologin minecraftd
chown -R minecraftd:minecraftd /srv/tt-minecraftd
## Stand-ins for what a container doesn't have.
cat > "${work}/bin/ss" <<'EOF'
#!/bin/sh
EOF
cat > "${work}/bin/systemctl" <<'EOF'
#!/bin/sh
echo enabled
EOF
cat > "${work}/bin/nft" <<'EOF'
#!/bin/sh
c(){ printf '{"counter":{"family":"inet","table":"tt_minecraftd","name":"%s","packets":%s}}' "$1" "$2"; }
printf '{"nftables":[%s,%s,%s,%s,%s,%s]}\n' "$(c in_refused 41)" "$(c in_flood 3)" "$(c in_closed 12)" "$(c out_local 0)" "$(c out_lan 7)" "$(c out_web 2)"
EOF
chmod 0755 "${work}/bin/ss" "${work}/bin/systemctl" "${work}/bin/nft"

## The steps run as typed, with the stand-ins first on PATH. --bin is only for the scenario's
## {prog}, which it doesn't use, so the installed copy is what runs and what the output names.
export PATH="${work}/bin:${PATH}" TERM=dumb
env -u DISPLAY -u WAYLAND_DISPLAY python3 "${here}/demo/gen-demo-gif.py" --scenario "${here}/demo/scenario.toml" \
	--out "${outGif}" --bin "${exe}"


##	History:
##		- 2026-11-13: Created.
##		- 2026-11-14: Podman in the made-up config, since native mode is off.
##		- 2026-11-19: Drawn by demo/gen-demo-gif.py from demo/scenario.toml: typed by hand, 50 fps,
##		  a drawn window, and 3 s of black before the loop.
