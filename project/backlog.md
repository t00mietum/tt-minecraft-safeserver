<!-- markdownlint-disable MD007 -- Unordered list indentation -->
<!-- markdownlint-disable MD010 -- No hard tabs -->
<!-- markdownlint-disable MD041 -- First line in a file should be a top-level heading -->

<!-- TOC ignore:true -->
# Project backlog

<!-- TOC ignore:true -->
## Table of contents

<!-- TOC -->

- [Introduction](#introduction)
- [Issues](#issues)
- [Template](#template)
	- [New format](#new-format)

<!-- /TOC -->

## Introduction

This is a product backlog just for pre-v1.0.0 release. After that, bugs, features, and enhancements will be managed in GitHub issues.

## Issues

- Live server has the whitelist off while it's open to the internet
	- ID: 2026111111065671
	- Type: Bug
	- Status: Waiting for answers
	- Severity: Critical
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Incorrect behavior [Bug]:
		- `white-list` and `enforce-whitelist` are both off, and players already join from the internet. Anyone with a Java account who finds the address can join.
	- Expected behavior [Bug]:
		- Only whitelisted players can join, Java and Bedrock both.
	- Possible cause [Bug]:
		- The old scripts never set it, and nothing checks it.
	- Note:
		- Before it's turned on, every current player has to be on the whitelist, Bedrock players included, or they get locked out.
		- The server tree is outside this repo, so the edit needs an OK or gets done by hand. `mcssctl` will force it on from then on.
	- Progress log:
		- 2026-11-11: Waiting for the list of Java usernames and Bedrock gamertags.

- Config reader for shcl in Go
	- ID: 2026111111065684
	- Type: Task
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Requirements:
		- Read `config.shcl` and `plugins.shcl` from Go.
	- Decisions:
		- Config is shcl. The tool reads it with the latest published release of shcl's Go module, beta or not.

- Config defaults and overrides
	- ID: 2026111111065697
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065684
	- Requirements:
		- Default configuration hard-coded.
		- Overridden by the config file.
		- Resistant to errors, so one bad line doesn't sink the whole thing.
		- Overridden by program options at run-time.

- Core commands
	- ID: 2026111111065711
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065697
	- Requirements:
		- `start`, `stop`, `status`, and `watch`, per `design.md`, with the same options as `tt-monerodctl`.
		- Docker mode is the default, and a sandboxed systemd unit is the fallback.
		- `--dry-run` and `--debug`.
		- `--help`, `--about`, and `--version`.
		- Hand the trees to the service account on start, and back on stop.
		- `stop` sends the `stop` console command first, with an optional countdown for the players.
		- Refuse to start when an old copy of the server is running under any account.

- Safe server settings
	- ID: 2026111111065724
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- `start` checks the settings listed in `design.md` and refuses when one is wrong. `repair` fixes them.
		- Online mode and the whitelist on. RCON, query, and the management server off. Proxy settings off.
		- Warn about an empty whitelist, and about more than 2 ops.

- Plugin pinning
	- ID: 2026111111065737
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Keep the approved jars and their hashes in `/etc/mcssctl/plugins.shcl`.
		- `start` refuses when a jar is new or changed.
		- `repair --approve-plugins` shows each one and asks.
		- The server can't write to the jars while it runs.

- console and cmd
	- ID: 2026111111065750
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- `console` attaches to the server console, with detach keys that can't stop the server by accident.
		- `cmd` sends one console command and prints what the server says back.
		- Works in Docker and native modes. No RCON.

- repair
	- ID: 2026111111065763
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065724, 2026111111065737
	- Requirements:
		- Run the checks listed in `design.md`, each with an optional fix.
		- `--check` only reports.

- setup
	- ID: 2026111111065776
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065763
	- Requirements:
		- Install from scratch, per `design.md`.
		- `--adopt` takes over an existing server folder as it is.
		- Ask before accepting the Minecraft EULA.
		- Install and set up Geyser and Floodgate when `bedrock` is on.
		- On an existing install, run `repair` and then `update`.

- Firewall
	- ID: 2026111111065789
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Keep the tool's rules in its own nftables table, loaded by its own systemd unit, per `design.md`.
		- Limit the game ports to `allowFrom`, with a rate limit per address.
		- Drop the server's new outbound connections to loopback and the LAN. Allow DNS, and the web unless `egress` is `open`.
		- `start` refuses to run when the table isn't loaded.
		- `setup` and `repair` manage it. They never change ufw or firewalld rules.

- update
	- ID: 2026111111065803
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Update the tool from GitHub releases.
		- Update Paper from the PaperMC API, within the same Minecraft version unless asked.
		- Update plugins that have a known source, and approve the new jars.
		- Refresh the pinned container image.

- Start at boot
	- ID: 2026111111065816
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065763
	- Requirements:
		- Add `startAtBoot` to the config, and a systemd unit that runs `start --foreground`, per `design.md`.
		- `start` and `stop` go through `systemctl` when the unit is enabled.
		- `setup` and `repair` install or remove the unit to match the config.

- Bedrock LAN discovery
	- ID: 2026111111065829
	- Type: Task
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Check that phones and consoles on the LAN see the server under LAN Games, with Geyser listening on all addresses.
		- Fix `how-to-connect.md` if they don't.

- NixOS module
	- ID: 2026111111065855
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065776
	- Requirements:
		- Add a flake with a NixOS module for the account, Docker, the config file, the firewall unit, and the boot unit.
		- On NixOS, `setup`, `repair`, and `update` check the system parts and print module settings, rather than change them.

- CI/CD pipeline
	- ID: 2026111111065868
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Requirements:
		- A CI/CD pipeline kicked off by a bash script (`cicd/cicd.bash`). It builds, tests, and can commit and push. Packaging and publishing are opt-in.

- Release-install script
	- ID: 2026111111070761
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065868
	- Requirements:
		- Runnable via a single `curl` or `wget`, and documented under "how to install".
		- Downloads, installs, and runs the latest release, with an option to abort.
	- Target OS: Debian, NixOS
	- Decisions:
		- It's Bash only, since both targets are Linux. It installs the binary, and `setup` does the rest.

- First release
	- ID: 2026111111070774
	- Type: Task
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065868, 2026111111070761
	- Requirements:
		- Publish `v1.0.0-alpha.1` on GitHub, with Linux builds for amd64 and arm64 and a checksum file that `update --self` can read.

- Move the first server over
	- ID: 2026111111070787
	- Type: Task
	- Status: Queued
	- Priority: Avg
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065776
	- Requirements:
		- Move the existing server to the new account and tool with `setup --adopt`.
		- Approve the plugins that are there now.
		- Retire the old start, stop, and manage scripts after it runs clean.

- Dev-environment install script
	- ID: 2026111111070800
	- Type: Feature
	- Status: Queued
	- Priority: Low
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065868
	- Requirements:
		- Runnable via a single `curl` or `wget`, and documented under "how to develop".
		- Clones main, installs dependencies, and states what it will do, with an option to abort.
	- Target OS: Debian, NixOS
	- Decisions:
		- It's Bash only, since both targets are Linux.

- Consoles over the internet
	- ID: 2026111111065842
	- Type: Feature
	- Status: Deferred
	- Priority: Low
	- Opened: 2026-11-11
	- Opened by: t00mietum
	- Requirements:
		- Let any Xbox, PlayStation, or Switch player join from outside the home, without changing settings on the console.
	- Decisions:
		- MCXboxBroadcast, when this gets picked up. It needs a spare Microsoft account, never the main one.
		- BedrockConnect and a LAN relay app were weighed, per `design.md`. A console that already uses BedrockConnect can join from anywhere in the meantime.

## Template

### New format

- Notes:

	- Only use rows that you actually need or expect will be filled in. Always fill in the title, ID, Type, Status, Opened and Created by.

	- The ID is the local time to the hundredth of a second. Opened is when it was written down, which may differ. (Use a keyboard macro and possibly something like project 'zuid' to generate.)

	- Status values meaning: Testing means the fix is in and checks are running or still to run. Waiting on signoff means automated testing passed. Moot means something else changed that made it irrelevant. Canceled means it still applies but was decided against. Waiting for testing means the fix is in and waits on a long CI run or an outside test host. Can't reproduce means a real attempt to reproduce it failed.

	- As issues are worked, and statuses change, place them in correct sorting order within the list:
		- First by status: Waiting for answers, Waiting on signoff, Testing, Waiting for testing, Can't reproduce, Stalled, Started, Queued, Done, Deferred, Canceled, Moot
		- Then by severity|priority: Critical, High, Avg, Low
		- Then by type: Bugs, [not bugs together]

	- Rows marked [Bug] are for bugs only, and rows marked [Feature] for features and enhancements. Priority and Severity share one row and one scale. Priority is for a Feature or Enhancement, and Severity for a Bug. Children are not nested. They sit at the top level and point back with Parent ID.

Template:

- Title
	- ID: YYYYmmDDHHMMSSNN
	- Type: [Bug|Feature|Enhancement|Task]
	- Status: [Queued|Waiting for answers|Waiting on signoff|Waiting for testing|Started|Testing|Stalled|Can't reproduce|Moot|Canceled|Deferred|Done]
	- Needs local test suite run?:
	- Needs external testing:
	- Priority [Feature|Enhancement] | Severity [Bug]: [Critical|High|Avg|Low]
	- Opened:
	- Opened by:
	- Assigned to:
	- Parent ID:
	- Prereq IDs:
	- Related IDs:
	- Target OS:
	- Test environment:
	- Version and build:
	- Requirements  [Feature]:
		- Hierarchical bulleted list.
	- Steps to reproduce [Bug]:
		- …
	- Incorrect behavior [Bug]:
	- Expected behavior [Bug]:
	- Reproduced [Bug]: [No, or when, where and how]
	- Possible cause [Bug]:
	- Actual cause [Bug]:
		- …
	- Estimated effort: [High|Avg|Low]
	- Actual effort: [High|Avg|Low]
	- Progress log:
		- …
	- Decisions:
		- …
	- Actual fix [Bug]:
	- Branch:
	- Commit:
	- Test case: [Reason not applicable, or CI test case #]
	- Acceptance signoff:
	- Superseded by ID:
	- Closed:
