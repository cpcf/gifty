#!/bin/sh
# Build Gifty for the server and install it. Usage: deploy/deploy.sh user@host
set -eu
host=${1:?Usage: deploy/deploy.sh user@host}
cd "$(dirname "$0")/.."
go test ./...
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$out/gifty" .
cp deploy/gifty.service deploy/gifty-backup.service deploy/gifty-backup.timer deploy/Caddyfile deploy/caddy-env.conf "$out/"
cp -R deploy/fail2ban "$out/fail2ban"
ssh "$host" 'rm -rf /tmp/gifty-deploy && mkdir /tmp/gifty-deploy'
scp -rq "$out"/* "$host:/tmp/gifty-deploy/"
ssh "$host" 'sudo sh -s' <<'EOF'
set -eu
cd /tmp/gifty-deploy
install -m 755 gifty /opt/gifty/gifty
install -m 644 gifty.service gifty-backup.service gifty-backup.timer /etc/systemd/system/
install -m 644 Caddyfile /etc/caddy/Caddyfile
mkdir -p /etc/systemd/system/caddy.service.d
install -m 644 caddy-env.conf /etc/systemd/system/caddy.service.d/gifty.conf
# Caddy writes the access log fail2ban reads. Servers set up before monitoring existed need fail2ban installed here.
install -d -o caddy -g caddy -m 755 /var/log/caddy
[ -e /var/log/caddy/access.log ] || install -o caddy -g caddy -m 644 /dev/null /var/log/caddy/access.log
command -v fail2ban-client >/dev/null || apt-get -o DPkg::Lock::Timeout=600 install -y fail2ban nftables
install -m 644 fail2ban/filter.d/*.conf /etc/fail2ban/filter.d/
install -m 644 fail2ban/jail.d/gifty.conf /etc/fail2ban/jail.d/
fail2ban-client -t
systemctl daemon-reload
systemctl enable --now gifty-backup.timer
systemctl enable gifty
systemctl restart gifty
systemctl restart caddy
systemctl enable fail2ban
systemctl restart fail2ban
rm -rf /tmp/gifty-deploy
sleep 1
systemctl is-active gifty fail2ban
EOF
echo "Deployed to $host"
