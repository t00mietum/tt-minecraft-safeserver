#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- One-line installer for tt-minecraftd on Debian and its derivatives. Finds the newest
##		  release on GitHub, checks the binary against the release's SHA256SUMS, puts it in
##		  /usr/local/bin owned by root, and runs `tt-minecraftd setup`, which does the rest.
##		- A different build already in /usr/local/bin is moved to /var/lib/tt-minecraftd/old-builds,
##		  where the new build keeps the old ones GFS-rotated, rather than written over.
##		- States its plan and asks first. Re-running it is safe: a current binary is left alone,
##		  and setup on an existing install runs repair and update.
##		- Stops when the .deb or .rpm package is installed, since a copy in /usr/local/bin would hide
##		  the package's from then on. That one is updated through the package manager.
##		- With --any-distro it runs on other distros, and first checks that what setup would
##		  install through apt is already there.
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

installerVersion="1.2.1"
exeName="tt-minecraftd"
repo="t00mietum/tt-minecraft-safeserver"
apiUrl="https://api.github.com/repos/${repo}/releases?per_page=100"
dlBase="https://github.com/${repo}/releases/download"
## setup and the boot unit both expect it here.
destDir="/usr/local/bin"
## Where the .deb and .rpm put it.
pkgBin="/usr/bin/tt-minecraftd"
## Who has to own a current build. The test has no root, so it gives its own uid.
rootUid=0
## For the plan only. The new build's retire-build owns the folder.
oldDir="/var/lib/tt-minecraftd/old-builds"
osRelease="/etc/os-release"
ttyDev="/dev/tty"
## There only when systemd is what booted the box.
systemdDir="/run/systemd/system"
## Where setup looks for them, as repair does.
catatonitPaths="/usr/libexec/podman/catatonit /usr/bin/catatonit"
nftPaths="/usr/sbin/nft /sbin/nft"

fEcho(){       printf '[ %s ]\n' "$*"; }
fEcho_Clean(){ printf '%s\n' "$*"; }
fDie(){        { echo; printf '[ ERROR: %s ]\n' "$*"; echo; } >&2; exit 1; }
fBadArg(){     { echo; printf '[ ERROR: %s ]\n' "$*"; echo; } >&2; }
fLower(){      printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

## Spelled out rather than read from the header, since under `bash <(curl ...)` there's no file to read.
fUsage(){ echo; cat <<'EOF'
Usage:
	bash <(curl -fsSL https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install.bash) [options] [-- setup options]
	bash <(wget -qO- https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install.bash) [options] [-- setup options]
Options:
	--release stable|dev ... stable = newest release (default), dev = newest including pre-releases
	--any-distro ........... for a distro this doesn't support. Nothing gets installed through a
	                         package manager, so Podman and catatonit, or Docker, and nftables
	                         have to be there already.
	-y|--yes ............... skip this installer's prompt (setup still asks its own questions)
	--version .............. print this installer's version
	-h|--help .............. this help
	-- ..................... anything after it goes to setup, such as: -- --yes --accept-eula
Notes:
	- Options take "--name value" or "--name=value".
	- Installs to /usr/local/bin/tt-minecraftd, owned by root, through sudo unless run as root, then
	  runs tt-minecraftd setup. A different build there is moved to /var/lib/tt-minecraftd/old-builds.
	- Debian and its derivatives, or another distro with --any-distro. The CPU is detected; builds
	  exist for amd64 and arm64.
	- With no stable release yet, stable takes the newest pre-release.
	- Stops when the .deb or .rpm package is installed. Update that through the package manager.
	- Needs systemd, curl or wget, and sha256sum.
EOF
echo
}

## fAnyFile "PATH PATH...". Paths have no spaces.
fAnyFile(){
	local p
	for p in $1; do [[ -f "${p}" ]] && return 0; done
	return 1
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
## https only, redirects too, since the sums come over the same channel as the build. curl refuses the
## hop itself. wget's --https-only holds only for links in a recursive run, so its headers are read
## after, and a redirect to anything else fails the download, which then isn't used.
fFetch(){
	local url="$1" out="${2:--}"
	if [[ "${fetcher}" == curl ]]; then curl --proto '=https' --tlsv1.2 -fsSL -o "${out}" "${url}"; return; fi
	local heads rc=0
	{ heads="$(wget --https-only -q -S -O "${out}" "${url}" 2>&1 1>&3 3>&-)"; } 3>&1 || rc=$?
	## A relative Location keeps the scheme it came from.
	if printf '%s\n' "${heads}" | awk '{ l = tolower($0) } l ~ /^[ \t]*location:[ \t]*[a-z][a-z0-9+.-]*:/ && l !~ /^[ \t]*location:[ \t]*https:/ { bad = 1 } END { exit !bad }'; then
		fEcho "${url} redirected to somewhere that isn't https, so what came back isn't used" >&2
		return 1
	fi
	return "${rc}"
}

## Through sudo when fMain set useSudo.
fAsRoot(){
	if ((useSudo)); then sudo "$@"; else "$@"; fi
}

## Ctrl-D reads as no. The terminal echoes a typed answer, so only Ctrl-D needs the line ended.
fAsk(){
	local answer=""
	if [[ -t 0 ]]; then read -r -p "$1 [y/N] " answer || { answer=""; echo; }
	elif { : < "${ttyDev}"; } 2> /dev/null; then printf '%s [y/N] ' "$1"; read -r answer < "${ttyDev}" || { answer=""; echo; }
	else fEcho "There's no terminal to ask on. Add --yes to go ahead without asking."; return 1
	fi
	case "${answer}" in y|Y|yes|Yes|YES) return 0 ;; *) return 1 ;; esac
}

