#!/bin/sh
# Lightsail launch script: runs once as root when the instance first boots.
# It prepares the machine; deploy.sh installs and starts Gifty itself.
# Safe to run again, e.g. `ssh ubuntu@host sudo sh -s < deploy/setup.sh`.
set -eu
export DEBIAN_FRONTEND=noninteractive
# The 512 MB plan runs out of memory during package upgrades without swap.
if [ ! -f /swapfile ]; then
	fallocate -l 1G /swapfile
	chmod 600 /swapfile
	mkswap /swapfile
	echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi
swapon /swapfile 2>/dev/null || true
apt-get -o DPkg::Lock::Timeout=600 update
# Automatic updates start at first boot; wait for them before finishing any interrupted install.
while pgrep -x unattended-upgr >/dev/null || pgrep -x dpkg >/dev/null; do sleep 5; done
dpkg --configure -a
apt-get -o DPkg::Lock::Timeout=600 -y upgrade
apt-get -o DPkg::Lock::Timeout=600 install -y caddy unattended-upgrades
id gifty >/dev/null 2>&1 || useradd --system --home-dir /var/lib/gifty --shell /usr/sbin/nologin gifty
install -d -o gifty -g gifty -m 700 /var/lib/gifty /var/lib/gifty/backups
install -d -m 755 /opt/gifty
install -d -m 750 -g gifty /etc/gifty
touch /etc/gifty/env
chown root:gifty /etc/gifty/env
chmod 640 /etc/gifty/env
