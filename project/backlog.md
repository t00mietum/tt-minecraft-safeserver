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

<!-- /TOC -->

## Introduction

This is a product backlog just for pre-v1.0.0 release. After that, bugs, features, and enhancements will be managed in GitHub issues.

## Issues

- ZFS snapshots on start and stop
	- ID: 2026111112264283
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-122642
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Related IDs: 2026111111065711
	- Requirements:
		- With `zfsDataset` set, `start` takes a snapshot before the hand-off, and `stop` takes one after the worlds are saved, per `design.md`.
		- Prune old ones on the schedule there, touching only the tool's own.

- Live server has the whitelist off while it's open to the internet
	- ID: 2026111111065671
	- Type: Bug
	- Status: Waiting for answers
	- Severity: Critical
	- Opened: 20261111-110656
	- Opened by: t00mietum
	- Incorrect behavior [Bug]:
		- `white-list` and `enforce-whitelist` are both off, and players already join from the internet. Anyone with a Java account who finds the address can join.
	- Expected behavior [Bug]:
		- Only whitelisted players can join, Java and Bedrock both.
	- Possible cause [Bug]:
		- The old scripts never set it, and nothing checks it.
	- Note:
		- Before it's turned on, every current player has to be on the whitelist, Bedrock players included, or they get locked out.
		- The server tree is outside this repo, so the edit needs an OK or gets done by hand. `tt-minecraftd` will force it on from then on.
	- Progress log:
		- 2026-11-11: Waiting for the list of Java usernames and Bedrock gamertags.

- Config reader for shcl in Go
	- ID: 2026111111065684
	- Type: Task
	- Status: Done
	- Priority: High
	- Opened: 20261111-110656
	- Opened by: t00mietum
	- Requirements:
		- Read `config.shcl` and `plugins.shcl` from Go.
	- Actual effort: Avg
	- Decisions:
		- Config is shcl. The tool reads it with the latest published release of shcl's Go module, beta or not.
		- shcl `v3.0.0-beta1`, the only v3 release. `go.mod` says Go 1.24, so Debian 13's Go builds it.
		- The reader lays the file over a `Config` the caller fills first, so the defaults item only has to fill it. Empty fields are left for that item to derive, such as `runGroup` from `runUser`.
		- A bad value, an unknown key, or a key set twice with different values gets a warning with its line, and the value before it stands. A list with one bad item is dropped whole. Only a file that can't be read is an error.
		- Values are checked here: paths for `chown -R` must be full and not `/`, names can't start with `-`, ports and `nice` stay in range, and `allowFrom` takes `lan`, `any`, addresses, or ranges.
		- Sizes take Java's `4G` and shcl's `4 GiB`, since shcl alone refuses `4G`.
		- `startAtBoot` is read here too, so every key in design.md's table is read. A test checks the two match.
		- In `plugins.shcl`, a jar is named by its path under `serverDir`. A bad entry is left out, so its jar isn't approved. shcl merges entries with the same path, so a jar listed with 2 hashes is left out too.
		- shcl's hints are dropped from the warnings. They repeat ours, and "did you mean a list" misleads for a key that takes one value.
		- Swept: every key in the table has a setter and a test line; both files go through the one `LoadFile`.
	- Branch: r1111
	- Test case: df5wt, df5wv, df5ww, df5wx, df5wy, df5wz, df5x0, df5x1, df5x2, df5x3, df5x4, df5x5
	- Closed: 20261111-120826

