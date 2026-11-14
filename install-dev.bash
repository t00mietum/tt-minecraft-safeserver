#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- One-line dev environment setup for tt-minecraft-safeserver on Debian and its derivatives.
##		  Installs what's missing of git, Go, and docker through apt, and clones main.
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
	--dir PATH ...... where to clone (default: ./tt-minecraft-safeserver)
	--no-docker ..... don't install docker. cicd/cicd.bash then runs on this box, and needs
	                  staticcheck and gosec on PATH.
	-y|--yes ........ skip the prompt
	--version ....... print this installer's version
	-h|--help ....... this help
Notes:
	- Options take "--name value" or "--name=value".
	- Installs what's missing of git, ca-certificates, golang-go, and docker.io through apt-get,
	  through sudo when needed, then clones the main branch.
	- Debian and its derivatives only for now.
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
	local dir="" withDocker=1 assumeYes=0
	while (($#)); do case "$1" in
		--dir)       dir="${2:-}"; shift; (($#)) && shift ;;
		--dir=*)     dir="${1#*=}"; shift ;;
		--no-docker) withDocker=0; shift ;;
		-y|--yes)    assumeYes=1; shift ;;
		--version)   echo "${projectName} dev installer v${installerVersion}"; return 0 ;;
		-h|--help)   fUsage; return 0 ;;
		*) printf '[ ERROR: unknown option: %s (try --help) ]\n' "$1" >&2; return 2 ;;
	esac; done
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
	case "${osId}" in
		debian) : ;;
		nixos) fDie "NixOS isn't supported yet. Its module is planned for a later release." ;;
		*) case " ${osLike} " in
			*" debian "*) : ;;
			*) fDie "only Debian and its derivatives are supported for now, since this installs packages through apt (this is '${osId:-unknown}')" ;;
		esac ;;
	esac

	## What's missing.
	local pkgs="" have="" goMinor
	if command -v git > /dev/null; then have="${have}, git"; else pkgs="${pkgs} git"; fi
	[[ -e "${caFile}" ]] || pkgs="${pkgs} ca-certificates"
	goMinor="$(fGoMinor)"
	if [[ -n "${goMinor}" ]] && ((goMinor >= goMinMinor)); then have="${have}, Go 1.${goMinor}"; else pkgs="${pkgs} golang-go"; fi
	if ((withDocker)); then
		if command -v docker > /dev/null; then have="${have}, docker"; else pkgs="${pkgs} docker.io"; fi
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
	if [[ " ${pkgs} " == *" docker.io "* && "$(id -u)" != 0 ]]; then
		echo
		fEcho_Clean "    cicd/cicd.bash runs its Go steps in docker. To use docker without sudo, add this account"
		fEcho_Clean "    to the docker group, which is the same as root, and log in again:"
		fEcho_Clean "        sudo usermod -aG docker ${USER:-\$USER}"
	fi
	echo
}

## Sourced by its test, which sets the paths above before calling fMain.
if ! (return 0 2> /dev/null); then fMain "$@"; fi


##	History:
##		- 2026-11-14: Created.
