<!-- markdownlint-disable MD007 -- Unordered list indentation -->
<!-- markdownlint-disable MD010 -- No hard tabs -->
<!-- markdownlint-disable MD055 -- Table pipe style [Expected: leading_and_trailing; Actual: leading_only; Missing trailing pipe] -->
<!-- markdownlint-disable MD041 -- First line in a file should be a top-level heading -->

<!-- TOC ignore:true -->
# Design

Design, requirements, and direction. The active pre-v1.0.0 bug/feature task list lives in `backlog.md`.

- Status: draft, 2026-11-11.

- Author: t00mietum.

<!-- TOC ignore:true -->
## Table of contents

<!-- TOC -->

- [Summary](#summary)
- [Background](#background)
- [Goals and non-goals](#goals-and-non-goals)
- [Assumptions](#assumptions)
- [Commands](#commands)
	- [start](#start)
	- [stop](#stop)
	- [status and watch](#status-and-watch)
	- [console and cmd](#console-and-cmd)
	- [setup](#setup)
	- [update](#update)
	- [repair](#repair)
- [Project structure](#project-structure)
	- [Folder structure](#folder-structure)
	- [Logical code structure](#logical-code-structure)
	- [Data flow](#data-flow)
	- [Execution flow/loops](#execution-flowloops)
- [Direction decisions](#direction-decisions)
- [Plan](#plan)
- [Architecture](#architecture)
	- [Software stack](#software-stack)
	- [Configuration model](#configuration-model)
	- [Saves and persistence](#saves-and-persistence)
	- [Privilege model](#privilege-model)
	- [Why Docker](#why-docker)
	- [Run modes](#run-modes)
	- [Tree ownership](#tree-ownership)
	- [Safe server settings](#safe-server-settings)
	- [Plugin pinning](#plugin-pinning)
	- [Bedrock players](#bedrock-players)
	- [Exposure](#exposure)
	- [Firewall](#firewall)
	- [Boot](#boot)
	- [NixOS](#nixos)
	- [ZFS snapshots](#zfs-snapshots)
	- [UI](#ui)
	- [Security](#security)
	- [Testing](#testing)
- [Needs more thought](#needs-more-thought)

<!-- /TOC -->

## Summary

- `tt-minecraftd` runs a Paper Minecraft Java server on Linux as a dedicated, unprivileged account. It starts and stops the server, shows its status, sends it console commands, sets it up from scratch, updates it, and repairs a broken install.

- The server runs inside a locked-down Docker container by default. Without Docker it runs in a sandboxed systemd unit, under the same account.

- Before every start it puts the safe settings back in place, such as online mode and the whitelist, and refuses to start when a plugin jar has changed since it was approved.

- Bedrock players, on phones, tablets, and consoles, join through the Geyser and Floodgate plugins.

- Its own firewall table limits who can connect to the game ports, and stops the server from opening connections to the LAN or to local services.

- It runs on Debian and NixOS, and can start the server at boot through a systemd unit.

- How players connect is in [how-to-connect.md](../how-to-connect.md).

## Background

- The server used to run from a few Bash scripts. They worked, but had some problems:
	- The run account was also in `sudo` and `docker`, so a plugin that went bad had root a step away.
	- The whitelist was off, and the game port was open to the internet.
	- Plugin jars could change between runs, and the scripts wouldn't notice.
	- Commands were built as strings and run through `eval`.
	- Stopping meant matching `ps` output with a regex, then SIGTERM and SIGKILL after a fixed 8 seconds.

- A list of the common ways open Minecraft servers get attacked was put together first. [Security](#security) maps each one to what `tt-minecraftd` does about it.

## Goals and non-goals

- Goals:
	- Run the server as an account that isn't used for anything else, and doesn't have sudo.
	- Make the unsafe settings hard to leave on by accident.
	- Notice a changed or new plugin jar before it runs.
	- Let Java players on PCs, and Bedrock players on phones and consoles, connect from the LAN and from the internet.
	- Keep root work to the few steps that need it, and do it in the foreground.
	- Hand the server tree to the service account before a start, and back to the operator after a stop.
	- Use the same commands as `tt-monerod-safeserver`, so both tools work the same way.
	- Run on Debian first, then NixOS.
	- Start at boot.

- Non-goals:
	- Only Paper is supported. Forge, NeoForge, Fabric, and other modded servers aren't.
	- Bedrock Dedicated Server isn't supported. Bedrock players come in through Geyser instead.
	- A config runs one server. `--config` can point at a second one.
	- Proxy networks, such as Velocity or BungeeCord, aren't supported.
	- The tool doesn't harden the rest of the host, such as SSH, adn leaves that to the host's own setup.

## Assumptions

- The host runs Debian or NixOS, on amd64 or arm64. Both use systemd.

- On NixOS, the system parts are declared in a NixOS module, rather than changed by `setup`. See [NixOS](#nixos).

- The operator has sudo, and is in the `docker` group for Docker mode.

- Players connect through a router port forward, a tunnel, or the LAN. See [Exposure](#exposure).

## Commands

~~~text
tt-minecraftd start   [--native] [--debug] [--dry-run] [--foreground]
tt-minecraftd stop    [--warn N] [--dry-run]
tt-minecraftd status
tt-minecraftd watch   [--interval N]
tt-minecraftd console
tt-minecraftd cmd     <server command>
tt-minecraftd setup   [--yes] [--dry-run] [--adopt PATH] [--accept-eula] [setup options]
tt-minecraftd update  [--yes] [--dry-run] [--self] [--paper] [--plugins] [--image] [--mc-version V]
tt-minecraftd repair  [--yes] [--dry-run] [--check] [--approve-plugins]
tt-minecraftd --help | --about | --version
Common:  --config PATH, --set KEY=VALUE
~~~

- The commands match `tt-monerod-safeserver`. `console` and `cmd` are new, since a Minecraft server gets managed from its console.

- Exit codes are 0 for success, 1 for an error, and 2 for bad usage.

- `--set KEY=VALUE` overrides one config key for this run, such as `--set heapMax=6G`. It can be given more than once. See [Configuration model](#configuration-model).

- `--dry-run` prints every command that would change something, with the sudo ones marked, and doesn't run them. Commands that only look, such as `docker ps`, still run, so the plan matches what a real run would do.

- When the boot unit is enabled, `start` and `stop` run `systemctl start` and `systemctl stop` on it instead. That way systemd and the tool don't disagree about what's running.

### start

1. Load and check the config. Refuse when a Paper server is already running, under any account. That includes one the old scripts started.

2. Get sudo once, up front, with `sudo -v`.

3. Check the safe settings in `server.properties` and the other server files. Refuse when one is wrong, and name the setting and `repair` as the fix. See [Safe server settings](#safe-server-settings).

4. Check every jar in `plugins/` against the approved list. Refuse when a jar is new or changed. See [Plugin pinning](#plugin-pinning).

5. With `firewall` on, check through sudo that the tool's nftables table is loaded. Refuse to start without it.

6. Take a ZFS snapshot, when a dataset is configured, and prune old ones.

7. Hand the trees to the service account. See [Tree ownership](#tree-ownership).

8. Launch in the configured mode. Then wait until the log says the server is done loading, or `startTimeout` runs out. When the process died, print the end of its log and exit 1.

- `--debug` runs the server in the foreground with its console attached.

- `--foreground` is for the boot unit. It runs as root, skips sudo, and stays attached to the server until it exits, so systemd can track it.

### stop

1. When the server isn't running, make sure the trees have gone back to the operator, and exit 0. That way the operator still gets them back after a crash.

2. With `--warn N`, broadcast a countdown to the players for N seconds first.

3. Send the `stop` console command, so Paper saves the worlds and kicks the players with a message. Wait up to `stopTimeout` seconds. Send SIGTERM if it's still up, and SIGKILL after another 30 seconds.

4. Take a ZFS snapshot now that the worlds are closed.

5. Hand the trees back to the operator.

### status and watch

- `status` runs once, and doesn't need sudo. It shows:
	- Whether the server runs, in which mode, as which account, and for how long.
	- The Minecraft and Paper versions, and players online. Those come from a server list ping on localhost, so they don't need RCON or query.
	- Memory use against the cap.
	- What listens on the game ports, and how many connections each has.
	- The firewall drop counters. A rising outbound count means something in the server tried to connect where it shouldn't.
	- The whitelist size and the list of ops.
	- Whether the plugin jars match the approved list.
	- Who owns the trees, and whether the boot unit is enabled.
	- The last lines of the log, cut to the terminal width.

- `watch` redraws `status` every few seconds until Ctrl-C.

### console and cmd

- `console` attaches to the server console. It prints the detach keys first, since Ctrl-C there would stop the server.
	- Docker mode uses `docker attach` with `--sig-proxy=false` and its own detach keys.
	- Native mode attaches to the service account's tmux session.

- `cmd` sends one console command and prints the log lines that follow it, such as `tt-minecraftd cmd whitelist add Steve`.
	- Docker mode writes to the container's input with the Docker API.
	- Native mode uses `tmux send-keys`.

- RCON is never used. See [Safe server settings](#safe-server-settings).

### setup

- Needs root, so it runs itself again through sudo when needed. It states its plan and asks before it changes anything, unless `--yes` is given.

- On a new install it:
	1. Writes the config, from defaults and the setup options.
	2. Creates the service account and its group, and adds the operator to the group.
	3. Creates the server tree, or with `--adopt PATH` uses an existing server folder as it is.
	4. Downloads and checks Paper on a new tree, the same way `update` does.
	5. Asks the operator to accept the Minecraft EULA, and writes `eula.txt` only after a yes or `--accept-eula`.
	6. Writes the safe settings. See [Safe server settings](#safe-server-settings).
	7. With `bedrock` on, downloads Geyser and Floodgate and sets them up. See [Bedrock players](#bedrock-players).
	8. Approves the plugin jars that are there, after listing each one with its hash.
	9. With `firewall` on, installs nftables through apt when `nft` is missing, writes the rules, and starts their unit. See [Firewall](#firewall).
	10. Pulls the pinned container image in Docker mode.
	11. Installs and enables the boot unit when `startAtBoot` is on.
	12. Copies itself to `/usr/local/bin` when it runs from somewhere else.
	13. Ends with `repair --check`.

- When it finds an install already, it runs `repair` and then `update`.

- It never installs Docker. Without Docker, it offers native mode.

- On NixOS it skips steps 2, 9, 11, and 12. It checks those parts instead, and prints the module settings for whatever is missing.

### update

- Needs root, and asks first unless `--yes` is given. When a part isn't named, it updates all of them.

- `--self` gets the latest `tt-minecraft-safeserver` release from GitHub, checks its SHA-256 sum, and replaces the running binary in one rename.

- `--paper` asks the PaperMC download API for the newest stable build of the current Minecraft version. It downloads the jar, checks its SHA-256 sum against the API, puts it beside the old ones, and points `paper.jar` at it. The last 2 builds stay on disk.
	- `--mc-version V` moves to another Minecraft version. It needs a yes even with `--yes`, since worlds can't go back to an older version and plugins may not work on the new one. It takes a ZFS snapshot first.

- `--plugins` updates each plugin with a known source in the approved list, such as Geyser, Floodgate, Modrinth, or Hangar. It shows the old and new versions, checks the hash from the source, and approves the new jar.

- `--image` pulls the newest container base image, checks that `java -version` in it is new enough for this Paper, and pins its digest in the config.

- A running server is left alone. It uses the new files on its next start.

- On NixOS, `--self` only prints what to change, since it comes from the Nix store.

### repair

- Needs root unless `--check` is given. `--check` reports only.

- Each check prints OK, FIXED, or FAILED. The exit code is 1 when anything failed.

- `--approve-plugins` lists each new or changed jar with its hash, asks about each one, and updates the approved list.

- Checks, in order:
	1. The config parses, and only root can write it.
	2. The service account and group exist, the account can't log in, and it isn't in any other group.
	3. The operator is in the group.
	4. The server tree exists, and so does `paper.jar`.
	5. Ownership and modes match whether the server runs.
	6. The safe settings are in place.
	7. Every plugin jar is approved. This one is never fixed, only reported, unless `--approve-plugins` is given.
	8. With `bedrock` on, Geyser and Floodgate are installed and set up to match the config, and the Floodgate key can only be read by the account and group.
	9. With `firewall` on, the rules file matches the config, its unit is enabled, and the table is loaded. With it off, the unit and the table get removed.
	10. When ufw or firewalld is active, it lets players connect to the game ports. A missing rule is reported with the command that adds it, and never added.
	11. In Docker mode, the image is there, and an old stopped container gets removed.
	12. With a dataset configured, it exists and is mounted.
	13. The boot unit is installed and enabled only when `startAtBoot` is on.
	14. Any Paper server that this tool didn't start is reported, and never killed.

## Project structure

### Folder structure

~~~text
go.mod
source/cmd/tt-minecraftd/    main package, argument parsing
source/internal/config/      config load and checks
source/internal/host/        command runner, sudo, dry run
source/internal/server/      Java argument list, server list ping, log reading
source/internal/props/       server.properties and the YAML files, read and fix
source/internal/plugins/     jar hashes and the approved list
source/internal/launch/      Docker and native modes, console
source/internal/tree/        ownership hand-off
source/internal/zfs/         snapshots and pruning
source/internal/firewall/    nftables rules and their unit
source/internal/fetch/       downloads and hash checks
source/internal/update/
source/internal/repair/
source/internal/boot/        systemd unit
nix/                         NixOS module and flake
project/                     design, backlog, and notes
cicd/                        build, test, and release scripts
~~~

### Logical code structure

- Every outside command goes through one runner in `host`. The runner logs each command, adds sudo where a step asks for it, and prints instead of running under `--dry-run`. Tests replace it with a fake that records what would run.

- The Java arguments are built once, as a list. The launcher only decides how the trees are mounted.

- `repair` is a list of checks. Each one has a test function and an optional fix function. `setup` reuses the fix functions, and `start` runs the test functions for the safe settings and the plugins.

- The parts that `tt-monerod-safeserver` needs too go in their own packages, so that way they're behind one interface. They're the runner, config loading, the account, the ownership hand-off, ZFS, the launchers, the firewall unit, the boot unit, the `repair` checks, self-update, and the CLI. See [Direction decisions](#direction-decisions).

### Data flow

- Config file -> checked settings -> Java argument list -> launcher.

- PaperMC API -> build info and SHA-256 -> download -> hash check -> `paper.jar`.

- Plugin jars -> SHA-256 -> compare with the approved list -> start or refuse.

### Execution flow/loops

- Every command runs once and exits, except `watch` and `console`. `watch` loops until interrupted, and `console` runs until detached.

- `start` reads the log once a second while it waits for the server to finish loading.

- `stop` polls once a second while it waits for the server to exit.

## Direction decisions

- Go was chosen over Bash and PowerShell.
	- `setup`, `update`, and `repair` need HTTPS, JSON, YAML, hashes, and the Docker API. Bash would need curl, jq, and yq, and it gets hard to test past a few hundred lines.
	- PowerShell isn't installed on most Linux servers.
	- Go builds one static binary, and its standard library has nearly all of that. `tt-monerod-safeserver` is in Go too, so code can move between them.

- Docker mode is the default, and a sandboxed systemd unit is the fallback.
	- A plugin runs with the same access as the server. In a container it can only see the server tree. [Why Docker](#why-docker) has more.
	- Native mode uses systemd's sandbox options rather than a bare tmux session as in `tt-monerod-safeserver`. They give most of what the container does for little extra code.

- The container runs a stock Eclipse Temurin JRE image that's pinned by its digest.
	- Java needs more from the image than `monerod` does, so mounting the host's Java into a slim image wasn't an option.
	- A custom image would need its own build and update path.

- The container uses host networking.
	- Bedrock LAN discovery uses UDP broadcast, which doesn't cross Docker's bridge.
	- Ports published on Docker's bridge skip the host's input firewall rules, ufw's included. With host networking, the tool's own table and any ufw rules apply as written.

- The console is the only way in. RCON, query, and the management server stay off.
	- RCON sends its password in plain text, and it listens on the same address as the game.
	- Scripts can use `cmd` for what they'd use RCON for.

- The safe settings get checked at every start, and fixed by `repair`, rather than written once by `setup`.
	- A server update, a plugin, or a quick hand edit can turn one back.

- Plugin jars are pinned by hash, and the server can't write to them while it runs.
	- Malware such as Fractureiser spread by changing jars. A changed jar now stops the next start.

- The tree is owned by the service account only while the server runs.
	- The operator can edit configs, add plugins, and back up worlds freely while it's stopped, without sudo.

- The operator gets sudo for single commands, rather than running the whole tool as root.

- Start at boot uses a systemd unit that runs `tt-minecraftd start --foreground` as root, rather than a unit that runs Java directly.
	- The checks, snapshots, and ownership hand-off then work the same way at boot as by hand.

- On NixOS, a NixOS module declares the account, Docker, the config file, the firewall unit, and the boot unit.
	- NixOS has its own `services.minecraft-server` module. It runs vanilla, without plugins, the container, or the checks.

- A bad line in the config gets a warning and its default, rather than stopping the load.
	- Every default is the safe choice, such as the whitelist on and the firewall on.

- The config format is shcl, and the tool reads it with shcl's Go module.

- The firewall rules go in the tool's own nftables table, rather than in ufw, firewalld, or `nftables.conf`. This matches `tt-monerod-safeserver`.

- Bedrock players log in through Floodgate, with only their Xbox account, rather than Geyser's `online` mode.
	- In `online` mode, a Bedrock player also needs a Java account, and most console players don't have one.
	- Java players log in with their own account the same as before.
	- Floodgate doesn't affect adding a server by its domain name. Java on PCs, and Bedrock on phones, tablets, and PCs, can all do that. Consoles can't, either way.

- Code that `tt-monerod-safeserver` needs too moves into a shared Go module, `tt-safeserver`, once `tt-monerod-safeserver` starts on code.
	- Each tool supplies a small Go interface with its own settings, launch details, firewall rules, checks, and commands. The shared module runs the common commands.
	- Each tool still has its own repo, binary, and releases. Other services like these can use the module later.
	- A single binary for every service was looked at, and so was a generic engine driven by a service file. The first ties every service's releases together. The second ends up as Docker Compose again, and Tor, plugin pinning, and the console wouldn't fit it.
	- The module waits for the second tool, because 2 tools that use an interface test its design better than 2 documents do.
	- The module is GPL v2 or later, the same as both tools.

- Tailscale and other VPNs were looked at for remote players, and left out as the main way in.
	- Consoles can't run them, and phones allow only one VPN at a time.
	- Every player would need to install and sign in to an app.
	- A whitelist with online mode, a current Paper, and the firewall below cover a friends server well enough.

## Plan

- The first release is `v1.0.0-alpha.1`.

1. `start`, `stop`, `status`, `watch`, `console`, and `cmd`, with the safe settings check, the plugin check, `--dry-run`, and tests.

2. `repair`, since `setup` reuses its fixes.

3. `setup`, including `--adopt` for an existing server.

4. The firewall.

5. `update`.

6. Start at boot.

7. The NixOS module.

8. Build and release scripts, and an install one-liner.

9. README.

## Architecture

### Software stack

- The tool is written in Go, and it uses the standard library wherever it can.

- Expected outside modules:
	- The shcl Go module, `github.com/yottacore/shcl/source/go/v3`, at its latest published release, beta or not.
	- A YAML package, for `paper-global.yml`, `spigot.yml`, and the Geyser and Floodgate configs.

- At run time it calls `docker` or `systemd-run` and `tmux`, `sudo`, `systemctl`, and `ss`. It also calls `zfs` when snapshots are on, `nft` when the firewall is on, and `apt-get` in `setup` on Debian. `repair` calls `ufw` or `firewall-cmd` to read their rules, when one is active.

### Configuration model

- One file, `/etc/tt-minecraftd/config.shcl`, owned by root and readable by all. `--config` points elsewhere.
	- When the default file is missing, as before `setup`, the defaults are used with a warning. A missing `--config` file is an error.

- The approved plugin list is `/etc/tt-minecraftd/plugins.shcl`, owned by root. It's stored outside the server tree, so the server can't change it.

- Settings come from the defaults in the table below, then the file, then the command line. `--native` sets `runMode`, and `--set` sets any key.
	- A key whose default comes from another one, such as `runGroup`, follows that key wherever it was set.
	- An option is checked the same way as the file. A bad one is an error rather than a warning, since it was typed just now.

- An unknown key or a bad value gets a warning each run, and the default is used.
	- So does a key that's set twice with different values. Neither value is used.
	- A list with one bad item is a bad value, so none of it is used.
	- A `heapMin` over `heapMax` gets a warning, and `heapMax` is used for both. A `memCap` that isn't over `heapMax` gets a warning, and its default.

- Key names and the words such as `docker` and `lan` don't care about case.

- Sizes take Java's form, such as `4G`, or shcl's, such as `4 GiB`. Either way a G is 1024 M, the same as Java. Timeouts take seconds, or a time such as `5m`.

- Settings:

| Key                 | Default                           | Notes
| :---                | :---                              | :---
| `runMode`           | `docker`                          | `docker` or `native`
| `runUser`           | `minecraftd`
| `runGroup`          | same as `runUser`                 | Stays the group before and after a run
| `ownerAfterStop`    | the operator                      | From `SUDO_USER`, else `USER`
| `serverDir`         | `/srv/tt-minecraftd/server`
| `ownerTrees`        | `[<serverDir>]`                   | Every tree that changes hands, such as a world on another dataset
| `zfsDataset`        | none                              | No snapshots when unset
| `dockerImage`       | Temurin 25 JRE, by digest         | `update --image` changes it
| `javaBin`           | `java` on PATH                    | Native mode only
| `heapMin`           | `1G`                              | Or `heapMax`, when that's less
| `heapMax`           | `4G`
| `memCap`            | `heapMax` plus a quarter, plus 1G | Swap is capped the same, so it stays at 0
| `nice`              | `0`
| `jvmArgs`           | Paper's recommended flags
| `extraJvmArgs`      | none
| `bindIP`            | all addresses                     | `server-ip`, and Geyser's address
| `gamePort`          | `25565`                           | TCP, for Java players
| `bedrock`           | `true`                            | Geyser and Floodgate
| `bedrockPort`       | `19132`                           | UDP, for Bedrock players
| `allowFrom`         | `[lan]`                           | Who may connect to the game ports. `lan`, `any`, or addresses and ranges
| `joinRateLimit`     | `20`                              | New connections per minute from one address
| `egress`            | `web`                             | What the server may connect out to. `web` or `open`
| `hideOnlinePlayers` | `true`
| `startTimeout`      | `300`                             | Seconds
| `stopTimeout`       | `120`                             | Seconds
| `firewall`          | `true`                            | See [Firewall](#firewall)
| `startAtBoot`       | `false`
| `extraArgs`         | none                              | Added to the Paper command as given

### Saves and persistence

- The tool doesn't store any state of its own, apart from the approved plugin list. Whether the server runs comes from Docker or systemd. Ownership comes from the the files.

- Server tree layout, as Paper makes it:

~~~text
<serverDir>/paper.jar               link to one of the builds below
<serverDir>/paper-<version>-<build>.jar
<serverDir>/server.properties, bukkit.yml, spigot.yml, config/
<serverDir>/whitelist.json, ops.json, banned-*.json
<serverDir>/plugins/*.jar           approved by hash
<serverDir>/plugins/<name>/         plugin data
<serverDir>/world*/                 worlds, or links to them
<serverDir>/logs/latest.log
~~~

### Privilege model

| Step                                    | Runs as
| :---                                    | :---
| Parsing, checks, `status`, `watch`      | operator
| Read the firewall table                 | operator through sudo
| ZFS snapshot and prune                  | operator through `zfs allow`, else sudo
| Change tree ownership                   | operator through sudo
| `docker run`, `attach`, and `stop`      | operator, in the `docker` group
| `systemd-run` and tmux in native mode   | operator through sudo
| `setup`, `update`, and `repair`         | root
| `start --foreground` from the boot unit | root, from systemd
| The server                              | the service account

- All sudo happens in the foreground, right after one `sudo -v`. Sudo never runs inside a detached session.

### Why Docker

- A Minecraft server parses packets from anyone who can connect to it, and it runs every plugin with its own access. Plugins have had malware in them before, and the server and its plugins have had bugs that let a player run code.

- The service account limits what bad code could do, but only so far. Outside a container it can still:
	- Read any file that's readable by everyone, which is most of `/etc` and often parts of home dirs. Fractureiser searched the whole disk for other jars to infect.
	- See every process on the host, with its command line.
	- Use as much memory and as many processes as the kernel allows.

- In Docker mode the container takes most of that away:
	- It sees the server trees, and the image's files as read-only. The rest of the host's files aren't mounted.
	- `paper.jar` and every plugin jar are mounted read-only on top of the tree, so the server can't change them while it runs.
	- It has its own process list, so it can't see or signal host processes.
	- `--cap-drop ALL` and `no-new-privileges` mean a setuid program in the image can't give it root.
	- The pid limit and the memory cap stop a runaway server from using up the host. When memory runs out anyway, `--oom-score-adj 1000` has the kernel kill the server before the desktop or other services.

- It helps day to day, too:
	- The image is pinned by its digest, so every start uses the same Java whatever the host has installed.
	- A stopped container still has its output, so `docker logs` shows why the server died.

- What it doesn't do:
	- With host networking, the server could still connect to the LAN and to local services. The [Firewall](#firewall) rules block that.
	- The container shares the host's kernel, so a kernel bug could still let code out. It's less isolation than a VM.
	- Being in the `docker` group is as good as root on that host. The operator has sudo already, so that doesn't add a new risk.

### Run modes

- Docker mode runs the container under the service account's uid and gid, with:
	- `--read-only`, and a tmpfs on `/tmp`. The tmpfs allows exec, since Netty and other libraries extract native code there and load it.
	- Each tree in `ownerTrees` mounted at the same path it has on the host, so links between them still work.
	- `paper.jar` and each plugin jar mounted read-only over their own paths.
	- `--cap-drop ALL`, `no-new-privileges`, a pid limit, the memory cap, and `--oom-score-adj 1000`.
	- `--init`, so signals get passed on and child processes get reaped.
	- `-i -t`, so the console works with `docker attach`.
	- There's no `--rm`, so `docker logs` still works after a crash. `start` removes the old container, but only one that has this tool's label.

- Native mode runs Java in a tmux session, under a transient systemd unit that `systemd-run` starts. The unit runs as the service account, with:
	- `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, and `NoNewPrivileges=yes`.
	- Write access only to the trees in `ownerTrees`.
	- An empty capability set, `MemoryMax`, and `TasksMax`.
	- The tmux server on a socket under `/run/tt-minecraftd/`. It never uses the operator's tmux server.

### Tree ownership

- Each tree in `ownerTrees` changes hands with `chown -R`. A missing tree gets a warning.
	- Before a start, the trees go to `runUser`. After a stop, they go to `ownerAfterStop`.
	- The group is always `runGroup`, and the operator is in it. So logs and `status` work while the server runs.

- At start, modes become `ug+rwX,o-rwx`. The server runs with `umask 0007`, so new files follow the same rule.

- In native mode, `paper.jar` and the plugin jars go to root, mode 0644, at start. That way the server can't change them, same as the read-only mounts in Docker mode.

- A tree that's `/` or less than 3 levels deep is refused. A tree given as a link is followed, and the same check applies to where it goes.

- With `ownerAfterStop` empty, as when the boot unit stops the server, the trees stay with `runUser`. The operator's next `stop` hands them back.

### Safe server settings

- `start` checks these, and `repair` sets them:

| File                | Setting                           | Value
| :---                | :---                              | :---
| `server.properties` | `online-mode`                     | `true`
| `server.properties` | `white-list`, `enforce-whitelist` | `true`
| `server.properties` | `enable-rcon`                     | `false`
| `server.properties` | `enable-query`                    | `false`
| `server.properties` | `management-server-enabled`       | `false`
| `server.properties` | `enable-jmx-monitoring`           | `false`
| `server.properties` | `server-ip`, `server-port`        | from `bindIP` and `gamePort`
| `server.properties` | `hide-online-players`             | from `hideOnlinePlayers`
| `spigot.yml`        | `settings.bungeecord`             | `false`
| `paper-global.yml`  | `proxies.velocity.enabled`        | `false`
| Geyser `config.yml` | `bedrock.address`, `bedrock.port` | from `bindIP` and `bedrockPort`
| Geyser `config.yml` | `auth-type`                       | `floodgate`

- With online mode off, anyone can join under any name, including an op's. With a proxy setting on, anyone who connects to the backend directly can claim to be anyone. Those 2 are the most common ways servers get taken over.

- An empty whitelist with `white-list` on locks everyone out. `setup` and `repair` warn about that, and print the `cmd` lines that add players.

- `start` also warns when there are more than 2 ops, or when an op isn't on the whitelist.

- `-Dlog4j2.formatMsgNoLookups=true` is always in the Java arguments. Current Paper doesn't need it, but it doesn't cost anything.

### Plugin pinning

- `/etc/tt-minecraftd/plugins.shcl` lists each approved jar by file name, SHA-256, and an optional source for `update --plugins`.
	- The name is the jar's path under `serverDir`, so a jar in `plugins/` and an extension with the same name stay apart:

		~~~text
		jar: plugins/Geyser-Spigot.jar
			sha256: 64 hex digits
			source: https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/spigot
		jar: paper-26.3-244.jar
			sha256: 64 hex digits
		~~~

	- An entry with no hash, a bad hash, or a path outside `serverDir` gets a warning and is left out, so its jar counts as not approved. A source that isn't `https` is ignored, and the hash still counts.

- `start` hashes every jar in `plugins/`, every Geyser extension in `plugins/Geyser-Spigot/extensions/`, and `paper.jar`. Extensions run with the same access as plugins. A jar that isn't listed, or doesn't match, stops the start. The message names the jar and `repair --approve-plugins`.

- `repair --approve-plugins` shows each new or changed jar with its hash, its size, its date, and the name and version inside its `plugin.yml` or `paper-plugin.yml`. It asks about each one. A refused jar gets moved out of `plugins/` to `plugins-refused/`.

- Plugins should come from known sources, such as Hangar, Modrinth, GeyserMC, or the author's own site. The tool can't judge a jar, but it does make sure a jar doesn't change without a person looking at it.

### Bedrock players

- Geyser lets Bedrock clients join the Java server. Floodgate lets them join with only their Xbox account, so they don't need a Java account too.

- Geyser checks the Xbox login, and Floodgate passes it on to the server, signed with a key in `plugins/floodgate/key.pem`. The key is a secret, so `repair` makes sure only the account and group can read it.

- Bedrock players get a prefix on their names, a `.` by default. They're added to the whitelist with Floodgate's `fwhitelist add <gamertag>` command, or `whitelist add .<gamertag>`.

- On the LAN, Bedrock clients find the server by itself, under Friends, LAN Games. That includes consoles. Geyser has to listen on all addresses for that, since a socket that's bound to one address doesn't get broadcasts.

- Over the internet, phones, tablets, and Bedrock on PCs can add the server by address and port. Consoles can't add a server by address. See [Needs more thought](#needs-more-thought).

### Exposure

- `allowFrom` says who may connect to the game ports. The firewall enforces it.
	- `lan` is the private address ranges, IPv4 and IPv6.
	- `any` allows everyone.
	- Addresses and ranges can be mixed with `lan`.

- A server that players join over the internet needs `allowFrom` set to `any`, plus one of these:
	- A router port forward for `gamePort` over TCP and `bedrockPort` over UDP, to the server's LAN address. A DNS name for the home address makes it easier for players.
	- A tunnel such as playit.gg. Players connect to the tunnel's address, and the home address stays hidden. The tunnel's agent connects to the server on localhost.

- Public servers get found by scanners within hours, so the whitelist and online mode matter most there. `hideOnlinePlayers` and a port other than 25565 cut down on the noise from scanners, but neither stops anyone.

- With a port forward, the home address is public, and so is anything that answers on it.

### Firewall

- With `firewall` on, the tool puts its rules in its own nftables table, `inet tt_minecraftd`, so they apply to IPv4 and IPv6.

- `setup` and `repair` write the rules to `/etc/tt-minecraftd/firewall.nft`, from the config. `tt-minecraftd-firewall.service` loads that file at boot.
	- It loads it again whenever `nftables.service` starts, restarts, or reloads. Debian's `nftables.conf` flushes every table, the tool's included.
	- The file deletes the old table and adds the new one in a single nft transaction, so the host is never without the rules.

- Inbound, new connections to the game ports:
	- Ones from loopback are always allowed. A tunnel agent connects that way.
	- Ones from an address that's not in `allowFrom` get dropped.
	- Past `joinRateLimit` a minute from one address, the rest get dropped. That slows down bot join floods.

- Inbound, new connections from anywhere but loopback get dropped on the RCON, query, and management ports, even though the server should never open them.

- Outbound, new connections from the service account:
	- Ones to loopback get dropped. That way the server can't connect to local services such as databases.
	- Ones to the private ranges get dropped, apart from DNS to the resolvers in `/etc/resolv.conf`.
	- With `egress` set to `web`, ones to the internet are allowed on ports 80 and 443 only. Online mode, Geyser, and Floodgate need HTTPS to Mojang, Microsoft, and GeyserMC.
	- With `egress` set to `open`, ones to the internet are allowed on any port.
	- Everything that gets dropped gets counted too, and logged at a limited rate. `status` shows the counts.
	- Replies to connections that came in still go out.
	- It works the same in both modes, since a host-network container's sockets belong to the service account's uid.

- The table only drops traffic. A drop in another table still applies, so ufw or firewalld also has to let players in. `repair` checks for that rule, and prints the command when it's missing. The tool never changes another firewall's rules.

### Boot

- With `startAtBoot` on, `setup` and `repair` install `tt-minecraftd.service` and enable it.
	- `ExecStart` runs `tt-minecraftd start --foreground`, and `ExecStop` runs `tt-minecraftd stop`.
	- `TimeoutStopSec` is a little longer than `stopTimeout`, so systemd doesn't kill the server first.
	- It starts after `network-online.target`, and after `docker.service` in Docker mode.
	- With `firewall` on, it requires `tt-minecraftd-firewall.service`, so the server never starts at boot without the rules.

- With `startAtBoot` off, `repair` disables and removes the unit.

### NixOS

- The repo has a flake with a NixOS module. It declares:
	- The service account and group.
	- Docker, in Docker mode.
	- `/etc/tt-minecraftd/config.shcl`, written from module options.
	- The firewall unit and its rules, and the ports in NixOS's own firewall.
	- The boot unit.

- `tt-minecraftd` detects NixOS by the `/etc/NIXOS` file. There, `setup` and `repair` still handle the server tree, Paper, the plugins, and the safe settings. For the rest they only check, and print the module options to set.

### ZFS snapshots

- With `zfsDataset` set, `start` takes `<YYYYmmDD-HHMMSS>_pre_tt-minecraftd-start`, and `stop` takes `<YYYYmmDD-HHMMSS>_post_tt-minecraftd-stop` after the worlds are saved.

- Old ones get pruned on a grandfather-father-son schedule. The ones kept are the first, the newest 3, and the last of each of the last 7 days, 4 weeks, and 6 months.
	- Only snapshots that match the tool's name pattern are touched.

- Snapshots on the same pool aren't a backup. Copying them off of the host, such as with `zfs send`, is up to the host's own backup.

- `setup` offers `zfs allow` for snapshot, destroy, and mount on the dataset, so this works without sudo.

### UI

- Output is plain text, with a blank line at the start and the end, and one between sections. It uses color only on a terminal.

- `--help` lists the commands and options. `--about` describes the tool. `--version` prints the version and build.

### Security

- How the common attacks on Minecraft servers are handled:

| Attack                                   | What `tt-minecraftd` does
| :---                                     | :---
| Scanners find the server and bots join   | Whitelist forced on. Scanner noise cut by `hideOnlinePlayers`
| Offline mode, joining under an op's name | Online mode forced on. Bedrock logins checked through Floodgate
| Log4Shell                                | Only current Paper builds. The log4j lookup flag set anyway
| BleedingPipe, in Forge mods              | Out of scope, since modded servers aren't supported
| Malicious plugins, such as Fractureiser  | Jars pinned by hash and read-only while running. Container. Outbound firewall
| Exposed RCON                             | RCON forced off, and its port dropped. `cmd` used instead
| Crash exploits                           | `update --paper` updates Paper to the newest build
| Join floods                              | Per-address rate limit in the firewall, and Paper's own throttle
| DDoS, and the home address being public  | A tunnel hides the home address. The host can't stop a big flood
| Proxy bypass                             | Proxy settings forced off, since proxies aren't supported

- The service account doesn't have sudo, a login shell, a home, or any other groups.

- In Docker mode the server can't see anything outside its trees and the image. In native mode systemd's sandbox limits it to its trees too, with the rest of the system read-only.

- The config doesn't have any secrets in it. The Floodgate key is the one secret in the server tree, with mode 0660.

- With the firewall on, the server can't open connections to the LAN or to local services, and a try gets counted.

- Every download is checked before use. Paper and plugins need a matching hash from their download API. Tool releases need a matching SHA-256 sum.

### Testing

- Unit tests run with the fake command runner. They check the exact command lists for each mode, the config errors, the safe settings checks and fixes on sample server files, and the plugin check against changed and new jars. They also check the rendered `firewall.nft` for each mix of `allowFrom`, `egress`, and `bedrock`.

- A smoke test starts a real Paper server in the container on a throwaway tree with a tiny world. It checks the hardening flags, that the server finishes loading, that `cmd` works, that the jars can't be written, and a clean exit on `stop`.

- A live start and stop on a real server checks ownership, the listening ports, that a Java and a Bedrock client can join, and that a player who isn't whitelisted gets turned away.

- With the firewall on, a connection from the service account to a LAN address or to localhost fails and bumps the drop counter.

## Needs more thought

- Consoles can't join over the internet yet, since Xbox, PlayStation, and Switch can't add a server by address. The usual ways around it are the MCXboxBroadcast Geyser extension, which shows the server in the Friends tab through an Xbox account, and BedrockConnect, which needs a DNS change on each console. A phone app or PC program on the console's own network can also show the server under LAN Games. MCXboxBroadcast needs the least from players, but a spare Xbox account's login has to be stored on the server, and may need more outbound access than `egress` set to `web` allows. It's the pick for a later release. Until then, a console that uses BedrockConnect can join from anywhere.

- Behind a tunnel, every player shows up as coming from localhost, unless the tunnel supports the PROXY protocol. IP bans and the per-address rate limit then don't work.

- Hot backups while the server runs, with `save-off` and `save-all flush` through `cmd`, then a snapshot, then `save-on`. That would help a server that runs for weeks without a stop.

- Whether `enforce-secure-profile` should be forced on, and how it gets along with Floodgate.

- `memCap` of a quarter above the heap plus 1G is a guess. Geyser and big view distances use more memory outside the heap.

- Tool releases could be signed, beyond a SHA-256 sum. Paper builds aren't signed either, only hashed.
