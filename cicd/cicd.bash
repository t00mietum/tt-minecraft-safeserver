#!/usr/bin/env bash

#  shellcheck disable=1091  ## 'source is valid here, but shellcheck doesn't know the path to it.'
#  shellcheck disable=2034  ## 'variable appears unused.' Complains about valid use of variable indirection (e.g. local -n var=$1)
#  shellcheck disable=2153  ## 'Possible misspelling.' False hits on vars assigned in the sourced config.bash.
#  shellcheck disable=2154  ## 'referenced but not assigned.' False hits on vars assigned in the sourced files.
#  shellcheck disable=2155  ## 'Declare and assign separately to avoid masking return values.' Cumbersome and unnecessary.
#  shellcheck enable=require-variable-braces  ## Every expansion braced: "${var}", not "$var".

##	- Purpose: Local CI/CD pipeline for tt-minecraft-safeserver. Project settings live in config.bash.
##	- Steps (fail-fast, any error aborts before the next step):
##	   remote sync, format, lint, tests, race tests, fuzzing, soak, vulnerability scan, CodeQL,
##	   release builds, packages, tests on linux/arm64, root host checks, root host ZFS checks,
##	   demo gif, dogfood, publish
##	- Syntax:
##	  cicd/cicd.bash [options]
##	  Options:
##	   -q, --quiet         quiet + unattended (no prompt)
##	   -m, --message MSG   commit message for the publish stage (also --msg, and -m=MSG)
##	   (no flags)          the default run: every step above
##	   --quick             what fits in about 10 minutes: no race tests, soak, vulnerability scan,
##	                       packages, root host checks or demo gif, a short fuzz, and at most 60s waiting for
##	                       the arm64 box. Ends with dogfood and publish.
##	   --long              everything, with a longer fuzz and soak. Otherwise the same as no flags.
##	       --slow          alias for --long
##	   --leakcheck         add every leak check to the run, race tests and soak included
##	   --no-leakcheck      take every leak check out: race tests, soak, and the goroutine check in
##	                       the tests
##	   --backlog-run-quick [AREA...]
##	                       what a backlog worker runs before it commits, in one call of about 4 minutes:
##	                       format check, lint, tests and CodeQL, or only the areas named, which
##	                       leaves out the whole-tree checks: shellcheck, markdownlint, test IDs
##	                       and CodeQL. Areas are the folders under source/internal, and cli for
##	                       the command.
##	   --backlog-run-full  what a backlog round runs once at its end: every check of the default run
##	   --merge-gate        the same checks, before a merge to main. Runs from the pre-push hook.
##	   --gate [AREA...]    alias for --backlog-run-quick
##	       --ci            alias for --merge-gate
##	                       None of those three dogfoods, publishes, commits, pushes, installs, syncs,
##	                       prompts, or rewrites a tracked file.
##	   --no-container      run on this box rather than in the build container
##	   --no-remote         skip other hosts and VMs: the arm64 tests
##	   --no-sync           skip the remote sync
##	   --no-<step>         skip one step: format, lint, test, race, fuzz, soak, vuln, codeql, build,
##	                       package, arm64, roothost, rootzfs, demo, dogfood, publish
##	   --keep-temp         keep the run's temp folder and containers, and say where they are
##	   --list              print the steps and test IDs the other options would run, and run nothing
##	   --lint-check        print only run output that wasn't seen before, then mark it seen. For the
##	                       startup ritual. Exit 2 when there is no log yet.
##	   -h, --help          show this help
##	- If neither -q nor -m is given, the run prompts once for a commit message (Ctrl+C aborts the
##	  whole run), then finishes unattended.
##	- CICD_DOGFOOD_DEST names another dogfood folder, for a test.
##	- CICD_MEM_CAP caps the run's memory, with no swap: a size such as 24g, or none.
##	  The default is a third of RAM, from 4g to 32g.
##	- The arm64 step builds the test binaries for linux/arm64 and runs them on the arm64 test box,
##	  ARM64_HOST in config.bash, under the shared host lock. VM_STATE_TOOL holds the box and puts a
##	  VM back the way it was found. A box or tool that isn't there, or a box busy past
##	  ARM64_LOCK_WAIT, is a SKIP and the run goes on. CICD_ARM64_HOST, CICD_ARM64_SSH, CICD_VM_STATE
##	  and CICD_ARM64_LOCK_WAIT override them.
##	- The fuzz step runs each Fuzz target in turn for FUZZ_TIME in config.bash (FUZZ_TIME_QUICK in
##	  --quick, FUZZ_TIME_LONG in --long), from a copy of the tree, so a failing input lands in the
##	  run folder. Its file is printed; copy it to the package's testdata/fuzz to keep it as a test.
##	  CICD_FUZZ_TIME overrides the time in every run.
##	- The soak step runs each TestSoak, which loops the main actions headless and fails when the live
##	  heap or the goroutines grow between round N and round 2N. N is SOAK_ROUNDS in config.bash
##	  (SOAK_ROUNDS_LONG in --long), or CICD_SOAK_ROUNDS. It uses a quarter of the cores.
##	- The CodeQL step analyzes a copy of the tracked Go files with the pinned bundle and the query
##	  suite in config.bash (CODEQL_*), and fails on any result. The bundle is downloaded once, checked
##	  against its sum, and kept in the docker volume <app>-codeql, or under ~/.cache/<app>/codeql with
##	  --no-container. It uses a quarter of the cores, and skips on a box that isn't Linux amd64.
##	- The demo step records the README's CLI demo gif from the linux/amd64 build, with
##	  utility/demo.bash, in the build container as root with no network, since it writes a made-up
##	  install under /etc and /srv. Each render goes to DEMO_DIR in config.bash, GFS-rotated. Only a
##	  release run copies it to DEMO_GIF, which the publish stage then commits. Without the container
##	  it's a SKIP.
##	- The two root host steps take about 3 minutes each and need docker that can run --privileged.
##	  A merge gate or full run that includes them goes past one 10-minute call, so start it detached.

##	History: At bottom of script.

##	Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)
##	Licensed under the GNU General Public License v2.0 or later. Full text at:
##		https://spdx.org/licenses/GPL-2.0-or-later.html
##	SPDX-License-Identifier: GPL-2.0-or-later


if (( BASH_VERSINFO[0] * 100 + BASH_VERSINFO[1] < 404 )); then
	printf '%s\n' "${0##*/}: needs bash 4.4 or newer, and this is bash ${BASH_VERSION}." >&2; exit 1
fi
set -Eeuo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/.." && pwd)"
source "${here}/config.bash"
[[ -z "${CICD_DOGFOOD_DEST:-}" ]] || DOGFOOD_DESTS=("${CICD_DOGFOOD_DEST}")  ## another folder, for a test
source "${here}/utility/include/gfs-rotate.bash"
source "${here}/utility/include/safe-rm.bash"
## A memory cap with no swap, CICD_MEM_CAP or a third of the RAM from 4g to 32g, checked before
## anything starts. The whole run goes again under a user scope at the cap, once, first in line
## for the OOM killer. A step killed at the cap exits 137.
source "${here}/utility/include/mem-cap.bash"
fMemCap || exit 2
fMemScope "${here}/${BASH_SOURCE[0]##*/}" "$@"
fLowPriority
cd "${root}"
stamp="$(date +%Y%m%d-%H%M%S)"

## printf, not 'echo -e': a typed commit message passes through here.
declare -i __wasLastEchoBlank=0
fEcho_ResetBlankCounter(){ __wasLastEchoBlank=0; }
fEcho_Clean(){
	if [[ -n "${1:-}" ]]; then printf '%s\n' "$*"; __wasLastEchoBlank=0
	elif ((! __wasLastEchoBlank)); then echo; __wasLastEchoBlank=1; fi
}
fEcho(){       if [[ -n "$*"     ]]; then fEcho_Clean "[ $* ]"; else fEcho_Clean ""; fi; }
fEcho_Force(){ fEcho_ResetBlankCounter; fEcho "$*"; }
__letterbox="•••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••"
fSection(){ fCpuBudget; fEcho_Clean; fEcho_Clean "${__letterbox}"; fEcho "$*"; }
fDie(){ { fEcho_Force "FAILED: $*"; } >&2; exit 1; }
fUsage(){ fEcho_Clean "$*" >&2; exit 2; }

