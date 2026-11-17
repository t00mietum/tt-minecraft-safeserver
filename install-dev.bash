#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- One-line dev environment setup for tt-minecraft-safeserver on Debian and its derivatives.
##		  Installs what's missing of git and Go through apt, and clones main. Docker or Podman
##		  has to be there already, unless asked to install docker.
##		- With --any-distro it installs nothing, and only checks that what it needs is there.
##		- States its plan and asks first. Re-running it is safe: what's there is skipped, and an
##		  existing clone is left as is.
##	Syntax:
##		bash <(curl -fsSL https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install-dev.bash) [options]
##		  See fUsage below.
##	Exit: 0 done, 1 refused, aborted or failed, 2 bad options.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under The MIT License (MIT). Full text at:
##		https://mit-license.org/
##	SPDX-License-Identifier: MIT


set -Eeuo pipefail

installerVersion="1.0.0"
projectName="tt-minecraft-safeserver"
cloneUrl="https://github.com/t00mietum/tt-minecraft-safeserver.git"
cloneBranch="main"
modulePath="github.com/t00mietum/tt-minecraft-safeserver"
## Go 1.21 and newer fetch the version go.mod asks for on their own.
goMinMinor=21
osRelease="/etc/os-release"
caFile="/etc/ssl/certs/ca-certificates.crt"
ttyDev="/dev/tty"

fEcho(){       printf '[ %s ]\n' "$*"; }
fEcho_Clean(){ printf '%s\n' "$*"; }
fDie(){        printf '[ ERROR: %s ]\n' "$*" >&2; echo >&2; exit 1; }

## Spelled out rather than read from the header, since under `bash <(curl ...)` there's no file to read.
fUsage(){ cat <<'EOF'
Usage:
	bash <(curl -fsSL https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install-dev.bash) [options]
	bash <(wget -qO- https://raw.githubusercontent.com/t00mietum/tt-minecraft-safeserver/main/install-dev.bash) [options]
Options:
	--dir PATH ......... where to clone (default: ./tt-minecraft-safeserver)
	--install-docker ... install docker.io through apt when there's no docker or podman
	--no-docker ........ go on without docker or podman. cicd/cicd.bash then runs on this box,
	                     and needs staticcheck and gosec on PATH.
	--any-distro ....... for a distro this doesn't support. Installs nothing, and checks that git,
	                     Go 1.21 or newer, and docker or podman are already there.
	-y|--yes ........... skip the prompt
	--version .......... print this installer's version
	-h|--help .......... this help
Notes:
	- Options take "--name value" or "--name=value".
	- Installs what's missing of git, ca-certificates, and golang-go through apt-get, through sudo
	  when needed, then clones the main branch.
	- Docker or Podman has to be installed already, since cicd/cicd.bash runs its checks in a
	  container. --install-docker or --no-docker get past that.
	- Debian and its derivatives, or any distro with --any-distro.
EOF
}

## Ctrl-D, or no terminal at all, reads as no.
fAsk(){
	local answer=""
	if [[ -t 0 ]]; then read -r -p "$1 [y/N] " answer || true
	elif { : < "${ttyDev}"; } 2> /dev/null; then printf '%s [y/N] ' "$1"; read -r answer < "${ttyDev}" || true; echo
	fi
	case "${answer}" in y|Y|yes|Yes|YES) return 0 ;; *) return 1 ;; esac
}

## Minor version of the Go on PATH when it's 1.x, else nothing.
fGoMinor(){
	local v=""
	command -v go > /dev/null || return 0
	v="$(go version 2> /dev/null || true)"
	if [[ "${v}" =~ go1\.([0-9]+) ]]; then echo "${BASH_REMATCH[1]}"; fi
}

## A git checkout whose go.mod is this module, so a fork's clone counts too.
fIsClone(){
	[[ -e "$1/.git" && -f "$1/go.mod" ]] || return 1
	awk -v m="${modulePath}" '$1 == "module" { found = ($2 == m); exit } END { exit !found }' "$1/go.mod"
}

fRoot(){ if ((useSudo)); then sudo "$@"; else "$@"; fi; }

fMain(){
	local dir="" withDocker=1 installDocker=0 anyDistro=0 assumeYes=0
	while (($#)); do case "$1" in
		--dir)            dir="${2:-}"; shift; (($#)) && shift ;;
		--dir=*)          dir="${1#*=}"; shift ;;
		--install-docker) installDocker=1; shift ;;
		--no-docker)      withDocker=0; shift ;;
		--any-distro)     anyDistro=1; shift ;;
		-y|--yes)         assumeYes=1; shift ;;
		--version)        echo "${projectName} dev installer v${installerVersion}"; return 0 ;;
		-h|--help)        fUsage; return 0 ;;
		*) printf '[ ERROR: unknown option: %s (try --help) ]\n' "$1" >&2; return 2 ;;
	esac; done
	if ((installDocker && ! withDocker)); then printf '[ ERROR: --install-docker and --no-docker cancel each other out ]\n' >&2; return 2; fi
	if ((installDocker && anyDistro)); then printf "[ ERROR: --any-distro installs nothing, so it doesn't go with --install-docker ]\n" >&2; return 2; fi
	if [[ -z "${dir}" ]]; then dir="${PWD}/${projectName}"
	elif [[ "${dir}" != /* ]]; then dir="${PWD}/${dir}"; fi
	while [[ "${dir}" == ?*/ ]]; do dir="${dir%/}"; done

	echo

	## What this box is.
	[[ "$(uname -s)" == Linux ]] || fDie "${projectName} runs on Linux only"
	local osId="" osLike=""
	if [[ -r "${osRelease}" ]]; then
		osId="$(awk -F= '$1 == "ID" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
		osLike="$(awk -F= '$1 == "ID_LIKE" { gsub(/"/, "", $2); print $2 }' "${osRelease}")"
	fi
	## Nothing this does is tied to a distro once it installs nothing, so --any-distro skips all of it, NixOS included.
	if ((! anyDistro)); then
		case "${osId}" in
			debian) : ;;
			nixos) fDie "NixOS isn't supported yet. Its module is planned for a later release. --any-distro skips this check, and installs nothing." ;;
			*) case " ${osLike} " in
				*" debian "*) : ;;
				*) fDie "only Debian and its derivatives are supported for now, since this installs packages through apt (this is '${osId:-unknown}'). --any-distro skips this check, and installs nothing." ;;
			esac ;;
		esac
	fi

	## What's missing.
	local pkgs="" have="" missing="" goMinor runtime=""
	if command -v git > /dev/null; then have="${have}, git"; else pkgs="${pkgs} git"; missing="${missing}, git"; fi
	## Not in missing: the path is Debian's, and elsewhere a missing CA bundle shows up as a failed clone.
	[[ -e "${caFile}" ]] || pkgs="${pkgs} ca-certificates"
	goMinor="$(fGoMinor)"
	if [[ -n "${goMinor}" ]] && ((goMinor >= goMinMinor)); then have="${have}, Go 1.${goMinor}"
	else pkgs="${pkgs} golang-go"; missing="${missing}, Go 1.${goMinMinor} or newer"; fi
	if ((withDocker)); then
		if command -v docker > /dev/null; then
			## podman-docker's docker is Podman, which needs no group.
			case "$(docker --version 2> /dev/null || true)" in *[Pp]odman*) runtime="podman-docker" ;; *) runtime="docker" ;; esac
			have="${have}, docker"
		elif command -v podman > /dev/null; then runtime="podman"; have="${have}, podman"
		elif ((installDocker)); then pkgs="${pkgs} docker.io"
		elif ((anyDistro)); then missing="${missing}, docker or podman"
		else fDie "docker or podman is needed, since cicd/cicd.bash runs its checks in a container. Install one first, or add --install-docker to install docker.io through apt, or --no-docker to go on without either."
		fi
	fi
	if ((anyDistro)); then
		[[ -z "${missing}" ]] || fDie "--any-distro installs nothing, and this box is missing: ${missing#, }. Install it with this distro's package manager, then run this again."
		pkgs=""
	fi
	pkgs="${pkgs# }"; have="${have#, }"

	local cloneHave=0
	if fIsClone "${dir}"; then cloneHave=1
	elif [[ -e "${dir}" ]] && ! [[ -d "${dir}" && -z "$(ls -A "${dir}")" ]]; then
		fDie "${dir} is already there and isn't a clone of ${projectName}. Pick another folder with --dir."
	fi

	useSudo=0
	if [[ -n "${pkgs}" && "$(id -u)" != 0 ]]; then
		useSudo=1
		command -v sudo > /dev/null || fDie "installing packages needs root, and there's no sudo. Run this as root, or install them first: ${pkgs}"
	fi

	## The plan.
	fEcho "Plan"
	if [[ -n "${pkgs}" ]]; then
		local how=""; if ((useSudo)); then how=", through sudo"; fi
		fEcho_Clean "    Packages ...: ${pkgs}, from apt${how}${have:+ (already here: ${have})}"
	elif ((anyDistro)); then
		fEcho_Clean "    Packages ...: none, since --any-distro installs nothing. ${have} already here."
	else
		fEcho_Clean "    Packages ...: none needed, ${have} already here"
	fi
	if ((cloneHave)); then
		fEcho_Clean "    Clone ......: ${dir} is already a clone, so it's left as is"
	else
		fEcho_Clean "    Clone ......: ${cloneUrl} (${cloneBranch})"
		fEcho_Clean "    Into .......: ${dir}"
	fi
	echo
	if ((! assumeYes)); then
		fAsk "Go ahead?" || { fEcho "Stopped. Nothing was changed."; echo; return 1; }
		echo
	fi

	if [[ -n "${pkgs}" ]]; then
		fEcho "Installing ${pkgs}"
		fRoot apt-get update || fDie "apt-get update failed"
		## Word splitting on purpose: package names have no spaces.
		# shellcheck disable=SC2086
		fRoot env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${pkgs} || fDie "apt-get install failed"
		echo
	fi

	if ((! cloneHave)); then
		fEcho "Cloning ${cloneBranch} into ${dir}"
		git clone --branch "${cloneBranch}" -- "${cloneUrl}" "${dir}" || fDie "git clone failed"
		echo
	fi

	## apt's golang-go on an older release, such as bookworm's 1.19, is too old to fetch a newer one.
	goMinor="$(fGoMinor)"
	if [[ -z "${goMinor}" ]] || ((goMinor < goMinMinor)); then
		fDie "Go 1.${goMinMinor} or newer is needed, and this has ${goMinor:+Go 1.}${goMinor:-none}. The rest is done. Get a newer Go from https://go.dev/dl/ or the release's backports."
	fi

	fEcho "Ready"
	fEcho_Clean "    cd ${dir}"
	fEcho_Clean "    go build ./source/cmd/tt-minecraftd ...... build the tool"
	fEcho_Clean "    cicd/cicd.bash --gate .................... the checks to pass before a commit"
	fEcho_Clean "    cicd/cicd.bash --help .................... the other runs"
	## A docker that was already here may still refuse this account, and cicd then runs on this box without a word.
	if [[ "$(id -u)" != 0 ]] && { [[ " ${pkgs} " == *" docker.io "* ]] || { [[ "${runtime}" == docker ]] && ! docker info > /dev/null 2>&1; }; }; then
		echo
		fEcho_Clean "    cicd/cicd.bash runs its Go steps in docker. To use docker without sudo, add this account"
		fEcho_Clean "    to the docker group, which is the same as root, and log in again:"
		fEcho_Clean "        sudo usermod -aG docker ${USER:-\$USER}"
	fi
	if [[ "${runtime}" == podman ]]; then
		echo
		fEcho_Clean "    cicd/cicd.bash calls docker, which Podman's podman-docker package adds. Without it, the"
		fEcho_Clean "    checks run on this box, and need staticcheck and gosec on PATH."
	fi
	echo
}

## Sourced by its test, which sets the paths above before calling fMain.
if ! (return 0 2> /dev/null); then fMain "$@"; fi


##	History:
##		- 2026-11-14: Created.
##		- 2026-11-17: Docker or Podman is a prerequisite, with --install-docker. Added --any-distro.
