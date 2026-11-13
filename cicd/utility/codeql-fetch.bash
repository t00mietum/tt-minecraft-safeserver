#!/usr/bin/env bash

#  shellcheck disable=1091  ## 'source is valid here, but shellcheck doesn't know the path to it.'
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- Puts the pinned CodeQL bundle (the CLI and its Go query packs) in a cache folder, for
##		  cicd.bash's CodeQL step. Runs in the build container against its cache volume, or on
##		  this box against a folder under ~/.cache.
##		- The download is checked against CODEQL_SHA256 in config.bash before it's unpacked. The
##		  folder keeps one version: a new pin replaces the old one once it has unpacked.
##	Syntax:
##		codeql-fetch.bash CACHE_DIR
##		  The CLI ends up at CACHE_DIR/<CODEQL_VERSION>/codeql/codeql.
##	Exit: 0 it's there, 1 the download, the sum or the unpack failed, 2 bad arguments.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


set -Eeuo pipefail

meDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${meDir}/../config.bash"
source "${meDir}/include/safe-rm.bash"

(($# == 1)) && [[ "$1" == /?* ]] || { sed -n '/^##	Syntax:/,/^##	Exit:/p' "${BASH_SOURCE[0]}" | sed 's/^##	\{0,1\}//' >&2; exit 2; }
cache="${1%/}"
[[ "${CODEQL_VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "${CODEQL_SHA256}" =~ ^[0-9a-f]{64}$ ]] || { echo "codeql-fetch: bad CODEQL_VERSION or CODEQL_SHA256 in config.bash" >&2; exit 2; }
want="${cache}/${CODEQL_VERSION}"
url="https://github.com/github/codeql-action/releases/download/codeql-bundle-v${CODEQL_VERSION}/codeql-bundle-go-linux64.tar.zst"

## Anything in the folder but the current version: an older pin, or a fetch that died partway.
## A part folder under an hour old may be another run's fetch, which can't be told apart by pid
## from another container, so it stays.
fPrune(){
	local d
	for d in "${cache}"/* "${cache}"/.part-*; do
		[[ -e "${d}" && "${d}" != "${want}" ]] || continue
		[[ "${d##*/}" != .part-* || -n "$(find "${d}" -maxdepth 0 -mmin +60 2>/dev/null)" ]] || continue
		fSafeRmUnder "${cache}" "${d}"
	done
}

mkdir -p -- "${cache}"
if [[ -x "${want}/codeql/codeql" && "$(cat "${want}/sha256" 2>/dev/null || true)" == "${CODEQL_SHA256}" ]]; then
	echo "codeql ${CODEQL_VERSION}: cached in ${cache}"
	fPrune; exit 0
fi

part="$(mktemp -d "${cache}/.part-XXXXXX")"; chmod 755 "${part}"
trap '[[ ! -d "${part}" ]] || fSafeRmUnder "${cache}" "${part}" >/dev/null' EXIT
echo "codeql ${CODEQL_VERSION}: downloading ${url##*/}"
curl -fsSL --retry 2 -o "${part}/bundle.tar.zst" "${url}"
echo "${CODEQL_SHA256}  ${part}/bundle.tar.zst" | sha256sum -c --quiet -
tar --zstd -xf "${part}/bundle.tar.zst" -C "${part}" --no-same-owner
rm -f -- "${part}/bundle.tar.zst"
[[ -x "${part}/codeql/codeql" ]] || { echo "codeql-fetch: no codeql/codeql in the bundle" >&2; exit 1; }
echo "${CODEQL_SHA256}" > "${part}/sha256"
## Another run may have put the same version in place meanwhile; then this copy goes.
if [[ -x "${want}/codeql/codeql" && "$(cat "${want}/sha256" 2>/dev/null || true)" == "${CODEQL_SHA256}" ]]; then
	echo "codeql ${CODEQL_VERSION}: another run unpacked it first"; exit 0
fi
if [[ -e "${want}" ]]; then fSafeRmUnder "${cache}" "${want}" >/dev/null; fi
mv -T -- "${part}" "${want}"
echo "codeql ${CODEQL_VERSION}: unpacked in ${cache} ($(du -sm -- "${want}" | cut -f1) MB)"
fPrune


##	History:
##		- 2026-11-13: Created.
