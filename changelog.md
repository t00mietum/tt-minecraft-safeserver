# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

<!--
## vNEXT - DATE

### Notes

### Added

### Changed

### Removed

### Other work
-->

## v1.0.0-beta.1 - 2026-11-23

### Notes

- The second pre-release. Most of the work since alpha.1 came out of 2 code reviews, and makes root safer around the server tree and the safe settings check harder to get past. Config keys may still change.

- `install.bash` from this release on needs this release's build, since it keeps replaced builds through `tt-minecraftd retire-build`.

### Added

- Replaced builds in `/usr/local/bin` are kept in `/var/lib/tt-minecraftd/old-builds`, with older copies thinned out over time. `repair`, `setup` and `install.bash` all do this.

- The installers take `--any-distro`, and `install-dev.bash` takes `--install-docker`. `install.bash` checks the build against the release's `SHA256SUMS`, and stops on a box where the package is installed.

- A new install that stops partway can be run again without `--adopt`.

- `--set` refuses a value that an unquoted `#` would cut short, and the config warns about a comment pressed against a value.

### Changed

- Plugins an admin adds are trusted. Nothing checks jars against a list, and `plugins.shcl` holds only optional download sources for `update --plugins`.

- `update` removes the plugins' libraries after updating plugins, so Paper fetches fresh ones at the next start.

- The `.deb` and `.rpm` put `tt-minecraftd` in `/usr/bin`. Hand installs stay in `/usr/local/bin`.

- Root's changes inside the server tree no longer follow links, and `start` refuses a plugin jar that leads outside the server's folders.

- The safe settings check reads `spigot.yml` dotted keys, YAML anchors and repeated sections, and `server.properties` line ends and comments the way the server does.

- Dry runs and `repair --check` never ask for a sudo password.

- The update agent's docs and setup's config lead with a logged-in CLI subscription, with the API kinds as the fallback.

- Downloads refuse a redirect from https to plain http.

### Removed

- Plugin hash pinning and `repair --approve-plugins`.

### Other work

- A push to main is gated on the commit being pushed, from a clean checkout.

- CodeQL, fuzzing and soak tests are part of the pipeline.

## v1.0.0-alpha.1 - 2026-11-14

### Notes

- The first release. It replaces the old start and stop scripts on one home server, and is alpha: commands and config keys may still change.

- Runs on Debian, amd64 and arm64. NixOS, native mode, and `update --self` are deferred.

### Added

- `start`, `stop`, `status`, and `watch`, in Podman or Docker, under the `minecraftd` account. `status` needs no sudo.

- Safe settings checked before every start, and plugin jars pinned by SHA-256 and mounted read-only.

- `console` and `cmd` for the server console, in place of RCON.

- `setup`, for a new install or an existing server folder with `--adopt`, including Paper, Geyser, and Floodgate.

- `repair`, which checks the install and fixes what drifted.

- `update` for Paper, plugins, and the container image, with `--mc-version` and a ZFS snapshot before the move. An optional update agent finds plugin builds that have no known source.

- The firewall's own nftables table, with an inbound allow list, a join rate limit, and outbound blocks to loopback and the LAN.

- A boot unit, through `startAtBoot`.

- `install.bash` for users and `install-dev.bash` for developers.

- Reproducible release builds, with `verify-release.bash` to check them, and `.deb` and `.rpm` packages.
