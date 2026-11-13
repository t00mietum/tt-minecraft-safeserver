#!/usr/bin/env bash

#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	Purpose:
##		- fSafeRm PATH: the one way the pipeline removes a folder. One absolute path, under a folder
##		  named tmp or under the run's own ${HOST_TEMP}/<app>/, never a whole project temp folder,
##		  and not through a link. HOST_TEMP is the caller's, resolved with realpath once at its start.
##		- fSafeRmUnder PARENT PATH: the same checks, for something outside a temp root, such as a
##		  folder in the repo. It removes only under the one PARENT it is given.
##		- A copy of the box-wide guard, since a clone or another box has none. Uses the caller's APP_NAME.
##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


fSafeRm(){
	local p="${1:-}" real='' own="${HOST_TEMP:?}/${APP_NAME:?}"
	if [[ "${#}" -ne 1 || -z "${p}" || "${p}" == *..* || "${p}" == *//* || "${p}" == *$'\n'* ]]; then
		printf 'safe-rm: refused, bad path: "%s"\n' "${p}" >&2; return 1
	fi
	p="${p%/}"
	if ! [[ "${p}" =~ ^/(.+/)?tmp/[^/]+(/[^/]+)*$ || "${p}" == "${own}"/?* ]]; then
		printf 'safe-rm: refused, not under a tmp folder: %s\n' "${p}" >&2; return 1
	fi
	if [[ "${p}" == "${HOST_TEMP}" || ( "${p}" == "${HOST_TEMP}"/* && "${p#"${HOST_TEMP}"/}" != */* ) ]]; then
		printf 'safe-rm: refused, a whole project temp folder: %s\n' "${p}" >&2; return 1
	fi
	if [[ -L "${p}" ]]; then printf 'safe-rm: refused, a link: %s\n' "${p}" >&2; return 1; fi
	real="$(realpath -e -- "${p}" 2> /dev/null)" || { printf 'safe-rm: refused, not there: %s\n' "${p}" >&2; return 1; }
	if [[ "${real}" != "${p}" ]]; then printf 'safe-rm: refused, goes through a link: %s\n' "${p}" >&2; return 1; fi
	rm -rf --one-file-system -- "${p}" && printf 'removed %s\n' "${p}"
}

fSafeRmUnder(){
	local parent="${1:-}" p="${2:-}" real=''
	if [[ "${#}" -ne 2 || -z "${parent}" || -z "${p}" || "${p}" == *..* || "${p}" == *//* || "${p}" == *$'\n'* ]]; then
		printf 'safe-rm: refused, bad path: "%s"\n' "${p}" >&2; return 1
	fi
	parent="${parent%/}"; p="${p%/}"
	[[ "${parent}" == /?* && "${parent}" != "${HOME:-/nonexistent}" ]] || { printf 'safe-rm: refused, bad parent: %s\n' "${parent}" >&2; return 1; }
	[[ "${p}" == "${parent}"/?* ]] || { printf 'safe-rm: refused, not under %s: %s\n' "${parent}" "${p}" >&2; return 1; }
	if [[ -L "${p}" ]]; then printf 'safe-rm: refused, a link: %s\n' "${p}" >&2; return 1; fi
	real="$(realpath -e -- "${p}" 2> /dev/null)" || { printf 'safe-rm: refused, not there: %s\n' "${p}" >&2; return 1; }
	if [[ "${real}" != "${p}" ]]; then printf 'safe-rm: refused, goes through a link: %s\n' "${p}" >&2; return 1; fi
	rm -rf --one-file-system -- "${p}" && printf 'removed %s\n' "${p}"
}


##	History:
##		- 2026-11-12: Created, from the shared safe removal guard.
