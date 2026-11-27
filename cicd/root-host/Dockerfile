# A throwaway Debian host with systemd and a real sudo, for the steps that need
# root. Podman and nftables are left out, since setup installs them.
FROM docker.io/library/debian:trixie-20251117

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		systemd systemd-sysv dbus sudo tmux procps iproute2 curl ca-certificates \
		python3 openjdk-25-jre-headless \
	&& apt-get clean \
	&& rm -rf /var/lib/apt/lists/*

# What a fresh install wouldn't run, and would fight the container over.
# Privileged means /proc/sys is the host's, so systemd-sysctl here set the host's kernel.core_pattern to "core" and broke its crash dumps. binfmt is host-wide too.
RUN systemctl mask getty.target systemd-logind.service systemd-firstboot.service systemd-modules-load.service \
		systemd-networkd-wait-online.service systemd-sysctl.service systemd-binfmt.service \
	&& useradd -m -s /bin/bash op \
	&& printf 'op ALL=(ALL) NOPASSWD: ALL\n' > /etc/sudoers.d/op \
	&& chmod 0440 /etc/sudoers.d/op

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