## The tag goes into download URLs, so a path in it would fetch from somewhere the plan doesn't name.
fTagOk(){
	[[ "$1" =~ ^[A-Za-z0-9._-]+$ && "$1" != *..* ]]
}

fMain(){
	local release="stable" assumeYes=0 anyDistro=0 setupArgs=()
	while (($#)); do case "$1" in
		--release)    (($# >= 2)) || { fBadArg "--release needs a value, stable or dev"; return 2; }; release="$(fLower "$2")"; shift 2 ;;
		--release=*)  release="$(fLower "${1#*=}")"; shift ;;
		--any-distro) anyDistro=1; shift ;;
		-y|--yes)     assumeYes=1; shift ;;
		--version)    echo "${exeName} installer v${installerVersion}"; return 0 ;;
		-h|--help)    fUsage; return 0 ;;
		--)           shift; setupArgs=("$@"); break ;;
		*) fBadArg "unknown option: $1 (try --help)"; return 2 ;;
	esac; done
	[[ "${release}" == stable || "${release}" == dev ]] || { fBadArg "--release takes stable or dev, not '${release}'"; return 2; }

	## What this box is.
	[[ "$(uname -s)" == Linux ]] || fDie "${exeName} runs on Linux only"
	## A copy in destDir comes first on PATH, so it would hide every package upgrade, and setup's repair moves it away.
	[[ ! -f "${pkgBin}" ]] || fDie "${exeName} is installed here from a package (${pkgBin}). Update it through the package manager, with the newer .deb or .rpm from the releases page. This script would put a second copy in ${destDir}, which would hide the package's."
	local osId="" osLike=""
	if [[ -r "${osRelease}" ]]; then
		osId="$(awk -F= '$1 == "ID" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
		osLike="$(awk -F= '$1 == "ID_LIKE" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
	fi
	## NixOS stays out even with --any-distro, since its /etc is built from its own config.
	case "${osId}" in
		debian) : ;;
		nixos) fDie "NixOS isn't supported yet. Its module is planned for a later release." ;;
		*) if ((! anyDistro)); then case " ${osLike} " in
			*" debian "*) : ;;
			*) fDie "only Debian and its derivatives are supported for now, since setup installs packages through apt (this is '${osId:-unknown}'). --any-distro skips this check, and then what setup needs has to be installed already." ;;
		esac; fi ;;
	esac
	[[ -d "${systemdDir}" ]] || fDie "${exeName} needs systemd, and it isn't running here"
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
	command -v stat > /dev/null || fDie "stat is needed"

	## What setup would get through apt. It takes Podman over Docker when both are there.
	if ((anyDistro)); then
		local missing=""
		if command -v podman > /dev/null; then
			fAnyFile "${catatonitPaths}" || command -v catatonit > /dev/null || missing="${missing}, catatonit (Podman's container init)"
		elif ! command -v docker > /dev/null; then
			missing="${missing}, Podman and catatonit, or Docker"
		fi
		fAnyFile "${nftPaths}" || missing="${missing}, nftables (for the firewall)"
		[[ -z "${missing}" ]] || fDie "--any-distro installs nothing, and this box is missing: ${missing#, }. Install it with this distro's package manager, then run this again."
	fi

	## Which release.
	echo
	fEcho "Looking up the newest ${release} release"
	local listJson releases tag note=""
	## The list, not releases/latest, which leaves out pre-releases.
	listJson="$(fFetch "${apiUrl}")" || fDie "couldn't reach the GitHub API (offline, or rate-limited?)"
	## A captive portal or proxy returns a page, which would read as no releases at all.
	if [[ "${listJson}" != *'"tag_name"'* && "${listJson//[[:space:]]/}" != "[]" ]]; then
		fDie "the GitHub API returned something that isn't a release list. Something between here and GitHub may be catching the request."
	fi
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
	fTagOk "${tag}" || fDie "the newest release's tag isn't a plain tag name: ${tag}"

	local asset="${exeName}-linux-${arch}"
	local url="${dlBase}/${tag}/${asset}"
	local destPath="${destDir}/${exeName}"
	local sums want
	## Fetched before the plan, since a local build can have the release's version, and only its bytes tell them apart.
	sums="$(fFetch "${dlBase}/${tag}/SHA256SUMS")" || fDie "download failed: SHA256SUMS for ${tag}"
	## Either case of hash and either line ending, as sha256sum -c takes.
	sums="${sums//$'\r'/}"
	want="$(awk -v f="${asset}" '$2 == f || $2 == "*" f { print $1; exit }' <<< "${sums}" | tr '[:upper:]' '[:lower:]')"
	[[ "${want}" =~ ^[0-9a-f]{64}$ ]] || fDie "SHA256SUMS for ${tag} doesn't list ${asset}"
	local haveVer="" haveSum="" current=0 useSudo=0
	if [[ -x "${destPath}" ]]; then
		## "tt-minecraftd v1.0.0-alpha.1 build df7q6 (2026-11-12)", then the copyright.
		haveVer="$("${destPath}" --version 2> /dev/null || true)"; haveVer="${haveVer%%$'\n'*}"; haveVer="${haveVer#"${exeName} "}"; haveVer="${haveVer%% *}"
		if [[ -r "${destPath}" ]]; then haveSum="$(sha256sum < "${destPath}" || true)"; haveSum="${haveSum%% *}"; fi
		## The release's bytes count only in a plain file that only root can change, since root runs it.
		if [[ "${haveSum}" == "${want}" ]] && fRootOnly "${destPath}"; then current=1; fi
	fi
	## Always root's, even where the folder lets the operator write, since setup's sudo run and the boot unit run it.
	if ((! current)) && [[ "$(id -u)" != 0 ]]; then
		useSudo=1
		command -v sudo > /dev/null || fDie "the binary is installed as root, and there's no sudo. Run this as root."
	fi
	local retire=0
	## The release's own bytes lose nothing when written over, so only another build is kept.
	if ((! current)) && [[ -f "${destPath}" && ! -L "${destPath}" && "${haveSum}" != "${want}" ]]; then retire=1; fi

	## The plan.
	fEcho "Plan"
	fEcho_Clean "    Release ....: ${tag}${note}"
	if ((anyDistro)); then fEcho_Clean "    Packages ...: none, since --any-distro installs nothing, and what setup needs is here"; fi
	if ((current)); then
		fEcho_Clean "    Install ....: ${destPath} is already ${tag}, matching SHA256SUMS, so it's left as is"
	else
		fEcho_Clean "    Download ...: ${url}"
		fEcho_Clean "    Check ......: SHA-256 against the release's SHA256SUMS"
		local how="" was="${haveVer:+ (replacing ${haveVer})}"
		if ((useSudo)); then how=", through sudo"; fi
		if [[ "${haveVer}" == "${tag}" ]]; then was=" (replacing a ${tag} build that isn't the release's)"; fi
		if [[ "${haveSum}" == "${want}" ]]; then was=" (the release's build, put back as root's, since someone else could change it)"; fi
		fEcho_Clean "    Install ....: ${destPath}${was}${how}"
		if ((retire)); then fEcho_Clean "    Old build ..: moves to ${oldDir}, where old builds are kept"; fi
	fi
	fEcho_Clean "    Then .......: ${exeName} setup${setupArgs[0]+ ${setupArgs[*]}}, which shows its own plan and asks again"
	echo
	if ((! assumeYes)); then
		fAsk "Go ahead?" || { echo; fEcho "Stopped. Nothing was changed."; echo; return 1; }
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

		fEcho "Checking SHA-256"
		local got
		got="$(sha256sum < "${work}/${asset}")"; got="${got%% *}"
		if [[ "${got}" != "${want}" ]]; then
			local first=""
			IFS= read -r -n 1 first < "${work}/${asset}" || true
			[[ "${first}" != "<" ]] || fDie "the download came back as a web page, not a binary. Something between here and GitHub is catching it. Nothing was installed."
			fDie "${asset} doesn't match SHA256SUMS (listed ${want}, got ${got}). Nothing was installed."
		fi

		fEcho "Installing ${destPath}"
		## Staged beside it, so the new build can keep a copy of the old one first, and the swap is one rename.
		local stage="${destDir}/.${exeName}.new"
		## Only when missing, since install -d sets the mode of one that's there too, such as Debian's old root:staff 2775.
		if [[ ! -d "${destDir}" ]]; then fAsRoot install -d -m 0755 "${destDir}" || fDie "couldn't make ${destDir}"; fi
		fAsRoot install -m 0755 -o root -g root "${work}/${asset}" "${stage}" || fDie "couldn't write ${stage}"
		## A build that matches its sum can still fail to run here, such as on a noexec mount.
		local rc=0
		"${stage}" --version > /dev/null 2>&1 || rc=$?
		if ((rc)); then
			fAsRoot rm -f -- "${stage}" || true
			fDie "the new build won't run on this box (exit ${rc}), so nothing was installed"
		fi
		if ((retire)) && ! fAsRoot "${stage}" retire-build "${destPath}"; then
			fAsRoot rm -f -- "${stage}" || true
			fDie "couldn't move the old build out of the way, so nothing was installed"
		fi
		fAsRoot mv -f -- "${stage}" "${destPath}" || fDie "couldn't move ${stage} into place"
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

