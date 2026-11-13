#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Records the README's CLI demo: a short session with the built tool, typed out and rendered
##		  to a gif. For cicd.bash's demo step, which runs it as root in a throwaway container with
##		  no network, since it writes a made-up install under /etc and /srv.
##		- Everything in it is made up: the players, the host name, and the plugin. ss and systemctl
##		  and nft are stand-ins for the sockets, the boot unit and the firewall counters.
##		- No window and no display: each screen is drawn with ImageMagick and joined with gifsicle.
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
for t in convert gifsicle; do command -v "${t}" >/dev/null 2>&1 || { echo "demo: needs ${t}" >&2; exit 1; }; done
mkdir -p -- "${work}"
[[ -z "$(ls -A -- "${work}")" ]] || { echo "demo: ${work} isn't empty" >&2; exit 2; }
## Handed back on failure too, or the caller can't remove its own run folder.
trap '[[ -z "${OWNER:-}" ]] || chown -R -- "${OWNER}" "${work}" "${outGif}" 2>/dev/null || true' EXIT

## Screen size in characters, and how it's drawn.
cols=88; rows=26; font="DejaVu-Sans-Mono"; pt=15; fg="#c5c8c6"; bg="#1d1f21"
cellW=9; cellH=19; padX=14; padY=12
prompt='$ '

## The made-up install.
install -D -m 0755 -- "${exe}" /usr/local/bin/tt-minecraftd
mkdir -p /etc/tt-minecraftd /srv/tt-minecraftd/server/logs /srv/tt-minecraftd/server/plugins /srv/tt-minecraftd/server/world "${work}/bin"
cat > /etc/tt-minecraftd/config.shcl <<'EOF'
runMode: native
javaBin: /usr/local/bin/java
heapMax: 4GB
memCap: 6GB
bedrock: false
EOF
printf 'demo jar\n' > /srv/tt-minecraftd/server/plugins/LuckyChest.jar
printf 'demo jar\n' > /srv/tt-minecraftd/server/paper-26.3-50.jar
ln -s paper-26.3-50.jar /srv/tt-minecraftd/server/paper.jar
cat > /etc/tt-minecraftd/plugins.shcl <<EOF
jar: plugins/LuckyChest.jar
	sha256: $(sha256sum < /srv/tt-minecraftd/server/plugins/LuckyChest.jar | cut -d' ' -f1)
jar: paper-26.3-50.jar
	sha256: $(sha256sum < /srv/tt-minecraftd/server/paper-26.3-50.jar | cut -d' ' -f1)
EOF
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

## The session: each command as typed, then its real output.
session=(
	"tt-minecraftd --version"
	"tt-minecraftd status"
	"tt-minecraftd start --dry-run"
)

## One screen per frame: the last rows lines of the session so far, plus a cursor.
frames="${work}/frames"; mkdir -p -- "${frames}"
declare -i nFrame=0
screen=()
fShot(){ ## DELAY_CS [CURSOR_LINE]: draw the screen as it is now, shown for DELAY_CS hundredths
	local delay="$1" last="${2:-}" f txt lines
	printf -v f '%s/%05d' "${frames}" "${nFrame}"
	lines=("${screen[@]}")
	[[ -z "${last}" ]] || lines+=("${last}")
	((${#lines[@]} <= rows)) || lines=("${lines[@]: -rows}")
	printf -v txt '%s\n' "${lines[@]}"
	## -annotate reads \ and % as escapes, and a leading @ as a file name.
	txt="${txt//\\/\\\\}"; txt="${txt//%/%%}"; [[ "${txt}" != @* ]] || txt="\\${txt}"
	convert -size "$((cols * cellW + 2 * padX))x$((rows * cellH + 2 * padY))" "xc:${bg}" -font "${font}" -pointsize "${pt}" -fill "${fg}" \
		-interline-spacing 2 -gravity northwest -annotate "+${padX}+${padY}" "${txt}" -depth 8 "${f}.gif" 2> "${f}.err" \
		|| { cat "${f}.err" >&2; return 1; }
	echo "${delay}" > "${f}.delay"
	nFrame+=1
}

export PATH="${work}/bin:${PATH}" HOME="${work}/home" TERM=dumb COLUMNS="${cols}"
mkdir -p -- "${HOME}"
cd "${HOME}"
fShot 80 "${prompt}_"
for cmd in "${session[@]}"; do
	## Typed a few characters at a time, which keeps the frame count down.
	for ((i = 3; i < ${#cmd}; i += 3)); do fShot 7 "${prompt}${cmd:0:i}_"; done
	fShot 50 "${prompt}${cmd}_"
	screen+=("${prompt}${cmd}")
	out="$(bash -c "${cmd}" 2>&1 || true)"
	while IFS= read -r line; do screen+=("${line:0:cols}"); done <<< "${out}"
	fShot 350 "${prompt}_"
done
fShot 200 "${prompt}_"

args=()
for f in "${frames}"/*.gif; do args+=("-d$(cat "${f%.gif}.delay")" "${f}"); done
gifsicle --no-warnings --loop -O3 --colors 16 "${args[@]}" -o "${outGif}"
echo "demo: ${nFrame} frames, $(stat -c %s "${outGif}") bytes -> ${outGif}"


##	History:
##		- 2026-11-13: Created.