- Config defaults and overrides
	- ID: 2026111111065697
	- Type: Feature
	- Status: Done
	- Priority: High
	- Opened: 20261111-110656
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065684
	- Requirements:
		- Default configuration hard-coded.
		- Overridden by the config file.
		- Resistant to errors, so one bad line doesn't sink the whole thing.
		- Overridden by program options at run-time.
	- Actual effort: Avg
	- Decisions:
		- Settings are built in order: the defaults, the file, the command-line options, then the keys that come from others, such as `runGroup` from `runUser`. Deriving last means an option that changes `runUser` moves `runGroup` too.
		- Options are checked by the same code as the file. A bad one is an error, not a warning, since it was typed just now.
		- design.md now has a common `--set KEY=VALUE`, with `--native` as a short form for `runMode`. The flags themselves are parsed in the Core commands item, which builds the CLI.
		- `heapMin` defaults to 1G, or `heapMax` when that's less. A `heapMin` over `heapMax`, or a `memCap` not over it, gets a warning and a safe value.
		- `ownerAfterStop` comes from `SUDO_USER`, then `USER`. It's left empty when neither names an account, as from the boot unit, and Core commands decides what `stop` does then.
		- The default image is `eclipse-temurin:25-jre`, pinned to its multi-arch digest. `jvmArgs` defaults to Paper's recommended flags.
		- An option value follows file syntax, so `##` starts a comment there too.
		- Swept: every default in design.md's table that's written as a value is checked against the code by a test. The derived ones have their own cases.
	- Branch: r1111
	- Test case: df5x6, df5x7, df5x8, df5x9, df5xa, df5xb
	- Closed: 20261111-121403

- Core commands
	- ID: 2026111111065711
	- Type: Feature
	- Status: Waiting on signoff
	- Priority: High
	- Opened: 20261111-110657
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065697
	- Requirements:
		- Build `start`, `stop`, `status`, and `watch`, per `design.md`, with the same options as `tt-monerod-safeserver`.
		- Docker mode is the default, and a sandboxed systemd unit is the fallback.
		- Add `--dry-run` and `--debug`.
		- Add `--help`, `--about`, and `--version`.
		- Hand the trees to the service account on start, and back on stop.
		- `stop` sends the `stop` console command first, with an optional countdown for the players.
		- Refuse to start when an old copy of the server is running under any account.
	- From 2026111111065697: parse `--set KEY=VALUE` and `--native` into `config.Option` and pass them to `config.Load`. Settle what `stop` does when `ownerAfterStop` is empty, as it can be from the boot unit, and whether a missing default config file is an error. `Load` returns the read error for now.
	- Split into 4 children: 2026111112162268 (CLI, runner, tree hand-off) is done, 2026111112163174 (Docker mode) waits on a hand test, and 2026111112163284 (native mode) waits on a hand test, and 2026111112163393 (status and watch) waits on a hand test.
	- Test case: its children's: 2026111112162268: df5xc, df5xd, df5xe, df5xf, df5xg, df5xh, df5xj, df5xk, df5xm, df5xn; 2026111112163174: df5xp, df5xq, df5xr, df5xs, df5xt, df5xv, df5xw, df5xy, df5xz; 2026111112163284: df5y0, df5y1, df5y2, df5y3, df5y4, df5y5, df5y6; 2026111112163393: df5y7, df5y8, df5y9, df5ya, df5yb, df5yc, df5yd, df5ye, df5yf, df5yg, df5yh
	- 20261111: children 2026111112163174 2026111112163284 2026111112163393 wait on UAT, and the rest are closed

- Core commands: CLI, command runner, and tree hand-off
	- ID: 2026111112162268
	- Type: Feature
	- Status: Done
	- Priority: High
	- Opened: 20261111-121622
	- Opened by: t00mietum
	- Parent ID: 2026111111065711
	- Requirements:
		- Parse the commands and the common options, `--set` and `--native` included, and load the config.
		- Add `--help`, `--about`, `--version`, `--dry-run`, and `--debug`.
		- One command runner, with sudo and dry run, and a fake for tests.
		- Hand the trees to the service account and back.
	- Actual effort: Avg
	- Decisions:
		- Common options work before or after the command. `--native` and `--set` reach the config in the order given, so the last one wins.
		- A missing default config means the defaults, with a warning, so `status` works before `setup`. A missing `--config` file is an error, exit 1. A bad option is exit 2.
		- `--dry-run` still runs commands that only look, such as `docker ps`, so the plan it prints matches a real run. design.md updated.
		- With `ownerAfterStop` empty, as from the boot unit, `stop` leaves the trees with `runUser` and warns. The operator's next `stop` hands them back, per design.md's `stop` step 1.
		- A tree given as a link is followed before `chown -R`, and the depth check runs on both the link and its target.
		- `--help` lists only the built commands. `start`, `stop`, `status`, and `watch` parse and load the config, then say they aren't built yet, until the next 3 children.
		- Swept: both flag passes share `commonFlags`; every chown goes through `tree.handOver`.
	- Branch: r1111
	- Test case: df5xc, df5xd, df5xe, df5xf, df5xg, df5xh, df5xj, df5xk, df5xm, df5xn
	- Closed: 20261111-122043

