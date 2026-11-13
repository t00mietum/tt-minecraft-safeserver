#!/usr/bin/env bash
#  shellcheck disable=2034  ## 'variable appears unused.' Everything here is consumed by cicd.bash after sourcing.
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Project settings for the pipeline. cicd.bash stays generic.
##		- Paths are relative to the repo root; cicd.bash cds there first.
##		- Every tool version the pipeline uses is pinned here, the container recipe included.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


## Only allow running 'sourced'.
declare -i isSourced_q3m8v=0; [[ "${BASH_SOURCE[0]}" == "${0}" ]] || isSourced_q3m8v=1
((isSourced_q3m8v)) || { echo -e "\nError in $(basename "${BASH_SOURCE[0]}"): This script is meant to be 'sourced' from within another script.\n"; exit 1; }

APP_NAME="tt-minecraft-safeserver"
EXE_NAME="tt-minecraftd"
GO_MAIN_PKG="./source/cmd/tt-minecraftd"
## Where the version string is kept. The tag and the build stamp are both read from it.
VERSION_FILE="source/cmd/tt-minecraftd/main.go"

## The build and test image, and the tools in it. Rebuilt when any of these change.
GO_IMAGE="docker.io/library/golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0"
STATICCHECK_VERSION="v0.7.0"
GOVULNCHECK_VERSION="v1.5.0"
GOSEC_VERSION="v2.30.0"
NFPM_VERSION="v2.47.0"
## Host tools that aren't in the image. Recorded and compared, never installed.
TOOL_VERSIONS=(
	"shellcheck=0.11.0"
	"markdownlint=0.49.1"
)

## Same flags at every build site, so a release can be rebuilt to its published checksum.
GO_BUILD_FLAGS=(-trimpath -buildvcs=false)
GO_LDFLAGS_COMMON="-s -w -buildid="
## What the Linux releases are built for. The binary names are what update --self reads, so
## they say amd64; the packages use each format's own arch names.
RELEASE_TARGETS=("linux/amd64" "linux/arm64")

## The areas --backlog-run-quick takes are the folders under source/internal, plus cli for the command.
CLI_AREA_DIR="source/cmd/tt-minecraftd"

SHELL_LINT_GLOBS=(
	"cicd/*.bash"
	"cicd/utility/*.bash"
	"cicd/utility/include/*.bash"
	"cicd/root-host/*.bash"
	"cicd/hooks/pre-push"
)
## The license, conduct, and contributing texts are stock, so they stay as they came.
MD_LINT_GLOBS=(
	"README.md"
	"how-to-connect.md"
	"changelog.md"
	"style_guide.md"
	"trademark.md"
	"project/*.md"
)

## Each run's log, GFS-rotated, for lint-report.bash.
LINT_LOG_DIR="cicd/artifacts/lint"
## Release builds and packages, one folder per version.
RELEASE_DIR="cicd/artifacts/release"
RELEASE_KEEP=5

## Dogfood: the native build goes to the first dir here that exists and is writable.
DOGFOOD_DESTS=(
	"${HOME}/synced/0-0/common/exec/app/linux"
)

## Half the cores, rounded down, or a quarter on a busy box (1-minute load over half the cores).
## HEAVY_JOBS is a quarter either way, for -race. Never under 1.
## fCpuJobs VAR CORES LOAD HEAVY sets VAR, with LOAD as the load average prints it.
fCpuJobs(){
	local -n jobs_cpuJ="$1"
	local -i cores=1 load100=0
	local load="${3:-0}" frac=""
	## Checked first, since arithmetic would run what a bad one holds.
	[[ "$2" =~ ^[1-9][0-9]{0,5}$ ]] && cores="$2"
	frac="${load#*.}"; [[ "${load}" == *.* ]] || frac=""; frac="${frac}00"
	if [[ "${load%%.*}" =~ ^[0-9]+$ && "${frac:0:2}" =~ ^[0-9]+$ ]]; then load100=$(( 10#${load%%.*} * 100 + 10#${frac:0:2} )); fi
	jobs_cpuJ=$(( cores / 2 ))
	if [[ "$4" == 1 ]] || (( load100 * 2 > cores * 100 )); then jobs_cpuJ=$(( cores / 4 )); fi
	(( jobs_cpuJ >= 1 )) || jobs_cpuJ=1
}
fCpuBudget(){
	local rest="" jobs=0
	if [[ -z "${cpuCores:-}" ]]; then cpuCores="$(nproc 2>/dev/null || echo 2)"; fi
	cpuLoad=""
	if [[ -r /proc/loadavg ]]; then read -r cpuLoad rest < /proc/loadavg || true; fi
	fCpuJobs jobs "${cpuCores}" "${cpuLoad}" 0; BUILD_JOBS="${jobs}"
	fCpuJobs jobs "${cpuCores}" "${cpuLoad}" 1; HEAVY_JOBS="${jobs}"
}
fCpuBudget
## Low priority for the caller, and so for everything it starts.
fLowPriority(){
	renice 19 -p "$$" >/dev/null 2>&1 || true
	if command -v ionice >/dev/null 2>&1; then ionice -c 3 -p "$$" >/dev/null 2>&1 || true; fi
}


##	History:
##		- 2026-11-12: Created.
