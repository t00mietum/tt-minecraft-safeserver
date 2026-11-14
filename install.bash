#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- One-line installer for tt-minecraftd on Debian and its derivatives. Finds the newest
##		  release on GitHub, checks the binary against the release's SHA256SUMS, puts it in
##		  /usr/local/bin, and runs `tt-minecraftd setup`, which does the rest.
##		- States its plan and asks first. Re-running it is safe: a current binary is left alone,
##		  and setup on an existing install runs repair and update.
##	Syntax:
##		bash <(curl -fsSL https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install.bash) [options] [-- setup options]
##		  See fUsage below.
##	Exit: 0 done, 1 refused, aborted or failed, 2 bad options.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under The MIT License (MIT). Full text at:
##		https://mit-license.org/
##	SPDX-License-Identifier: MIT


set -Eeuo pipefail

installerVersion="1.0.0"
exeName="tt-minecraftd"
repo="t00mietum/tt-minecraft-safeserver"
apiUrl="https://api.github.com/repos/${repo}/releases?per_page=100"
dlBase="https://github.com/${repo}/releases/download"
## setup and the boot unit both expect it here.
destDir="/usr/local/bin"
osRelease="/etc/os-release"
ttyDev="/dev/tty"

fEcho(){       printf '[ %s ]\n' "$*"; }
fEcho_Clean(){ printf '%s\n' "$*"; }
fDie(){        printf '[ ERROR: %s ]\n' "$*" >&2; echo >&2; exit 1; }

## Spelled out rather than read from the header, since under `bash <(curl ...)` there's no file to read.
fUsage(){ cat <<'EOF'
Usage:
	bash <(curl -fsSL https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install.bash) [options] [-- setup options]
	bash <(wget -qO- https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install.bash) [options] [-- setup options]
Options:
	--release stable|dev ... stable = newest release (default), dev = newest including pre-releases
	-y|--yes ............... skip this installer's prompt (setup still asks its own questions)
	--version .............. print this installer's version
	-h|--help .............. this help
	-- ..................... anything after it goes to setup, such as: -- --yes --accept-eula
Notes:
	- Options take "--name value" or "--name=value".
	- Installs to /usr/local/bin/tt-minecraftd, through sudo when needed, then runs tt-minecraftd setup.
	- Debian and its derivatives only for now. The CPU is detected; builds exist for amd64 and arm64.
	- With no stable release yet, stable takes the newest pre-release.
	- Needs curl or wget, and sha256sum.
EOF
}

## Tag, then 1 for a pre-release. The API puts one field per line, and drafts aren't public, but skip them anyway.
fListReleases(){
	awk '
		/^ *"tag_name": *"/ { tag = $0; sub(/^ *"tag_name": *"/, "", tag); sub(/".*/, "", tag) }
		/^ *"draft": *true/ { tag = "" }
		/^ *"prerelease": *(true|false)/ { if (tag != "") print tag " " ($0 ~ /true/ ? 1 : 0); tag = "" }
	'
}

## Newest by version, not by list order. A pre-release sorts below its final (1.0.0-beta.2 < 1.0.0)
## and digit runs compare as numbers (alpha.2 < alpha.10), which sort -V gets wrong.
fNewest(){
	awk '{
		ver = $1; sub(/^v/, "", ver); pre = ""
		if (index(ver, "-")) { pre = substr(ver, index(ver, "-") + 1); ver = substr(ver, 1, index(ver, "-") - 1) }
		n = split(ver, part, ".")
		key = sprintf("%08d%08d%08d", part[1], (n > 1 ? part[2] : 0), (n > 2 ? part[3] : 0))
		if (pre == "") { key = key "1" } else {
			key = key "0"
			while (match(pre, /[0-9]+/)) { key = key substr(pre, 1, RSTART - 1) sprintf("%08d", substr(pre, RSTART, RLENGTH)); pre = substr(pre, RSTART + RLENGTH) }
			key = key pre
		}
		print key " " $1
	}' | LC_ALL=C sort -r | awk 'NR == 1 { print $2 }'
}

## fFetch URL [OUTFILE]. stdout with no OUTFILE.
fFetch(){
	local url="$1" out="${2:--}"
	if [[ "${fetcher}" == curl ]]; then curl -fsSL -o "${out}" "${url}"
	else wget -q -O "${out}" "${url}"; fi
}