- Core commands: Docker mode start and stop
	- ID: 2026111112163174
	- Type: Feature
	- Status: Waiting on signoff
		- Waits on the hand test below. The console takes `stop` in a plain terminal; Paper's own console is still to see.
	- Needs hand testing: start and stop a throwaway Paper server in Docker mode, with the EULA accepted by hand. It loads, `stop --warn 10` counts down and saves, and the jars can't be written.
	- Priority: High
	- Opened: 20261111-121631
	- Opened by: t00mietum
	- Parent ID: 2026111111065711
	- Prereq IDs: 2026111112162268
	- Requirements:
		- `start` and `stop` in Docker mode, per `design.md`, through the runner, with `--debug`, `--foreground`, and `--warn N`.
		- Refuse to start when a Paper server is already running, under any account.
		- `stop` sends the `stop` console command first, and hands the trees back even after a crash.
		- Test with the fake runner and `--dry-run` only.
	- Actual effort: Avg
	- Progress log:
		- Verified: the same `docker run` flags on a small image give a writable `/tmp`, read-only jars and image, and umask 0007. The console's `stop` reached a real container's terminal.
	- Decisions:
		- `stop` writes to the container's input through the Docker API, as `cmd` will, since `docker attach` refuses input that isn't a terminal. A line ends with a carriage return, which a cooked terminal takes, and a raw one such as Paper's should too.
		- The countdown says the time left at the start, then at 60, 30, 10, and 5 to 1 seconds.
		- "Already running" comes from `ps`: any `java -jar paper*.jar`, under any account. That covers the old scripts and this tool's own container.
		- The server runs under `sh -c 'umask 0007 && exec "$@"'` in the image, since Docker has no umask option. The pid limit is 2048, for Paper's thread pools.
		- `--debug` runs attached and hands the trees back when the server exits. `--foreground` runs detached, so `console` still works, then waits with `docker wait`.
		- A start that times out leaves the server running, with an error that says so.
		- The safe settings, plugin, and firewall steps of `start` come with their own items. ZFS snapshots had none, so 2026111112264283 is filed.
		- Swept: every container command uses `launch.Container`; `start` and `stop` both hand back through `tree.ToOperator`.
	- Branch: r1111
	- Test case: df5xp, df5xq, df5xr, df5xs, df5xt, df5xv, df5xw, df5xy, df5xz

