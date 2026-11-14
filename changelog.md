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