declare -A stepTitle=(
	[sync]="Remote sync" [format]="Format" [lint]="Lint" [test]="Tests" [race]="Race tests" [fuzz]="Fuzzing"
	[soak]="Soak" [vuln]="Vulnerability scan" [codeql]="CodeQL" [build]="Release builds" [package]="Packages" [arm64]="Tests on linux/arm64"
	[roothost]="Root host checks" [rootzfs]="Root host ZFS checks" [demo]="Demo gif" [dogfood]="Dogfood" [publish]="Publish"
)
allSteps=(sync format lint test race fuzz soak vuln codeql build package arm64 roothost rootzfs demo dogfood publish)
stepList=(); stepTotal=0; stepName=""; stepWhere=""; stepSkip=""
declare -A stepOff=() stepResult=()
fStepTime(){
	if [[ "${EPOCHREALTIME:-}" =~ ^([0-9]+)[.,]([0-9]{2}) ]]; then printf '%(%Y%m%d-%H%M%S)T.%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
	else printf '%(%Y%m%d-%H%M%S)T.00' -1; fi
}
fStepOf(){ local i; for i in "${!stepList[@]}"; do [[ "${stepList[i]}" != "$1" ]] || { printf 'Step %s of %s' "$((i + 1))" "${stepTotal}"; return 0; }; done ;}
fStep(){ stepSkip=""; stepName="${stepTitle[$1]:-$1}"; stepWhere="$(fStepOf "$1")"; stepNow="$1"; fSection "$(fStepTime): ${stepWhere}; Starting ${stepName}" ;}
## PASS, SKIP, or FAIL from the exit trap for a step a failure left open.
fStepEnd(){
	local r="${1:-PASS}"
	[[ -n "${stepName}" ]] || return 0
	[[ -z "${stepSkip}" ]] || r=SKIP
	fEcho "$(fStepTime): ${stepWhere}; ${r} ${stepName}"
	stepResult["${stepNow}"]="${r}${stepSkip:+ (${stepSkip})}"
	stepName=""
}
## A step that can't run here. The run goes on, and the summary says why.
fSkip(){ stepSkip="$*"; fEcho_Clean "SKIP: $*"; }

mode=default; message=""; quiet=0; leakOpt=""; noContainer=0; noRemote=0; keepTemp=0; listOnly=0; lintCheck=0
gateAreas=(); aliasNote=""; modeOpts=()
while (($#)); do case "$1" in
	-q|--quiet)         quiet=1; shift ;;
	--quick)            modeOpts+=(quick); shift ;;
	--long|--slow)      modeOpts+=(long); shift ;;
	--leakcheck|--no-leakcheck)
		[[ -z "${leakOpt}" || "${leakOpt}" == "$1" ]] || fUsage "--leakcheck and --no-leakcheck are opposites; give one."
		leakOpt="$1"; shift ;;
	--no-container)     noContainer=1; shift ;;
	--no-remote)        noRemote=1; shift ;;
	--no-sync|--no-format|--no-lint|--no-test|--no-race|--no-fuzz|--no-soak|--no-vuln|--no-codeql|--no-build|--no-package|--no-arm64|--no-roothost|--no-rootzfs|--no-demo|--no-dogfood|--no-publish)
		stepOff["${1#--no-}"]="skipped by $1"; shift ;;
	--message=*|--msg=*|-m=*) message="${1#*=}"; shift ;;
	-m|--message|--msg) message="${2-}"; shift; (($#)) && shift ;;
	--backlog-run-quick|--gate)
		[[ "$1" == --gate ]] && aliasNote="--gate is another name for --backlog-run-quick"
		modeOpts+=(gate); shift
		while (($#)) && [[ "$1" != -* ]]; do gateAreas+=("$1"); shift; done ;;
	--backlog-run-full) modeOpts+=(full); shift ;;
	--merge-gate|--ci)
		[[ "$1" == --ci ]] && aliasNote="--ci is another name for --merge-gate"
		modeOpts+=(merge); shift ;;
	--keep-temp)        keepTemp=1; shift ;;
	--list)             listOnly=1; shift ;;
	--lint-check)       lintCheck=1; shift ;;
	-h|--help)          sed -n '/^##	- Purpose:/,/^##	History:/p' "${BASH_SOURCE[0]}" | sed '$d; s/^##	\{0,1\}//'; exit 0 ;;
	*) fUsage "unknown option: ${1} (try --help)" ;;
esac; done

if ((lintCheck)); then exec "${here}/utility/lint-report.bash" --check; fi

