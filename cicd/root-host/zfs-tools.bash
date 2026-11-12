#!/bin/bash
# zfs and zpool for root-host.bash --zfs, as root in the container. Nothing of
# zfsutils may start: an import scan there would see this host's own pools.
set -euo pipefail

units=(zfs-import-cache.service zfs-import-scan.service zfs-import.target zfs-mount.service
	zfs-share.service zfs-volume-wait.service zfs-volumes.target zfs-load-key.service zfs-zed.service
	zfs.target zfs-scrub-monthly@.timer zfs-scrub-weekly@.timer zfs-trim-monthly@.timer zfs-trim-weekly@.timer)
systemctl mask "${units[@]}" > /dev/null 2>&1
printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d
chmod 755 /usr/sbin/policy-rc.d
sed -i 's/^Components: main$/Components: main contrib/' /etc/apt/sources.list.d/debian.sources
apt-get update -qq > /dev/null
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends zfsutils-linux > /dev/null
rm /usr/sbin/policy-rc.d
zfs version
