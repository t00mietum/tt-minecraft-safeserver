#!/usr/bin/env bash

#  shellcheck disable=1091  ## 'source is valid here, but shellcheck doesn't know the path to it.'
#  shellcheck disable=2153  ## 'Possible misspelling.' False hits on vars assigned in the sourced config.bash.
#  shellcheck disable=2154  ## 'referenced but not assigned.' False hits on vars assigned in the sourced config.bash.
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Rebuilds a release's Linux binaries from its tag and compares them with the release's
##		  SHA256SUMS. Each binary is built the way the pipeline's build step built it: the tag's own
##		  config.bash gives the Go version, the targets and the flags, and the tag's commit time
##		  goes in as the build time.
##		- Needs git and any Go 1.21 or newer, which downloads the pinned Go version for the build.
##		  Run it from inside a clone.
##	Syntax:
##		verify-release.bash TAG [SHA256SUMS]
##		  With no SHA256SUMS file, the release's own is downloaded from GitHub.
##	Exit: 0 every binary matches, 1 one differs or isn't listed, 2 bad arguments or a failed build.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


set -Eeuo pipefail

meDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${meDir}/include/safe-rm.bash"
releaseRepo="t00mietum/tt-minecraft-safeserver"

fUsage(){ [[ -z "${1:-}" ]] || echo "verify-release: $1" >&2; sed -n '/^##	Syntax:/,/^##	Exit:/p' "${BASH_SOURCE[0]}" | sed 's/^##	\{0,1\}//' >&2; exit 2; }
fFail(){ echo "verify-release: $*" >&2; exit 2; }
(($# == 1 || $# == 2)) || fUsage
tag="$1"; sums="${2:-}"
[[ "${tag}" =~ ^[A-Za-z0-9._/-]+$ && "${tag}" != -* ]] || fUsage "bad tag: ${tag}"
[[ -z "${sums}" || -f "${sums}" ]] || fUsage "no file ${sums}"
[[ -z "${sums}" ]] || sums="$(realpath -e -- "${sums}")"
command -v go >/dev/null 2>&1 || fFail "needs go on PATH"
commit="$(git rev-parse --verify --quiet "${tag}^{commit}")" || fFail "no commit for ${tag} in this clone"
epoch="$(git log -1 --format=%ct "${commit}")"

APP_NAME="tt-minecraft-safeserver"
HOST_TEMP="$(realpath -e -- "${HOST_TEMP:-${TMPDIR:-/tmp}}")"
mkdir -p -- "${HOST_TEMP}/${APP_NAME}"
work=""
for ((n = 1; n < 1000; n++)); do
	printf -v work '%s/%s/%(%Y%m%d-%H%M%S)T_%03d' "${HOST_TEMP}" "${APP_NAME}" -1 "${n}"
	mkdir -- "${work}" 2>/dev/null && break
	work=""
done
[[ -n "${work}" ]] || fFail "can't make a temp folder in ${HOST_TEMP}/${APP_NAME}"
trap 'fSafeRm "${work}" >/dev/null || true' EXIT

## The tag's own tree and settings, so a later change to them can't move the result.
mkdir -- "${work}/src"
git archive "${commit}" | tar -x -C "${work}/src"
source "${work}/src/cicd/config.bash"
version="$(sed -n 's/^var version = "\(.*\)".*/\1/p' "${work}/src/${VERSION_FILE}" | head -n 1)"
[[ "${version}" == "${tag}" ]] || fFail "${tag} holds version ${version:-none}; a release's tag and version are the same"
goVer="$(sed -n 's/^.*golang:\([0-9][0-9.]*\)-.*$/\1/p' <<< "${GO_IMAGE}")"
[[ -n "${goVer}" ]] || fFail "no Go version in GO_IMAGE: ${GO_IMAGE}"

if [[ -z "${sums}" ]]; then
	command -v curl >/dev/null 2>&1 || fFail "needs curl to fetch SHA256SUMS, or pass the file"
	sums="${work}/SHA256SUMS"
	curl -fsSL --retry 2 -o "${sums}" "https://github.com/${releaseRepo}/releases/download/${tag}/SHA256SUMS" || fFail "can't fetch SHA256SUMS for ${tag}"
fi

echo "Rebuilding ${EXE_NAME} ${tag} (commit ${commit:0:12}, Go ${goVer})"
declare -i bad=0
for t in "${RELEASE_TARGETS[@]}"; do
	os="${t%%/*}"; arch="${t##*/}"; name="${EXE_NAME}-${os}-${arch}"
	( cd "${work}/src" && env -u GOFLAGS GOTOOLCHAIN="go${goVer}" GOOS="${os}" GOARCH="${arch}" CGO_ENABLED=0 GOAMD64=v1 \
		go build "${GO_BUILD_FLAGS[@]}" -ldflags "${GO_LDFLAGS_COMMON} -X main.version=${tag} -X main.buildEpoch=${epoch} -X main.release=1" \
		-o "${work}/${name}" "${GO_MAIN_PKG}" ) || fFail "build failed for ${t}"
	got="$(sha256sum < "${work}/${name}")"; got="${got%% *}"
	want="$(awk -v f="${name}" '$2 == f || $2 == "./" f || $2 == "*" f { print $1; exit }' "${sums}")"
	if [[ -z "${want}" ]]; then echo "MISSING  ${name}: not in SHA256SUMS"; bad+=1
	elif [[ "${got}" == "${want}" ]]; then echo "MATCH    ${name}  ${got}"
	else echo "DIFFER   ${name}  built ${got}, listed ${want}"; bad+=1; fi
done
((bad == 0)) || exit 1


##	History:
##		- 2026-11-13: Created.