if ((${#modeOpts[@]})); then
	mode="${modeOpts[0]}"
	for m in "${modeOpts[@]}"; do [[ "${m}" == "${mode}" ]] || fUsage "${mode} and ${m} are separate runs; give one."; done
fi
handsOff=0; [[ "${mode}" == gate || "${mode}" == full || "${mode}" == merge ]] && handsOff=1
if ((handsOff)) && [[ -n "${message}" ]]; then fUsage "--${mode} never publishes, so it takes no --message."; fi
[[ -z "${aliasNote}" ]] || { fEcho_Clean; fEcho_Clean "${aliasNote}"; }

areaDirs=()
if [[ "${mode}" == gate ]]; then
	gateAreasAll=(cli); for d in "${root}"/source/internal/*/; do gateAreasAll+=("$(basename "${d}")"); done
	for a in "${gateAreas[@]:-}"; do
		[[ -z "${a}" ]] && continue
		[[ " ${gateAreasAll[*]} " == *" ${a} "* ]] || fUsage "--backlog-run-quick: no area '${a}'. Areas: ${gateAreasAll[*]}"
		if [[ "${a}" == cli ]]; then areaDirs+=("./${CLI_AREA_DIR}/..."); else areaDirs+=("./source/internal/${a}/..."); fi
	done
fi
pkgs=("./...")
((${#areaDirs[@]})) && pkgs=("${areaDirs[@]}")

## Which steps this mode runs, and why the rest don't.
declare -A modeSteps=(
	[default]="sync format lint test race fuzz soak vuln codeql build package arm64 roothost rootzfs demo dogfood publish"
	[long]="sync format lint test race fuzz soak vuln codeql build package arm64 roothost rootzfs demo dogfood publish"
	[quick]="sync format lint test fuzz codeql build arm64 dogfood publish"
	[gate]="format lint test codeql"
	[full]="format lint test race fuzz soak vuln codeql build package arm64 roothost rootzfs"
	[merge]="format lint test race fuzz soak vuln codeql build package arm64 roothost rootzfs"
)
if [[ "${leakOpt}" == --leakcheck ]]; then
	for s in race soak; do [[ " ${modeSteps[${mode}]} " == *" ${s} "* ]] || modeSteps[${mode}]+=" ${s}"; done
fi
[[ "${leakOpt}" != --no-leakcheck ]] || { stepOff[race]="skipped by --no-leakcheck"; stepOff[soak]="skipped by --no-leakcheck"; }
((! noRemote)) || stepOff[arm64]="skipped by --no-remote"
((! ${#areaDirs[@]})) || [[ -n "${stepOff[codeql]:-}" ]] || stepOff[codeql]="whole tree only, and areas were named"
for s in "${allSteps[@]}"; do
	if [[ -n "${stepOff[${s}]:-}" ]]; then continue
	elif [[ " ${modeSteps[${mode}]} " == *" ${s} "* ]]; then stepList+=("${s}")
	else stepOff["${s}"]="not part of the ${mode} run"; fi
done
## Keep the pipeline's own order, whatever order --leakcheck added a step in.
ordered=(); for s in "${allSteps[@]}"; do [[ " ${stepList[*]} " != *" ${s} "* ]] || ordered+=("${s}"); done
stepList=("${ordered[@]}")
stepTotal="${#stepList[@]}"

## One folder for the run's temp files, ${HOST_TEMP}/<app>/<start time>_<NNN>. TMPDIR, TEMP and TMP point
## into it. Its run.pid marks it as a run's, and is checked again before removal.
runTmp=""; runName=""; runRoot=""; runRe='^[0-9]{8}-[0-9]{6}_[0-9]{3}$'
tidyGone=(); declare -i tidyFreed=0
useContainer=0; ctrImage=""; ctrCacheVol="${APP_NAME}-go-cache"; ctrCodeqlVol="${APP_NAME}-codeql"; ctrFailed=0
dockerBefore=""
## Apparent size: what ZFS reports on disk is compressed, and lags fresh writes.
fDirBytes(){ local out=""; out="$(du -sbx -- "$1" 2>/dev/null)" || out=0; echo "${out%%[[:space:]]*}" ;}
## The project's own run folders over 2 days old, whose run is gone.
fSweepRuns(){
	local d pid size
	local -i nStale=0
	while IFS= read -r d; do
		[[ "${d##*/}" =~ ${runRe} && "${d}" != "${runTmp}" && -f "${d}/run.pid" && ! -L "${d}" ]] || continue
		pid=""; read -r pid < "${d}/run.pid" || true
		if [[ "${pid}" =~ ^[0-9]+$ ]] && kill -0 "${pid}" 2>/dev/null; then continue; fi
		size="$(fDirBytes "${d}")"
		if fSafeRm "${d}" >/dev/null; then nStale+=1; tidyFreed+=size; fi
	done < <(find "${runRoot}" -mindepth 1 -maxdepth 1 -type d -mtime +1 2>/dev/null || true)
	((nStale == 0)) || tidyGone+=("${nStale} stale run folder(s)")
}
## Containers by this run's label, then what the pipeline's images left behind. Never docker's build
## cache, never anything without the project's label, never rmi -f.
fCtrCleanup(){
	local ids="" id repo n
	command -v docker >/dev/null 2>&1 || return 0
	docker info >/dev/null 2>&1 || return 0
	if ((! keepTemp)); then
		ids="$(docker ps -aq --filter "label=dev.run=${runName}" 2>/dev/null || true)"
		if [[ -n "${ids}" ]]; then
			# shellcheck disable=SC2086 # one ID per word
			docker stop -t 20 ${ids} >/dev/null 2>&1 || true
			# shellcheck disable=SC2086
			docker rm ${ids} >/dev/null 2>&1 || true
			tidyGone+=("$(wc -w <<< "${ids}") container(s)")
		fi
		ids="$(docker images -q --filter dangling=true --filter "label=dev.project=${APP_NAME}" 2>/dev/null || true)"
		for id in ${ids}; do docker rmi "${id}" >/dev/null 2>&1 && tidyGone+=("a dangling image"); done
		## Keep the current tag and the one before it, of each image the pipeline makes.
		for repo in "${APP_NAME}-cicd" "${APP_NAME}-root-host"; do
			n=0
			while read -r id; do
				n=$((n + 1)); ((n > 2)) || continue
				docker rmi "${id}" >/dev/null 2>&1 && tidyGone+=("an old ${repo} image")
			done < <(docker images --filter "label=dev.project=${APP_NAME}" --filter "reference=${repo}" --format '{{.CreatedAt}}|{{.ID}}' 2>/dev/null | sort -r | cut -d'|' -f2)
		done
	else
		fEcho_Clean "Kept containers and images, by label dev.run=${runName}"
	fi
	## The Go cache volume stays between runs for speed, and is emptied whole when over 2 GB.
	if [[ -n "${ctrImage}" ]] && docker volume inspect "${ctrCacheVol}" >/dev/null 2>&1; then
		local mb
		mb="$(docker run --rm --user "$(id -u):$(id -g)" -v "${ctrCacheVol}:/cache" "${ctrImage}" du -sm /cache 2>/dev/null | cut -f1 || echo 0)"
		if [[ "${mb}" =~ ^[0-9]+$ ]] && ((mb > 2048)); then
			docker run --rm --user "$(id -u):$(id -g)" -v "${ctrCacheVol}:/cache" "${ctrImage}" sh -c 'chmod -R u+w /cache; find /cache -mindepth 1 -delete' >/dev/null 2>&1 \
				&& tidyGone+=("the Go cache volume (${mb} MB)")
		fi
	fi
}
fRunTmpDone(){
	local rc="${1:-0}" size="" list="" after=""
	((rc == 0)) || fStepEnd FAIL
	[[ -n "${runTmp}" ]] || return 0
	trap '' INT TERM
	fCtrCleanup || true
	if ((keepTemp)); then fEcho_Clean; fEcho_Clean "Kept the temp folder: ${runTmp}"; fEcho_Clean; fi
	if ((! keepTemp)) && [[ "${runName}" =~ ${runRe} && -f "${runTmp}/run.pid" ]]; then
		size="$(fDirBytes "${runTmp}")"
		if fSafeRm "${runTmp}" >/dev/null; then tidyGone+=("the run folder"); tidyFreed+=size; fi
	fi
	if ((! ${#tidyGone[@]})); then fEcho_Clean "Cleanup: nothing to remove"; fEcho_Clean; return 0; fi
	printf -v list '%s, ' "${tidyGone[@]}"
	fEcho_Clean "Cleanup: removed ${list%, }; freed about $(numfmt --to=si --suffix=B "${tidyFreed}" 2>/dev/null || echo "${tidyFreed} bytes") of files"
	if [[ -n "${dockerBefore}" ]] && command -v docker >/dev/null 2>&1; then
		after="$(docker system df --format '{{.Type}} {{.Size}}' 2>/dev/null | tr '\n' ';' || true)"
		fEcho_Clean "Docker: before ${dockerBefore%;}"
		fEcho_Clean "        after  ${after%;}"
	fi
	fEcho_Clean
}
if ((! listOnly)); then
	## Once, so every path under it is spelled the same.
	HOST_TEMP="$(realpath -e -- "${HOST_TEMP:-${TMPDIR:-/tmp}}")" || fDie "no temp folder at ${HOST_TEMP:-${TMPDIR:-/tmp}}"
	export HOST_TEMP
	runRoot="${HOST_TEMP}/${APP_NAME}"
	mkdir -p -- "${runRoot}" || fDie "can't make ${runRoot}"
	## A plain mkdir, so a name another run took fails and the counter goes on.
	for ((runSeq = 1; runSeq < 1000; runSeq++)); do
		printf -v runTmp '%s/%s_%03d' "${runRoot}" "${stamp}" "${runSeq}"
		mkdir -- "${runTmp}" 2>/dev/null && break
		runTmp=""
	done
	[[ -n "${runTmp}" ]] || fDie "can't make a run folder in ${runRoot}"
	runName="${runTmp##*/}"
	echo "$$" > "${runTmp}/run.pid"
	trap 'fRunTmpDone "$?"' EXIT
	trap 'exit 130' INT; trap 'exit 143' TERM
	export TMPDIR="${runTmp}" TEMP="${runTmp}" TMP="${runTmp}"
	fSweepRuns
fi
trap 'rc=$?; printf "\n[ CICD ABORTED (exit %s) at line %s: %s ]\n" "${rc}" "${LINENO}" "${BASH_COMMAND}" >&2; exit "${rc}"' ERR

publishMsg=""
if   [[ -n "${message}" ]]; then publishMsg="${message}"
elif ((quiet));             then publishMsg="${APP_NAME} CI/CD ${stamp}"
fi

fGlobFiles(){ ## OUT_ARRAY GLOB...
	local -n out_gF="$1"; shift
	local g f hadNullglob=0
	out_gF=()
	shopt -q nullglob && hadNullglob=1; shopt -s nullglob
	for g in "$@"; do for f in ${g}; do [[ -f "${f}" ]] && out_gF+=("${f}"); done; done
	((hadNullglob)) || shopt -u nullglob
}
fGlobFiles shellFiles "${SHELL_LINT_GLOBS[@]}"
fGlobFiles mdFiles "${MD_LINT_GLOBS[@]}"

## The version, read from its one place.
goVersion="$(sed -n 's/^var version = "\(.*\)".*/\1/p' "${root}/${VERSION_FILE}" | head -n 1)"
[[ -n "${goVersion}" ]] || fDie "no version in ${VERSION_FILE}"
fCommitEpoch(){ git log -1 --format=%ct 2>/dev/null || echo 0 ;}
fIsRelease(){ [[ "$(git branch --show-current 2>/dev/null)" == main && "$(git describe --exact-match --tags HEAD 2>/dev/null || true)" == "${goVersion}" ]] && echo 1 || echo 0 ;}

## A host tool by name: on PATH, or where go install puts it.
fHostTool(){ local f="" d
	f="$(command -v "$1" 2>/dev/null || true)"
	for d in "${HOME}/go/bin/bin" "${HOME}/.go/bin" "${HOME}/go/bin"; do [[ -n "${f}" || ! -x "${d}/$1" ]] || f="${d}/$1"; done
	echo "${f}"
}

## The container. Built once per recipe, from cicd/container, with the pinned versions as build args.
fCtrSetup(){
	local tag
	((! noContainer)) || { fEcho_Clean "Container: off (--no-container), running on this box"; return 0; }
	if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then fEcho_Clean "Container: no docker here, running on this box"; return 0; fi
	tag="${APP_NAME}-cicd:$({ cat "${here}/container/Dockerfile" "${here}/container/markdownlint/package-lock.json"; grep -E '^(GO_IMAGE|STATICCHECK_VERSION|GOVULNCHECK_VERSION|GOSEC_VERSION|NFPM_VERSION|NODE_[A-Z0-9_]*|SHELLCHECK_[A-Z0-9_]*)=' "${here}/config.bash"; } | sha256sum | cut -c1-12)"
	if ! docker image inspect "${tag}" >/dev/null 2>&1; then
		fEcho_Clean "Container: building ${tag}"
		if ! nice -n 19 docker build -q --label "dev.project=${APP_NAME}" --label "dev.run=${runName}" \
			--build-arg "GO_IMAGE=${GO_IMAGE}" --build-arg "STATICCHECK_VERSION=${STATICCHECK_VERSION}" --build-arg "GOVULNCHECK_VERSION=${GOVULNCHECK_VERSION}" \
			--build-arg "GOSEC_VERSION=${GOSEC_VERSION}" --build-arg "NFPM_VERSION=${NFPM_VERSION}" \
			--build-arg "NODE_VERSION=${NODE_VERSION}" --build-arg "NODE_SHA256_AMD64=${NODE_SHA256_AMD64}" --build-arg "NODE_SHA256_ARM64=${NODE_SHA256_ARM64}" \
			--build-arg "SHELLCHECK_VERSION=${SHELLCHECK_VERSION}" --build-arg "SHELLCHECK_SHA256_AMD64=${SHELLCHECK_SHA256_AMD64}" \
			--build-arg "SHELLCHECK_SHA256_ARM64=${SHELLCHECK_SHA256_ARM64}" -t "${tag}" "${here}/container" >/dev/null; then
			fEcho_Clean "Container: the image would not build, running on this box"; return 0
		fi
	fi
	if ! docker volume inspect "${ctrCacheVol}" >/dev/null 2>&1; then
		docker volume create --label "dev.project=${APP_NAME}" --label "dev.run=${runName}" "${ctrCacheVol}" >/dev/null
		docker run --rm --user 0 -v "${ctrCacheVol}:/cache" "${tag}" chmod 1777 /cache
	fi
	ctrImage="${tag}"; useContainer=1
	dockerBefore="$(docker system df --format '{{.Type}} {{.Size}}' 2>/dev/null | tr '\n' ';' || true)"
	fEcho_Clean "Container: ${tag}"
}
## fGo STEP [VAR=value...] -- command...: a Go tool, in the container or on this box, from the repo root.
## HEAVY=1 gives it the hard steps' cores, and MOUNT=VOLUME:PATH adds a volume in the container.
fGo(){
	local step="$1" envs=() memArgs=() jobs="${BUILD_JOBS}" e
	shift
	while (($#)) && [[ "$1" != -- ]]; do envs+=("$1"); shift; done
	shift
	for e in "${envs[@]:-}"; do [[ "${e}" != HEAVY=1 ]] || jobs="${HEAVY_JOBS}"; done
	if ((useContainer)); then
		fMemCtrArgs memArgs
		local envArgs=() w
		for w in "${envs[@]:-}"; do
			case "${w}" in ""|HEAVY=1) ;; MOUNT=*) envArgs+=(-v "${w#MOUNT=}") ;; *) envArgs+=(-e "${w}") ;; esac
		done
		docker run --rm --name "${APP_NAME}-${runName}-${step}" --label "dev.project=${APP_NAME}" --label "dev.run=${runName}" \
			"${memArgs[@]}" --cpus "${jobs}" --cpu-shares 256 --user "$(id -u):$(id -g)" \
			-e HOME=/cache/home -e GOCACHE=/cache/gocache -e GOMODCACHE=/cache/gomod -e "GOMAXPROCS=${jobs}" \
			"${envArgs[@]}" -v "${ctrCacheVol}:/cache" -v "${root}:/src:ro" -v "${runTmp}:${runTmp}" \
			-v /etc/passwd:/etc/passwd:ro -v /etc/group:/etc/group:ro -w /src \
			"${ctrImage}" nice -n 19 "$@"
	else
		( for w in "${envs[@]:-}"; do [[ -z "${w}" || "${w}" == HEAVY=1 || "${w}" == MOUNT=* ]] || export "${w?}"; done
		  export GOMAXPROCS="${jobs}" GOFLAGS="${GOFLAGS:+${GOFLAGS} }-buildvcs=false"
		  cmd="$1"; shift
		  case "${cmd}" in
			staticcheck|gosec|govulncheck|nfpm) cmd="$(fHostTool "${cmd}")"; [[ -n "${cmd}" ]] || { echo "not installed: $1" >&2; exit 127; } ;;
		  esac
		  "${cmd}" "$@" )
	fi
}
## Go packages, as the arguments for a tool: all of them, or the named areas.
fPkgs(){ printf '%s\n' "${pkgs[@]}"; }

lintLog=""
fLintOut(){ if [[ -n "${lintLog}" ]]; then tee -a "${lintLog}"; else cat; fi; }

fStepSync(){
	if ! git rev-parse --abbrev-ref '@{u}' >/dev/null 2>&1; then
		fEcho_Clean "no upstream for this branch - nothing to sync"
	elif ! git fetch --quiet 2>/dev/null; then
		fEcho_Clean "WARNING: can't reach origin - building without refreshing"
	else
		local counts behind ahead
		counts="$(git rev-list --left-right --count '@{u}...HEAD' 2>/dev/null || printf '0\t0')"
		behind="${counts%%[[:space:]]*}"; ahead="${counts##*[[:space:]]}"
		if   ((behind == 0)); then fEcho_Clean "up to date with origin (${ahead} to publish)"
		elif ((ahead > 0)); then
			## Diverged: merge when it merges cleanly, otherwise put the tree back and stop.
			fEcho_Clean "diverged (${ahead} local, ${behind} remote); merging upstream with --no-ff"
			git merge --no-ff --no-edit --autostash '@{u}' >/dev/null 2>&1 || { git merge --abort >/dev/null 2>&1 || true; fDie "upstream doesn't merge cleanly; reconcile before building"; }
		else
			fEcho_Clean "fast-forwarding ${behind} commit(s) from origin"
			git merge --ff-only --autostash '@{u}' || fDie "fast-forward from origin failed"
		fi
	fi
	## Read after the sync, which can move HEAD.
	goVersion="$(sed -n 's/^var version = "\(.*\)".*/\1/p' "${root}/${VERSION_FILE}" | head -n 1)"
}

formatChanged=()
fStepFormat(){
	local bad
	bad="$(gofmt -l source 2>/dev/null || true)"
	if [[ -z "${bad}" ]]; then fEcho_Clean "gofmt: clean"; return 0; fi
	if ((handsOff)) || [[ "${mode}" == gate ]]; then
		printf '%s\n' "${bad}"; fDie "gofmt wants to reformat the files above"
	fi
	## A run that publishes may reformat, and records which files it touched so publish adds only those.
	local f
	while IFS= read -r f; do gofmt -w "${f}"; formatChanged+=("${f}"); done <<< "${bad}"
	fEcho_Clean "gofmt: reformatted ${#formatChanged[@]} file(s)"
}

fStepLint(){
	local f drift=() spec have
	if [[ -n "${LINT_LOG_DIR:-}" ]]; then lintLog="${root}/${LINT_LOG_DIR}/lint_${stamp}.txt"; fi
	for f in "${shellFiles[@]}"; do bash -n "${f}" || fDie "syntax error: ${f}"; done
	fEcho_Clean "bash -n: ${#shellFiles[@]} file(s)"
	fGo lint -- go vet "${pkgs[@]}" 2>&1 | fLintOut || fDie "go vet findings"
	fEcho_Clean "go vet: clean"
	fGo lint -- staticcheck "${pkgs[@]}" 2>&1 | fLintOut || fDie "staticcheck findings"
	fEcho_Clean "staticcheck: clean"
	fGo lint -- gosec -quiet "${pkgs[@]}" 2>&1 | fLintOut || fDie "gosec findings"
	fEcho_Clean "gosec: clean"
	if ((${#areaDirs[@]})); then return 0; fi
	## The image has both at the pinned versions; without it, the host's copies, if any.
	if ((useContainer)) || command -v shellcheck >/dev/null 2>&1; then
		fGo lint -- shellcheck "${shellFiles[@]}" 2>&1 | fLintOut || fDie "shellcheck findings"
		fEcho_Clean "shellcheck: clean"
	else fEcho "WARNING: shellcheck skipped (not installed)"; fi
	if ((useContainer)) || command -v markdownlint >/dev/null 2>&1; then
		fGo lint -- markdownlint "${mdFiles[@]}" 2>&1 | fLintOut || fDie "markdownlint findings"
		fEcho_Clean "markdownlint: clean (${#mdFiles[@]} file(s))"
	else fEcho "WARNING: markdownlint skipped (not installed)"; fi
	"${here}/utility/test-id.bash" --check -q || fDie "test IDs (see above)"
	fEcho_Clean "test IDs: every test has its own"
	((! useContainer)) || return 0
	for spec in "${TOOL_VERSIONS[@]}"; do
		case "${spec%%=*}" in
			shellcheck)   have="$(shellcheck --version 2>/dev/null | awk '$1=="version:"{print $2}')" ;;
			markdownlint) have="$(markdownlint --version 2>/dev/null || true)" ;;
			*) have="" ;;
		esac
		[[ -z "${have}" || "${have}" == "${spec#*=}" ]] || drift+=("${spec%%=*} ${have} (recorded ${spec#*=})")
	done
	((${#drift[@]} == 0)) || fEcho "WARNING: tool versions differ from the recorded set: ${drift[*]}"
}

## One line per test with its ID, from the test binary's own output, then the verdict.
fGoTestRun(){ ## LABEL [VAR=value...]: go test over the chosen packages, to a log in the run folder
	local label="$1" log rc=0 extra=()
	shift
	log="${runTmp}/gotest_${label}.log"
	[[ "${leakOpt}" != --no-leakcheck ]] || extra+=(CICD_NO_LEAKCHECK=1)
	fGo test "${extra[@]}" "$@" -- go test -count=1 -p "${testJobs}" -v "${pkgs[@]}" > "${log}" 2>&1 || rc=$?
	grep -E '^(PASS|FAIL|SKIP) [0-9a-z]{5} ' "${log}" | sed 's/^/  /' || true
	if ((rc)); then
		grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|SKIP)|PASS [0-9a-z]{5} |SKIP [0-9a-z]{5} )' "${log}" | tail -n 80 || true
		fDie "go test failed (${label})"
	fi
	fEcho_Clean "go test (${label}): $(grep -cE '^PASS [0-9a-z]{5} ' "${log}" || true) test line(s) passed"
}
fStepTest(){
	local fresh="${runTmp}/gotmp"
	testJobs="${BUILD_JOBS}"
	mkdir -- "${fresh}"
	## Twice, the second with TMPDIR on this run's folder: ext4 there caught an inode-reuse bug /tmp hid.
	if ((useContainer)); then fGoTestRun default; else fGoTestRun default "TMPDIR=${TMPDIR_DEFAULT:-/tmp}"; fi
	fGoTestRun fresh "TMPDIR=${fresh}"
}
fStepRace(){
	local log="${runTmp}/gotest_race.log" rc=0 fresh="${runTmp}/gorace" extra=()
	mkdir -- "${fresh}"
	[[ "${leakOpt}" != --no-leakcheck ]] || extra+=(CICD_NO_LEAKCHECK=1)
	## -race needs cgo, and the container has gcc.
	fGo race "${extra[@]}" HEAVY=1 CGO_ENABLED=1 "TMPDIR=${fresh}" -- go test -race -count=1 -p "${HEAVY_JOBS}" -v "${pkgs[@]}" > "${log}" 2>&1 || rc=$?
	if ((rc)); then grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|SKIP)|PASS [0-9a-z]{5} |SKIP [0-9a-z]{5} )' "${log}" | tail -n 80 || true; fDie "race test failed"; fi
	fEcho_Clean "go test -race: $(grep -cE '^PASS [0-9a-z]{5} ' "${log}" || true) test line(s) passed"
}
fStepVuln(){
	local out rc=0
	out="$(fGo vuln -- govulncheck -test "${pkgs[@]}" 2>&1)" || rc=$?
	if ((rc)); then
		## Offline is a skip. A real finding says "Vulnerability #".
		if grep -q 'Vulnerability #' <<< "${out}"; then grep -E 'Vulnerability #|Found in|Fixed in|More info|affected by' <<< "${out}" | fLintOut; fDie "govulncheck findings"; fi
		fSkip "govulncheck can't reach vuln.go.dev"; return 0
	fi
	fEcho_Clean "govulncheck: clean"
}

## Each Fuzz target for its budget, from a copy of the tree: go test writes a failing input under
## the package's testdata, and the tree is read-only in the container.
fStepFuzz(){
	local copy="${runTmp}/fuzzsrc" log="${runTmp}/fuzz.log" budget="${FUZZ_TIME}" f name dir rc n=0 found
	case "${mode}" in quick) budget="${FUZZ_TIME_QUICK}" ;; long) budget="${FUZZ_TIME_LONG}" ;; esac
	budget="${CICD_FUZZ_TIME:-${budget}}"
	[[ "${budget}" =~ ^[0-9]+[smh]$ ]] || fDie "fuzz time '${budget}' isn't a count of s, m or h"
	mkdir -- "${copy}"
	cp -a -- source go.mod go.sum "${copy}/"
	fEcho_Clean "${budget} per target, ${HEAVY_JOBS} worker(s)"
	while IFS= read -r f; do
		dir="./$(dirname "${f}")"
		while IFS= read -r name; do
			n=$((n + 1)); rc=0
			fGo fuzz HEAVY=1 -- go -C "${copy}" test -count=1 -run '^$' -fuzz "^${name}\$" -fuzztime "${budget}" -parallel "${HEAVY_JOBS}" "${dir}" > "${log}" 2>&1 || rc=$?
			grep -E '^(PASS|FAIL|SKIP) [0-9a-z]{5} ' "${log}" | sed 's/^/  /' || true
			if ((rc)); then
				grep -vE '^(fuzz: elapsed|(PASS|FAIL|SKIP) [0-9a-z]{5} )' "${log}" | tail -n 40 || true
				found="$(find "${copy}/${dir#./}/testdata/fuzz/${name}" -type f -newer "${copy}/go.mod" 2>/dev/null | head -n 1)"
				[[ -z "${found}" ]] || { fEcho_Clean "failing input: ${found}"; sed 's/^/  /' "${found}"; }
				fDie "fuzzing found a failure in ${name} (${dir})"
			fi
		done < <(sed -nE 's/^func (Fuzz[A-Za-z0-9_]*)\(.*/\1/p' "${f}")
	done < <(grep -rlE '^func Fuzz' --include='*_test.go' source | sort)
	((n)) || { fSkip "no Fuzz targets in the tree"; return 0; }
	fEcho_Clean "fuzzing: ${n} target(s), nothing found"
}