## Ctrl-D, or no terminal at all, reads as no.
fAsk(){
	local answer=""
	if [[ -t 0 ]]; then read -r -p "$1 [y/N] " answer || true
	elif { : < "${ttyDev}"; } 2> /dev/null; then printf '%s [y/N] ' "$1"; read -r answer < "${ttyDev}" || true; echo
	fi
	case "${answer}" in y|Y|yes|Yes|YES) return 0 ;; *) return 1 ;; esac
}

fMain(){
	local release="stable" assumeYes=0 setupArgs=()
	while (($#)); do case "$1" in
		--release)   release="${2:-}"; shift; (($#)) && shift ;;
		--release=*) release="${1#*=}"; shift ;;
		-y|--yes)    assumeYes=1; shift ;;
		--version)   echo "${exeName} installer v${installerVersion}"; return 0 ;;
		-h|--help)   fUsage; return 0 ;;
		--)          shift; setupArgs=("$@"); break ;;
		*) printf '[ ERROR: unknown option: %s (try --help) ]\n' "$1" >&2; return 2 ;;
	esac; done
	[[ "${release}" == stable || "${release}" == dev ]] || { printf '[ ERROR: --release takes stable or dev ]\n' >&2; return 2; }

	echo

	## What this box is.
	[[ "$(uname -s)" == Linux ]] || fDie "${exeName} runs on Linux only"
	local osId="" osLike=""
	if [[ -r "${osRelease}" ]]; then
		osId="$(awk -F= '$1 == "ID" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
		osLike="$(awk -F= '$1 == "ID_LIKE" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
	fi
	case "${osId}" in
		debian) : ;;
		nixos) fDie "NixOS isn't supported yet. Its module is planned for a later release." ;;
		*) case " ${osLike} " in
			*" debian "*) : ;;
			*) fDie "only Debian and its derivatives are supported for now, since setup installs packages through apt (this is '${osId:-unknown}')" ;;
		esac ;;
	esac
	local arch
	case "$(uname -m)" in
		x86_64|amd64)  arch="amd64" ;;
		aarch64|arm64) arch="arm64" ;;
		*) fDie "no build for CPU '$(uname -m)'. Builds exist for amd64 and arm64." ;;
	esac

	if command -v curl > /dev/null; then fetcher=curl
	elif command -v wget > /dev/null; then fetcher=wget
	else fDie "curl or wget is needed"; fi
	command -v sha256sum > /dev/null || fDie "sha256sum is needed"

	## Which release.
	fEcho "Looking up the newest ${release} release"
	local listJson releases tag note=""
	## The list, not releases/latest, which leaves out pre-releases.
	listJson="$(fFetch "${apiUrl}")" || fDie "couldn't reach the GitHub API (offline, or rate-limited?)"
	releases="$(fListReleases <<< "${listJson}" || true)"
	if [[ "${release}" == stable ]]; then
		tag="$(awk '$2 == 0' <<< "${releases}" | fNewest || true)"
		if [[ -z "${tag}" ]]; then
			tag="$(fNewest <<< "${releases}" || true)"
			note=", a pre-release, since there's no stable release yet"
		fi
	else
		tag="$(fNewest <<< "${releases}" || true)"
	fi
	[[ -n "${tag}" ]] || fDie "there's no ${release} release to install yet"

	local asset="${exeName}-linux-${arch}"
	local url="${dlBase}/${tag}/${asset}"
	local destPath="${destDir}/${exeName}"
	local haveVer="" current=0 useSudo=0
	if [[ -x "${destPath}" ]]; then
		## "tt-minecraftd v1.0.0-alpha.1 build df7q6 (2026-11-12)", then the copyright.
		haveVer="$("${destPath}" --version 2> /dev/null || true)"; haveVer="${haveVer%%$'\n'*}"; haveVer="${haveVer#"${exeName} "}"; haveVer="${haveVer%% *}"
		[[ "${haveVer}" != "${tag}" ]] || current=1
	fi
	if ((! current)) && [[ "$(id -u)" != 0 ]] && ! { [[ -d "${destDir}" && -w "${destDir}" ]] && { [[ ! -e "${destPath}" ]] || [[ -w "${destPath}" ]]; }; }; then
		useSudo=1
		command -v sudo > /dev/null || fDie "${destDir} needs root to write to, and there's no sudo. Run this as root."
	fi

	## The plan.
	fEcho "Plan"
	fEcho_Clean "    Release ....: ${tag}${note}"
	if ((current)); then
		fEcho_Clean "    Install ....: ${destPath} is already ${tag}, so it's left as is"
	else
		fEcho_Clean "    Download ...: ${url}"
		fEcho_Clean "    Check ......: SHA-256 against the release's SHA256SUMS"
		local how=""; if ((useSudo)); then how=", through sudo"; fi
		fEcho_Clean "    Install ....: ${destPath}${haveVer:+ (replacing ${haveVer})}${how}"
	fi
	fEcho_Clean "    Then .......: ${exeName} setup${setupArgs[0]+ ${setupArgs[*]}}, which shows its own plan and asks again"
	echo
	if ((! assumeYes)); then
		fAsk "Go ahead?" || { fEcho "Stopped. Nothing was changed."; echo; return 1; }
		echo
	fi

	if ((! current)); then
		local tmpRoot work
		tmpRoot="$(cd "${TMPDIR:-/tmp}" && pwd -P)" || fDie "no temp folder at ${TMPDIR:-/tmp}"
		work="$(mktemp -d "${tmpRoot}/tt-minecraftd-install.XXXXXX")" || fDie "couldn't make a work folder in ${tmpRoot}"
		__installWork="${work}"; __installTmpRoot="${tmpRoot}"; __installAsset="${asset}"
		trap fWorkGone EXIT

		fEcho "Downloading ${asset} ${tag}"
		fFetch "${url}" "${work}/${asset}" || fDie "download failed: ${url}"
		fFetch "${dlBase}/${tag}/SHA256SUMS" "${work}/SHA256SUMS" || fDie "download failed: SHA256SUMS for ${tag}"

		fEcho "Checking SHA-256"
		local want got
		want="$(awk -v f="${asset}" '$2 == f || $2 == "*" f { print $1; exit }' "${work}/SHA256SUMS")"
		[[ -n "${want}" ]] || fDie "SHA256SUMS for ${tag} doesn't list ${asset}"
		got="$(sha256sum < "${work}/${asset}")"; got="${got%% *}"
		[[ "${got}" == "${want}" ]] || fDie "${asset} doesn't match SHA256SUMS (listed ${want}, got ${got}). Nothing was installed."

		fEcho "Installing ${destPath}"
		if ((useSudo)); then
			sudo install -d -m 0755 "${destDir}"
			sudo install -m 0755 "${work}/${asset}" "${destPath}"
		else
			install -d -m 0755 "${destDir}"
			install -m 0755 "${work}/${asset}" "${destPath}"
		fi
		local nowVer; nowVer="$("${destPath}" --version 2> /dev/null || true)"
		fEcho "Installed: ${nowVer%%$'\n'*}"
		echo
	fi

	fEcho "Running ${exeName} setup"
	## setup asks questions, so give it the terminal when this script came in on a pipe.
	if [[ ! -t 0 ]] && { : < "${ttyDev}"; } 2> /dev/null; then
		"${destPath}" setup ${setupArgs[0]+"${setupArgs[@]}"} < "${ttyDev}" || fDie "setup didn't finish. Run '${exeName} setup' again once that's sorted."
	else
		"${destPath}" setup ${setupArgs[0]+"${setupArgs[@]}"} || fDie "setup didn't finish. Run '${exeName} setup' again once that's sorted."
	fi
	echo
}

## Takes out only the 2 files it fetched and the folder it made, under the temp folder it named.
## A failed cleanup doesn't change how the run ended.
fWorkGone(){
	local w="${__installWork:-}" r="${__installTmpRoot:-}"
	[[ -n "${w}" && -n "${r}" ]] || return 0
	case "${w}" in
		"${r}"/tt-minecraftd-install.?*)
			if [[ -d "${w}" && ! -L "${w}" && "${w}" != *..* ]]; then
				rm -f -- "${w}/${__installAsset:-x}" "${w}/SHA256SUMS" || true
				rmdir -- "${w}" || true
			fi ;;
	esac
}

## Sourced by its test, which sets the paths above before calling fMain.
if ! (return 0 2> /dev/null); then fMain "$@"; fi


##	History:
##		- 2026-11-14: Created.
