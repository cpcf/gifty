#!/bin/sh
# Build Gifty for the server and install it. Usage: deploy/deploy.sh [user@host]
set -eu
host=${1:-ubuntu@gifty.connorfleming.co.uk}
cd "$(dirname "$0")/.."
go test ./...
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$out/gifty" .
cp deploy/gifty.service deploy/gifty-backup.service deploy/gifty-backup.timer deploy/Caddyfile "$out/"
ssh "$host" 'rm -rf /tmp/gifty-deploy && mkdir /tmp/gifty-deploy'
scp -q "$out"/* "$host:/tmp/gifty-deploy/"
ssh "$host" 'sudo sh -s' <<'EOF'
set -eu
cd /tmp/gifty-deploy
install -m 755 gifty /opt/gifty/gifty
install -m 644 gifty.service gifty-backup.service gifty-backup.timer /etc/systemd/system/
install -m 644 Caddyfile /etc/caddy/Caddyfile
systemctl daemon-reload
systemctl enable --now gifty-backup.timer
systemctl enable gifty
systemctl restart gifty
systemctl reload caddy
rm -rf /tmp/gifty-deploy
sleep 1
systemctl is-active gifty
EOF
echo "Deployed to $host"