## Each TestSoak, with the rounds set: the tests skip without them.
fStepSoak(){
	local log="${runTmp}/soak.log" rounds="${SOAK_ROUNDS}" rc=0 n
	[[ "${mode}" != long ]] || rounds="${SOAK_ROUNDS_LONG}"
	rounds="${CICD_SOAK_ROUNDS:-${rounds}}"
	[[ "${rounds}" =~ ^[1-9][0-9]*$ ]] || fDie "soak rounds '${rounds}' isn't a count"
	fEcho_Clean "rounds ${rounds} and $((rounds * 2)), ${HEAVY_JOBS} worker(s)"
	fGo soak HEAVY=1 "${SOAK_ENV}=${rounds}" -- go test -count=1 -p "${HEAVY_JOBS}" -v -run '^TestSoak$' "${pkgs[@]}" > "${log}" 2>&1 || rc=$?
	## A test's numbers, then its PASS or FAIL line: go test prints each package's output in one piece.
	grep -E '^((PASS|FAIL|SKIP) [0-9a-z]{5} |[[:space:]]+soak_test\.go:[0-9]+: )' "${log}" | sed -E 's/^[[:space:]]*/  /' || true
	if ((rc)); then
		grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|SKIP)|(PASS|FAIL|SKIP) [0-9a-z]{5} |[[:space:]]+soak_test\.go:)' "${log}" | tail -n 40 || true
		fDie "soak test failed"
	fi
	## A skip here means the rounds never reached the test, so it checked nothing.
	! grep -qE '^SKIP [0-9a-z]{5} ' "${log}" || fDie "a soak test skipped: ${SOAK_ENV} didn't reach it"
	n="$(grep -cE '^PASS [0-9a-z]{5} ' "${log}" || true)"
	((n)) || { fSkip "no soak tests in ${pkgs[*]}"; return 0; }
	fEcho_Clean "soak: ${n} test(s), nothing grew"
}

## The pinned bundle, fetched once into its cache, run over a copy of the tracked Go files so stray
## files in the tree don't count. Any result fails the step.
fCodeqlRam(){ ## memory for codeql's --ram, in MB: 8 GB, or three quarters of a smaller cap
	local mb=8192 n
	if [[ "${memCap:-none}" =~ ^([0-9]+)([kmg]?)$ ]]; then
		n="${BASH_REMATCH[1]}"
		case "${BASH_REMATCH[2]}" in g) n=$((10#${n} * 1024)) ;; m) n=$((10#${n})) ;; k) n=$((10#${n} / 1024)) ;; *) n=$((10#${n} / 1048576)) ;; esac
		((n * 3 / 4 >= mb)) || mb=$((n * 3 / 4))
	fi
	((mb >= 1024)) || mb=1024
	echo "${mb}"
}
fStepCodeql(){
	local cache copy="${runTmp}/codeqlsrc" db="${runTmp}/codeqldb" out="${runTmp}/codeql.csv" log="${runTmp}/codeql.log" cql ram f
	local mount=() files=() rc=0 n re
	if [[ "$(uname -s)/$(uname -m)" != Linux/x86_64 ]]; then fSkip "CodeQL has no build for $(uname -s)/$(uname -m)"; return 0; fi
	if ((useContainer)); then
		cache=/codeql; mount=("MOUNT=${ctrCodeqlVol}:/codeql")
		if ! docker volume inspect "${ctrCodeqlVol}" >/dev/null 2>&1; then
			docker volume create --label "dev.project=${APP_NAME}" --label "dev.run=${runName}" "${ctrCodeqlVol}" >/dev/null
			docker run --rm --user 0 -v "${ctrCodeqlVol}:/codeql" "${ctrImage}" chmod 1777 /codeql
		fi
	else
		cache="${XDG_CACHE_HOME:-${HOME}/.cache}/${APP_NAME}/codeql"
		command -v zstd >/dev/null 2>&1 || { fSkip "zstd isn't installed here, to unpack the CodeQL bundle"; return 0; }
	fi
	fGo codeql "${mount[@]}" -- bash cicd/utility/codeql-fetch.bash "${cache}" > "${log}" 2>&1 || rc=$?
	if ((rc)); then
		tail -n 20 "${log}"
		## A box that can't reach GitHub skips, as the vulnerability scan does; a wrong sum fails.
		grep -q 'curl: ([0-9]*) \(Could not resolve\|Failed to connect\|Connection timed out\)' "${log}" && { fSkip "can't reach GitHub for the CodeQL bundle"; return 0; }
		fDie "the CodeQL bundle didn't fetch or check out"
	fi
	sed 's/^/  /' "${log}"
	cql="${cache}/${CODEQL_VERSION}/codeql/codeql"
	ram="$(fCodeqlRam)"
	mkdir -- "${copy}"
	while IFS= read -r -d '' f; do [[ ! -f "${f}" ]] || files+=("${f}"); done < <(git ls-files -z -- source go.mod go.sum)
	((${#files[@]})) || fDie "no tracked Go files to analyze"
	cp --parents -t "${copy}" -- "${files[@]}"
	fEcho_Clean "${#files[@]} tracked file(s), ${HEAVY_JOBS} thread(s), ${ram} MB, suite ${CODEQL_SUITE#*:}"
	rc=0
	fGo codeql HEAVY=1 "${mount[@]}" -- "${cql}" database create "${db}" --language=go --source-root="${copy}" \
		--command="go build -p ${HEAVY_JOBS} ./..." --threads="${HEAVY_JOBS}" --ram="${ram}" >> "${log}" 2>&1 || rc=$?
	((rc == 0)) || { tail -n 40 "${log}"; fDie "codeql database create failed"; }
	fGo codeql HEAVY=1 "${mount[@]}" -- "${cql}" database analyze "${db}" "${CODEQL_SUITE}" --format=csv --output="${out}" \
		--threads="${HEAVY_JOBS}" --ram="${ram}" >> "${log}" 2>&1 || rc=$?
	((rc == 0)) || { tail -n 40 "${log}"; fDie "codeql database analyze failed"; }
	## name, description, severity, message, path, line, ...
	re='^"(([^"]|"")*)","(([^"]|"")*)","(([^"]|"")*)","(([^"]|"")*)","(([^"]|"")*)","([0-9]+)"'
	while IFS= read -r f; do
		if [[ "${f}" =~ ${re} ]]; then printf '  %s:%s: %s: %s (%s)\n' "${BASH_REMATCH[9]#/}" "${BASH_REMATCH[11]}" "${BASH_REMATCH[5]}" "${BASH_REMATCH[7]//\"\"/\"}" "${BASH_REMATCH[1]}"
		else printf '  %s\n' "${f}"; fi
	done < "${out}" | fLintOut
	n="$(grep -c . "${out}" || true)"
	((n == 0)) || fDie "CodeQL: ${n} result(s)"
	fEcho_Clean "CodeQL ${CODEQL_VERSION}: no results"
}

relDir=""
fStepBuild(){
	local t os arch out epoch rel sum1 sum2
	epoch="$(fCommitEpoch)"; rel="$(fIsRelease)"
	relDir="${root}/${RELEASE_DIR}/${goVersion}"
	mkdir -p -- "${relDir}"
	for t in "${RELEASE_TARGETS[@]}"; do
		os="${t%%/*}"; arch="${t##*/}"
		out="${runTmp}/build/${EXE_NAME}-${os}-${arch}"
		mkdir -p -- "${runTmp}/build"
		fGo build "GOOS=${os}" "GOARCH=${arch}" CGO_ENABLED=0 GOAMD64=v1 -- go build "${GO_BUILD_FLAGS[@]}" -p "${BUILD_JOBS}" \
			-ldflags "${GO_LDFLAGS_COMMON} -X main.version=${goVersion} -X main.buildEpoch=${epoch} -X main.release=${rel}" -o "${out}" "${GO_MAIN_PKG}" \
			|| fDie "go build failed for ${os}/${arch}"
		cp -f -- "${out}" "${relDir}/${EXE_NAME}-${os}-${arch}"
		fEcho_Clean "built ${EXE_NAME}-${os}-${arch}  $(sha256sum "${out}" | cut -c1-16)  $(stat -c %s "${out}") bytes"
	done
	if [[ "${mode}" != quick ]]; then
		## The same source has to give the same bytes, from another path.
		mkdir -p -- "${runTmp}/build2"
		fGo build GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOAMD64=v1 -- go build "${GO_BUILD_FLAGS[@]}" -p "${BUILD_JOBS}" \
			-ldflags "${GO_LDFLAGS_COMMON} -X main.version=${goVersion} -X main.buildEpoch=${epoch} -X main.release=${rel}" -o "${runTmp}/build2/${EXE_NAME}-linux-amd64" "${GO_MAIN_PKG}" \
			|| fDie "go build failed (second build)"
		sum1="$(sha256sum < "${runTmp}/build/${EXE_NAME}-linux-amd64")"; sum2="$(sha256sum < "${runTmp}/build2/${EXE_NAME}-linux-amd64")"
		[[ "${sum1}" == "${sum2}" ]] || fDie "the same source built to two different binaries"
		fEcho_Clean "reproducible: two builds of linux/amd64 give the same checksum"
	fi
	fEcho_Clean "$("${runTmp}/build/${EXE_NAME}-linux-amd64" --version 2>&1 || true)"
}
fStepPackage(){
	local t arch f ver="${goVersion#v}" yaml epoch fmt
	epoch="$(fCommitEpoch)"
	[[ -d "${relDir}" ]] || fDie "no build to package"
	for t in "${RELEASE_TARGETS[@]}"; do
		arch="${t##*/}"
		yaml="${runTmp}/nfpm_${arch}.yaml"
		cat > "${yaml}" <<EOF_YAML
name: ${EXE_NAME}
arch: ${arch}
platform: linux
version: ${ver}
maintainer: t00mietum
description: A hardened Paper Minecraft server, run in Podman, Docker, or natively.
license: GPL-2.0-or-later
homepage: https://github.com/t00mietum/tt-minecraft-safeserver
contents:
  - src: ${runTmp}/build/${EXE_NAME}-linux-${arch}
    dst: /usr/bin/${EXE_NAME}
    file_info:
      mode: 0755
  - src: license.md
    dst: /usr/share/doc/${EXE_NAME}/license.md
EOF_YAML
		for fmt in deb rpm; do
			fGo package "SOURCE_DATE_EPOCH=${epoch}" -- nfpm package -p "${fmt}" -f "${yaml}" -t "${runTmp}/pkg_${arch}.${fmt}" > "${runTmp}/nfpm.log" 2>&1 \
				|| { cat "${runTmp}/nfpm.log"; fDie "nfpm failed for ${fmt} ${arch}"; }
			f="${runTmp}/pkg_${arch}.${fmt}"
			cp -f -- "${f}" "${relDir}/${EXE_NAME}_${ver}_${arch}.${fmt}"
			fEcho_Clean "packaged ${EXE_NAME}_${ver}_${arch}.${fmt}"
		done
	done
	(cd "${relDir}" && find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%f\n' | sort | xargs sha256sum > "${runTmp}/sums" && cp -f -- "${runTmp}/sums" SHA256SUMS)
	fEcho_Clean "SHA256SUMS: $(wc -l < "${relDir}/SHA256SUMS") file(s)"
	fReleaseRotate
}
## One folder per version. Keep the newest few, by modification time.
fReleaseRotate(){
	local d n=0
	while IFS= read -r d; do
		n=$((n + 1)); ((n > RELEASE_KEEP)) || continue
		fSafeRmUnder "${root}/${RELEASE_DIR}" "${d}" >/dev/null && tidyGone+=("old release folder ${d##*/}")
	done < <(find "${root}/${RELEASE_DIR}" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -rn | cut -d' ' -f2-)
}
## The arm64 test binaries, built here and run on the arm64 box inside the host lock's wrap. The
## helper leaves "reached" once the box answers, so a box that never did is a SKIP, not a FAIL.
fStepArm64(){
	local tool bin="${runTmp}/arm64bin" log="${runTmp}/arm64.log" state="${runTmp}/arm64state" rc=0 testRc="" d extra=() lockWait="${ARM64_LOCK_WAIT}"
	## The tests take about a minute there, but a quick run doesn't sit in line for the box.
	[[ "${lockWait}" =~ ^[0-9]{1,6}$ ]] || lockWait=600
	[[ "${mode}" != quick ]] || ((lockWait <= 60)) || lockWait=60
	tool="$(command -v -- "${VM_STATE_TOOL}" 2>/dev/null || true)"
	if [[ -z "${tool}" ]]; then fSkip "no ${VM_STATE_TOOL} here to hold ${ARM64_HOST}"; return 0; fi
	mkdir -- "${bin}" "${state}"
	fGo arm64 GOOS=linux GOARCH=arm64 CGO_ENABLED=0 -- go test -c "${GO_BUILD_FLAGS[@]}" -p "${BUILD_JOBS}" -o "${bin}/" ./... > "${runTmp}/arm64build.log" 2>&1 \
		|| { cat "${runTmp}/arm64build.log"; fDie "arm64 test build failed"; }
	## Package folder basenames are unique here, as go test -c -o DIR/ needs.
	while IFS= read -r d; do [[ ! -f "${bin}/${d##*/}.test" ]] || printf '%s %s\n' "${d##*/}.test" "${d}"; done \
		< <(find source -name '*_test.go' -printf '%h\n' | sort -u) > "${bin}/pkgdirs"
	fEcho_Clean "built $(wc -l < "${bin}/pkgdirs") arm64 test binaries; waiting up to ${lockWait}s for ${ARM64_HOST}"
	[[ "${leakOpt}" != --no-leakcheck ]] || extra+=(CICD_NO_LEAKCHECK=1)
	"${tool}" wrap "${ARM64_HOST}" --wait "${lockWait}" --why "${APP_NAME}: arm64 tests" -- \
		bash "${here}/utility/arm64-tests.bash" "${ARM64_SSH}" "${bin}" "${runName}" "${state}" "${extra[@]}" > "${log}" 2>&1 || rc=$?
	if [[ ! -f "${state}/reached" ]]; then
		tail -n 5 "${log}" | sed 's/^/  /'
		fSkip "${ARM64_HOST} isn't there, or stayed busy past ${lockWait}s; exit ${rc}"; return 0
	fi
	grep -E '^(box: |work folder: |removed |arm64-tests: )' "${log}" || true
	grep -E '^(PASS|FAIL|SKIP) [0-9a-z]{5} ' "${log}" | sed 's/^/  /' || true
	if [[ -f "${state}/rc" ]]; then read -r testRc < "${state}/rc" || true; fi
	if [[ "${testRc}" != 0 ]]; then
		grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|SKIP)|PASS [0-9a-z]{5} |SKIP [0-9a-z]{5} )' "${log}" | tail -n 80 || true
		fDie "arm64 tests failed on ${ARM64_HOST}"
	fi
	((rc == 0)) || fEcho "WARNING: ${VM_STATE_TOOL} exited ${rc} after the tests passed; check ${ARM64_HOST} went back as found"
	fEcho_Clean "arm64 on ${ARM64_HOST}: $(grep -cE '^PASS [0-9a-z]{5} ' "${log}" || true) test line(s) passed"
}
fRootHost(){ ## [--zfs]
	local log="${runTmp}/roothost${1:-}.log" rc=0
	if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then fSkip "no docker here"; return 0; fi
	nice -n 19 bash "${here}/root-host/root-host.bash" "${@}" > "${log}" 2>&1 || rc=$?
	grep -E '^(PASS|FAIL|SKIP) [0-9a-z]{5} ' "${log}" | sed 's/^/  /' || true
	if ((rc)); then tail -n 60 "${log}"; fDie "root host checks failed"; fi
	fEcho_Clean "root host: $(grep -cE '^PASS ' "${log}" || true) check(s) passed"
}
## The README's CLI demo, from the linux/amd64 build. A render of an unreleased build stays in the
## artifacts; only a release run updates the tracked copy.
demoChanged=()
fStepDemo(){
	local src="${runTmp}/build/${EXE_NAME}-linux-amd64" out="${runTmp}/demo.gif" log="${runTmp}/demo.log" memArgs=()
	((useContainer)) || { fSkip "needs the build container, since it writes a made-up install under /etc and /srv"; return 0; }
	[[ -x "${src}" ]] || { fSkip "no linux/amd64 build in this run"; return 0; }
	fMemCtrArgs memArgs
	docker run --rm --name "${APP_NAME}-${runName}-demo" --label "dev.project=${APP_NAME}" --label "dev.run=${runName}" \
		"${memArgs[@]}" --cpus "${BUILD_JOBS}" --cpu-shares 256 --network none --user 0 -e "OWNER=$(id -u):$(id -g)" \
		-v "${root}:/src:ro" -v "${runTmp}:${runTmp}" "${ctrImage}" nice -n 19 bash /src/cicd/utility/demo.bash "${src}" "${runTmp}/demo" "${out}" \
		> "${log}" 2>&1 || { tail -n 30 "${log}"; fDie "the demo didn't record"; }
	tail -n 1 "${log}"
	mkdir -p -- "${root}/${DEMO_DIR}"
	cp -f -- "${out}" "${root}/${DEMO_DIR}/demo_${stamp}.gif"
	gfs_rotate "${root}/${DEMO_DIR}" demo gif >/dev/null 2>&1 || true
	fEcho_Clean "kept in ${DEMO_DIR}: $(find "${root}/${DEMO_DIR}" -maxdepth 1 -name 'demo_*.gif' | wc -l) render(s)"
	if [[ "$(fIsRelease)" == 1 ]]; then
		mkdir -p -- "$(dirname -- "${root}/${DEMO_GIF}")"
		cp -f -- "${out}" "${root}/${DEMO_GIF}"
		if git ls-files --error-unmatch -- "${DEMO_GIF}" >/dev/null 2>&1 && git diff --quiet -- "${DEMO_GIF}"; then fEcho_Clean "release: ${DEMO_GIF} is unchanged"
		else demoChanged=("${DEMO_GIF}"); fEcho_Clean "release: ${DEMO_GIF} updated, for the publish stage to commit"; fi
	fi
}
fStepDogfood(){
	local d="${DOGFOOD_DESTS[0]}" exe="${EXE_NAME}" src="${relDir}/${EXE_NAME}-linux-amd64"
	[[ -f "${src}" ]] || fDie "no native build to copy"
	if [[ -e "${d}/${exe}" ]] && command -v fuser >/dev/null 2>&1 && fuser -s "${d}/${exe}" 2>/dev/null; then
		fSkip "${d}/${exe} is in use"; return 0
	fi
	if [[ -d "${d}" && -w "${d}" ]]; then
		cp -f -- "${src}" "${d}/${exe}"; chmod +x "${d}/${exe}"
	## The final destination is root's, and an unattended run never asks for a password.
	elif sudo -n true 2>/dev/null; then
		sudo -n install -m 0755 -- "${src}" "${d}/${exe}" || fDie "sudo install to ${d} failed"
	else
		fSkip "${d} needs root; run: sudo install -m 0755 ${src} ${d}/${exe}"; return 0
	fi
	fEcho_Clean "installed -> ${d}/${exe}"
}
fGitTool(){ if command -v gitsby >/dev/null 2>&1; then gitsby raw git "$@"; else git "$@"; fi; }
fStepPublish(){
	local f branch changed=("${formatChanged[@]}" "${demoChanged[@]}")
	if ((${#changed[@]})); then
		[[ -n "${publishMsg}" ]] || publishMsg="${APP_NAME} CI/CD ${stamp}"
		for f in "${changed[@]}"; do fGitTool add -- "${f}"; done
		fGitTool commit -m "${publishMsg}" -- "${changed[@]}" >/dev/null
		fEcho_Clean "committed ${#changed[@]} file(s) the run changed: ${changed[*]}: \"${publishMsg}\""
	else
		fEcho_Clean "nothing for the run to commit"
	fi
	branch="$(git branch --show-current)"
	if [[ -z "${branch}" ]]; then fSkip "no branch to push"; return 0; fi
	if GIT_SSH_COMMAND='ssh' fGitTool push -u origin "${branch}" >/dev/null 2>&1; then fEcho_Clean "pushed ${branch}"; else fEcho "WARNING: push of ${branch} failed"; fi
}

fCpuPlan(){
	fEcho_Clean "Cores ...............: ${BUILD_JOBS} of ${cpuCores}, ${HEAVY_JOBS} for the hard steps (load ${cpuLoad:-unknown}), at low priority"
	if [[ "${CICD_MEM_SCOPED:-none}" != none ]]; then fEcho_Clean "Memory cap ..........: ${CICD_MEM_SCOPED^^}, no swap (CICD_MEM_CAP; a step over it exits 137)"
	elif [[ "${memCap}" == none ]]; then fEcho_Clean "Memory cap ..........: none (CICD_MEM_CAP=none)"
	else fEcho_Clean "Memory cap ..........: none (no systemd user scope here)"; fi
}

#•••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••
fEcho_Clean
case "${mode}" in
	gate)  fEcho_Clean "${APP_NAME} backlog quick run${gateAreas[*]:+: ${gateAreas[*]}}" ;;
	full)  fEcho_Clean "${APP_NAME} backlog full run: every check of the default run; nothing is synced, dogfooded, published or rewritten" ;;
	merge) fEcho_Clean "${APP_NAME} merge gate: every check of the default run; nothing is synced, dogfooded, published or rewritten" ;;
	*)     fEcho_Clean "${APP_NAME} local CI/CD (${mode} run)" ;;
esac
fEcho_Clean
fEcho_Clean "Repo root ...........: ${root}"
fEcho_Clean "Version .............: ${goVersion}"
fCpuPlan
if ((noRemote)); then fEcho_Clean "Other hosts .........: skipped by --no-remote"
elif [[ " ${stepList[*]} " != *" arm64 "* ]]; then fEcho_Clean "Other hosts .........: none in this run"
else fEcho_Clean "Other hosts .........: ${ARM64_HOST} (linux/arm64 tests), through ${VM_STATE_TOOL}"; fi
if [[ "${leakOpt}" == --leakcheck ]]; then fEcho_Clean "Leak checks .........: every one (--leakcheck): race tests, soak, goroutine check"
elif [[ "${leakOpt}" == --no-leakcheck ]]; then fEcho_Clean "Leak checks .........: none (--no-leakcheck)"
else fEcho_Clean "Leak checks .........: goroutine check in every test run; race tests and soak where the run has them"; fi
fEcho_Clean "Steps ...............: ${stepList[*]}"
offNote=""
for s in "${allSteps[@]}"; do [[ -z "${stepOff[${s}]:-}" ]] || offNote+="${s} (${stepOff[${s}]}), "; done
[[ -z "${offNote}" ]] || fEcho_Clean "Turned off ..........: ${offNote%, }"
fEcho_Clean

if ((listOnly)); then
	fEcho_Clean "$(bash "${here}/utility/test-id.bash" --list | grep -c .) test(s) with IDs:"
	bash "${here}/utility/test-id.bash" --list | sed 's/^/  /'
	fEcho_Clean
	exit 0
fi

if ((! quiet && ! handsOff)) && [[ " ${stepList[*]} " == *" publish "* && -z "${publishMsg}" ]]; then
	read -r -p "Commit message for the publish stage (blank = a stamped default; Ctrl+C aborts): " typedMessage
	fEcho_ResetBlankCounter
	publishMsg="${typedMessage}"
fi

## Tee the rest of the run to a gitignored log, for the lint check at session start.
if [[ -n "${LINT_LOG_DIR:-}" ]] && mkdir -p "${root}/${LINT_LOG_DIR}" 2>/dev/null; then
	gfs_rotate "${root}/${LINT_LOG_DIR}" run log >/dev/null 2>&1 || true
	gfs_rotate "${root}/${LINT_LOG_DIR}" lint txt >/dev/null 2>&1 || true
	exec > >(tee "${root}/${LINT_LOG_DIR}/run_${stamp}.log") 2>&1
	## Wait for tee to drain, else the prompt comes back mid-flush.
	teePid=$!
	trap 'fRunTmpDone "$?"; exec 1>&- 2>&-; wait "${teePid}" 2>/dev/null' EXIT
fi

needCtr=0; for s in lint test race fuzz soak vuln codeql build package arm64 demo; do [[ " ${stepList[*]} " != *" ${s} "* ]] || needCtr=1; done
((! needCtr)) || fCtrSetup

for s in "${stepList[@]}"; do
	fStep "${s}"
	case "${s}" in
		sync)     fStepSync ;;
		format)   fStepFormat ;;
		lint)     fStepLint ;;
		test)     fStepTest ;;
		race)     fStepRace ;;
		fuzz)     fStepFuzz ;;
		soak)     fStepSoak ;;
		vuln)     fStepVuln ;;
		codeql)   fStepCodeql ;;
		build)    fStepBuild ;;
		package)  fStepPackage ;;
		arm64)    fStepArm64 ;;
		roothost) fRootHost ;;
		rootzfs)  fRootHost --zfs ;;
		demo)     fStepDemo ;;
		dogfood)  fStepDogfood ;;
		publish)  fStepPublish ;;
	esac
	fStepEnd
done

fSection "Summary"
for s in "${stepList[@]}"; do fEcho_Clean "  ${stepResult[${s}]:-?}  ${stepTitle[${s}]}"; done
for s in "${allSteps[@]}"; do [[ -z "${stepOff[${s}]:-}" ]] || fEcho_Clean "  off   ${stepTitle[${s}]} (${stepOff[${s}]})"; done
fSection "${APP_NAME} CI/CD: done."
fEcho_Clean


##	History:
##		- 2026-11-12: Created, from the sister projects' pipelines, with the container, the shared flags, the step lines, and the rest of the CICD process rules.
##		- 2026-11-13: arm64 step: the linux/arm64 tests run on the arm64 test box, under the host lock.
##		- 2026-11-13: fuzz step: each Fuzz target for a budget, from a copy of the tree.
##		- 2026-11-13: shellcheck and markdownlint run in the image, at pinned versions.
##		- 2026-11-13: soak step: the main actions over and over, heap and goroutines at round N against 2N.
##		- 2026-11-13: CodeQL step: the pinned Go bundle over the tracked Go files, cached outside the repo.
##		- 2026-11-13: demo step: the README's CLI demo gif, kept GFS-rotated, and committed only at a release.