- Core commands: native mode start and stop
	- ID: 2026111112163284
	- Type: Feature
	- Status: Waiting on signoff
		- Waits on the hand test below. No real unit was started here.
	- Needs hand testing: start and stop a throwaway Paper server in native mode, with the EULA accepted by hand. It loads, `stop --warn 10` counts down and saves, the jars can't be written or swapped, and `systemctl show tt-minecraftd-server` lists the sandbox settings.
	- Priority: High
	- Opened: 20261111-121632
	- Opened by: t00mietum
	- Parent ID: 2026111111065711
	- Prereq IDs: 2026111112163174
	- Requirements:
		- `start` and `stop` in native mode: `systemd-run` with the sandbox options, and tmux on a socket under `/run/tt-minecraftd/`, per `design.md`.
		- `paper.jar` and the plugin jars go to root at start.
	- Actual effort: Avg
	- Progress log:
		- Verified: tmux `send-keys -l` drops a `;` that ends the text, and `-H 3b` sends it whole. The unit's settings pass `systemd-analyze verify`.
	- Decisions:
		- The unit is `tt-minecraftd-server.service`, since `tt-minecraftd.service` is the boot unit. It's `Type=forking`, since `tmux new-session -d` returns once the session is up. `--collect` drops it after a crash, so the next start can reuse the name.
		- The jars also get `ReadOnlyPaths`, since the server can write their folder and could swap a root-owned jar. `OOMScoreAdjust=1000`, a swap of 0, and the pid limit match Docker mode. design.md updated.
		- `$` in the server command goes as `$$`, since systemd expands variables there.
		- "Is it up" is the unit's `ActiveState`. "Done loading" is a `logs/latest.log` that isn't the one from before the launch.
		- `stop` past `stopTimeout` runs `systemctl stop`, with `TimeoutStopSec` set to the 30 seconds before SIGKILL.
		- `--debug` attaches to tmux. A detach leaves the server and its trees as they are; an exit hands them back. `--foreground` checks the unit every 5 seconds.
		- Console lines go through `tmux send-keys` as the service account, with `sudo -u`, or `runuser` when root. `host.Cmd` has an `As` field for it, which `console` and `cmd` can use too.
		- Docker and native share the start and stop steps through `launch.Kit`. Docker's tests pass unchanged.
		- 20261111: `TestMissingConfig` (df5xn) no longer expects `start --native --dry-run` to say it isn't built. `status` still does.
		- Swept: every sudo goes through `host.Cmd`; both modes refuse others, count down, and hand back through `Kit`.
	- Branch: r1111
	- Test case: df5y0, df5y1, df5y2, df5y3, df5y4, df5y5, df5y6

- Core commands: status and watch
	- ID: 2026111112163393
	- Type: Feature
	- Status: Waiting on signoff
		- Waits on the hand test below. The ping was checked against a stand-in server only, not a real Paper.
	- Needs hand testing: run `status` and `watch` beside the throwaway Paper server from the Docker and native hand tests. Version, players, memory, ports, and the log tail show, and Ctrl-C ends `watch`.
	- Priority: High
	- Opened: 20261111-121633
	- Opened by: t00mietum
	- Parent ID: 2026111111065711
	- Prereq IDs: 2026111112163174
	- Requirements:
		- `status` and `watch`, per `design.md`. Parts that need a later item, such as the firewall counters, show as not set up yet.
	- Actual effort: Avg
	- Progress log:
		- Verified: against a throwaway container named for the tool, `status` showed it running, its account, its time up, and its memory from the cgroup, then exited once stopped.
	- Decisions:
		- Players and the version come from a server list ping on `bindIP`, or 127.0.0.1 when it's all addresses. The Paper build comes from the jar `paper.jar` links to, so it shows when stopped too.
		- Memory is the cgroup's `memory.current`: the container's, found through its pid, or the unit's `MemoryCurrent`. Both work without sudo.
		- Ports come from `ss`, which can't name another account's process without root, so it shows the address that listens and the TCP connection count.
		- The log is `logs/latest.log` in both modes, since `docker logs` of a terminal holds escape codes. Control characters are dropped from it and from op names.
		- The server owns the tree, so files that aren't plain files are not read, and a jar that's a pipe or a device counts as changed. design.md updated.
		- The plugin check is `plugins.CheckJars`, for `start`'s check to reuse. `watch` hashes a jar again only when its size or time changes.
		- The approved list is read from beside the config, so a second `--config` gets its own. design.md updated.
		- A part that can't be read says why, and the rest still shows. Exit 0, since `status` ran. A failed command's stderr goes into its line, not over the screen, through `host.Exec.QuietErr`.
		- No color yet. design.md allows it on a terminal only, and nothing needs it.
		- 20261111: `TestMissingConfig` (df5xn) no longer expects `status` to say it isn't built. It checks the config path `status` reads beside instead.
		- Swept: every file read from the tree goes through `openRegular` or `Sums.Sum`; both modes judge "running" through the same calls as `start`, `launch.ContainerState` and `launch.UnitUp`.
	- Branch: r1111
	- Test case: df5y7, df5y8, df5y9, df5ya, df5yb, df5yc, df5yd, df5ye, df5yf, df5yg, df5yh

