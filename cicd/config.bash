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
## shellcheck and markdownlint, in the image too. Node is only there for markdownlint, whose version
## and dependencies are pinned in container/markdownlint/package-lock.json. The shellcheck sums are
## of the release files as first downloaded, since the project doesn't publish any.
NODE_VERSION="22.23.2"
NODE_SHA256_AMD64="b294a556e639d64338823920e5866c21c02741742d2e1529ee1a225c1ec9252a"
NODE_SHA256_ARM64="013b59cfd2819703a6f4a14ab891fc46fc2a4e3f5bcd92de3fb4929b43e35b30"
SHELLCHECK_VERSION="v0.11.0"
SHELLCHECK_SHA256_AMD64="8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"
SHELLCHECK_SHA256_ARM64="12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588"
MARKDOWNLINT_VERSION="$(sed -n 's/^ *"markdownlint-cli": "\(.*\)".*/\1/p' "${BASH_SOURCE[0]%/*}/container/markdownlint/package.json")"
## Without the container, the host's copies are used, and compared with these.
TOOL_VERSIONS=(
	"shellcheck=${SHELLCHECK_VERSION#v}"
	"markdownlint=${MARKDOWNLINT_VERSION}"
)

## Same flags at every build site, so a release can be rebuilt to its published checksum.
GO_BUILD_FLAGS=(-trimpath -buildvcs=false)
GO_LDFLAGS_COMMON="-s -w -buildid="
## What the Linux releases are built for. The binary names are what update --self reads, so
## they say amd64; the packages use each format's own arch names.
RELEASE_TARGETS=("linux/amd64" "linux/arm64")

## Other hosts: linux/arm64 is the only other target, since the tool is Linux only. The arm64 step
## runs the test binaries on that box, held through the shared host lock's wrap in VM_STATE_TOOL,
## which also puts a VM back the way it was found. A box or tool that isn't there skips the step.
ARM64_HOST="${CICD_ARM64_HOST:-vmDebARM64}"              ## the name the host lock knows it by
ARM64_SSH="${CICD_ARM64_SSH:-tester@${ARM64_HOST}}"     ## ssh target, a plain test account
VM_STATE_TOOL="${CICD_VM_STATE:-vm-state}"              ## on PATH, or a full path
ARM64_LOCK_WAIT="${CICD_ARM64_LOCK_WAIT:-600}"          ## seconds to wait for the box before skipping

## Fuzzing: how long each Fuzz target runs. The seeds run with the plain tests either way.
FUZZ_TIME="30s"
FUZZ_TIME_QUICK="10s"
FUZZ_TIME_LONG="5m"

## Soak: rounds of the main actions before the first heap and goroutine sample, and again before
## the second. The tests skip outside the soak step.
SOAK_ENV="TT_MINECRAFTD_SOAK"   ## soak.Env in source/internal/soak
SOAK_ROUNDS="100"
SOAK_ROUNDS_LONG="1000"

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

## Dogfood: this is the only server, so the build goes to its final place, where setup puts it.
## Root's folder, so the step installs with sudo when it can without a password.
DOGFOOD_DESTS=(
	"/usr/local/bin"
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
##		- 2026-11-13: Fuzz times. shellcheck, markdownlint and Node pinned for the image.
##		- 2026-11-13: Soak rounds.