## True when the path is a regular file, not a link, owned by rootUid, with no group or other write.
fRootOnly(){
	local path="$1" st uid mode
	[[ -f "${path}" && ! -L "${path}" ]] || return 1
	st="$(stat -c '%u %a' -- "${path}" 2> /dev/null)" || return 1
	uid="${st%% *}"; mode="${st#* }"
	## Checked as text first, since an arithmetic test would run what's in it.
	[[ "${uid}" =~ ^[0-9]+$ && "${mode}" =~ ^[0-7]+$ ]] || return 1
	[[ "${uid}" == "${rootUid}" ]] || return 1
	(( (8#${mode} & 8#022) == 0 ))
}

## Takes out only the file it fetched and the folder it made, under the temp folder it named.
## A failed cleanup doesn't change how the run ended.
fWorkGone(){
	local w="${__installWork:-}" r="${__installTmpRoot:-}"
	[[ -n "${w}" && -n "${r}" ]] || return 0
	case "${w}" in
		"${r}"/tt-minecraftd-install.?*)
			if [[ -d "${w}" && ! -L "${w}" && "${w}" != *..* ]]; then
				rm -f -- "${w}/${__installAsset:-x}" || true
				rmdir -- "${w}" || true
			fi ;;
	esac
}

## Sourced by its test, which sets the paths above before calling fMain.
if ! (return 0 2> /dev/null); then fMain "$@"; fi


##	History:
##		- 2026-11-14: Created.
##		- 2026-11-17: A binary counts as current only when its SHA-256 matches the release's, not its version.
##		- 2026-11-17: Added --any-distro, and a systemd check.
##		- 2026-11-19: The binary is always installed owned by root. A different build there is moved to
##		  /var/lib/tt-minecraftd/old-builds by the new build's retire-build, rather than written over.
##		- 2026-11-20: The staged build has to run before it replaces anything. The release tag is checked
##		  before it goes into a URL, SHA256SUMS takes either case and CRLF, and a page from a captive
##		  portal is named as one. Option values take any case. Errors have a blank line either side, and
##		  with no terminal to ask on it says to add --yes.
##		- 2026-11-20: Downloads are https only, redirects included.
##		- 2026-11-22: Stops when the .deb or .rpm package is installed, and says to update through the
##		  package manager. /usr/local/bin is made only when it's missing, so an existing one keeps its mode.
##		  With wget, a redirect away from https fails the download, as it does with curl.
##		  retire-build keeps a copy of the old build and leaves it in place, so the rename of the new one
##		  is the only change to /usr/local/bin/tt-minecraftd, and there's always a binary there.
##		- 2026-11-22: The release's build already there counts as current only in a plain file owned by
##		  root with no group or other write. Otherwise it's put back as root's, with no copy kept.