- Safe server settings
	- ID: 2026111111065724
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-110657
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Requirements:
		- `start` checks the settings listed in `design.md` and refuses when one is wrong. `repair` fixes them.
		- Force online mode and the whitelist on. Force RCON, query, the management server, and the proxy settings off.
		- Warn about an empty whitelist, and about more than 2 ops.
	- Prereq moved from the Core commands parent to its Docker mode child, which is all this needs to start.
	- Split: the Geyser rows are in child 2026111112554261, which is Queued. The rest is done.
	- Actual effort: Avg
	- Progress log:
		- 2026-11-11: `start` checks `server.properties`, `spigot.yml`, and `paper-global.yml` in both modes, and refuses with the setting named. `repair` sets them, and `repair --check` only reports. The whitelist and ops warnings show at `start` and `repair`.
		- Verified: each new test fails with its check broken, and passes with it in place.
	- Decisions:
		- `proxies.proxy-protocol` is forced off too, as a proxy setting. design.md updated, with a note under Needs more thought for tunnels.
		- A missing `server.properties` is unsafe, since Minecraft's defaults have the whitelist off. `repair` writes one. A missing YAML file or key is fine, since each defaults to off.
		- Booleans compare without case, as Java reads them. A fix changes only the wrong lines, and keeps comments, order, owner, and mode.
		- Files are opened without following links, and a fix writes a new file and renames it into place, since the server account can write the tree and `repair` runs as root.
		- `repair` is only the safe settings check so far. The repair item adds the rest. It refuses without root unless `--check` is given.
		- `status` now reads the whitelist and ops through the same code.
		- The empty whitelist warning names `tt-minecraftd cmd whitelist add`, per design.md, though `cmd` comes with its own item.
		- Swept: both modes call the check through `Kit`; `status` and the warnings share one player reader.
	- Branch: r1111
	- Test case: df5yj, df5yk, df5ym, df5yn, df5yp, df5yq, df5yr, df5ys

- Safe server settings: Geyser config
	- ID: 2026111112554261
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-125542
	- Opened by: t00mietum
	- Parent ID: 2026111111065724
	- Requirements:
		- With `bedrock` on, `start` checks Geyser's `config.yml` and `repair` sets it: `bedrock.address` and `bedrock.port` from `bindIP` and `bedrockPort`, and `auth-type` set to `floodgate`, per `design.md`.
		- Check the key names against the Geyser build in use, since its config layout has changed between versions. `auth-type` has sat at the top level, under `remote:`, and under `java:`.
		- Use the YAML and file code in `source/internal/settings`.

- Plugin pinning
	- ID: 2026111111065737
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-110657
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Requirements:
		- Keep the approved jars and their hashes in `/etc/tt-minecraftd/plugins.shcl`.
		- `start` refuses when a jar is new or changed.
		- `repair --approve-plugins` shows each one and asks.
		- The server can't write to the jars while it runs.
	- Prereq moved from the Core commands parent to its Docker mode child, which is all this needs to start.

- console and cmd
	- ID: 2026111111065750
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-110657
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Requirements:
		- `console` attaches to the server console, with detach keys that can't stop the server by accident.
		- `cmd` sends one console command and prints what the server says back.
		- Make both work in Docker and native modes, without RCON.
	- Prereq moved from the Core commands parent to its Docker mode child, which is all this needs to start.

- repair
	- ID: 2026111111065763
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-110657
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
	- Opened: 20261111-110657
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
	- Opened: 20261111-110657
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Requirements:
		- Keep the tool's rules in its own nftables table, loaded by its own systemd unit, per `design.md`.
		- Limit the game ports to `allowFrom`, with a rate limit per address.
		- Drop the server's new outbound connections to loopback and the LAN. Allow DNS, and the web unless `egress` is `open`.
		- `start` refuses to run when the table isn't loaded.
		- `setup` and `repair` manage it. They never change ufw or firewalld rules.
	- Prereq moved from the Core commands parent to its Docker mode child, which is all this needs to start.

- update
	- ID: 2026111111065803
	- Type: Feature
	- Status: Queued
	- Priority: High
	- Opened: 20261111-110658
	- Opened by: t00mietum
	- Prereq IDs: 2026111112163174
	- Requirements:
		- Update the tool from GitHub releases.
		- Update Paper from the PaperMC API, within the same Minecraft version unless asked.
		- Update plugins that have a known source, and approve the new jars.
		- Refresh the pinned container image.
	- Prereq moved from the Core commands parent to its Docker mode child, which is all this needs to start.

- Start at boot
	- ID: 2026111111065816
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-110658
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
	- Opened: 20261111-110658
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Check that phones and consoles on the LAN see the server under LAN Games, with Geyser listening on all addresses.
		- Fix `how-to-connect.md` if they don't.

- Move shared code to tt-safeserver
	- ID: 2026111111303242
	- Type: Task
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-113032
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065711
	- Requirements:
		- Move the parts that `tt-monerod-safeserver` needs too into a new Go module and repo, `tt-safeserver`, per `design.md`.
		- Do it when `tt-monerod-safeserver` starts on code, so 2 tools test the interface.
		- Create the GitHub repo first, plus the same working folder layout this project uses, with `repo/`, `private/`, and the doc links.
		- Its backlog and docs get kept up from here, with the same style passes. Not while `tt-monerod-safeserver` is also being worked on.
	- Decisions:
		- One library module, with one binary per tool. A single binary for every service and a generic engine were weighed and left out.
		- GPL v2 or later.

- NixOS module
	- ID: 2026111111065855
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-110658
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
	- Opened: 20261111-110658
	- Opened by: t00mietum
	- Requirements:
		- A CI/CD pipeline kicked off by a bash script (`cicd/cicd.bash`). It builds, tests, and can commit and push. Packaging and publishing are opt-in.

- Release-install script
	- ID: 2026111111070761
	- Type: Feature
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-110707
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
	- Opened: 20261111-110707
	- Opened by: t00mietum
	- Prereq IDs: 2026111111065868, 2026111111070761
	- Requirements:
		- Publish `v1.0.0-alpha.1` on GitHub, with Linux builds for amd64 and arm64 and a checksum file that `update --self` can read.

- Move the first server over
	- ID: 2026111111070787
	- Type: Task
	- Status: Queued
	- Priority: Avg
	- Opened: 20261111-110707
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
	- Opened: 20261111-110708
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
	- Opened: 20261111-110658
	- Opened by: t00mietum
	- Requirements:
		- Let any Xbox, PlayStation, or Switch player join from outside the home, without changing settings on the console.
	- Decisions:
		- MCXboxBroadcast, when this gets picked up. It needs a spare Microsoft account, never the main one.
		- BedrockConnect and a LAN relay app were weighed, per `design.md`. A console that already uses BedrockConnect can join from anywhere in the meantime.

## Template

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
		- ...
	- Incorrect behavior [Bug]:
	- Expected behavior [Bug]:
	- Reproduced [Bug]: [No, or when, where and how]
	- Possible cause [Bug]:
	- Actual cause [Bug]:
		- ...
	- Estimated effort: [High|Avg|Low]
	- Actual effort: [High|Avg|Low]
	- Progress log:
		- ...
	- Decisions:
		- ...
	- Actual fix [Bug]:
	- Branch:
	- Commit:
	- Test case: [Reason not applicable, or CI test case #]
	- Acceptance signoff:
	- Superseded by ID:
	- Closed:
