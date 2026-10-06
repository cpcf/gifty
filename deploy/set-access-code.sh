#!/bin/sh
# Change the access code and restart Gifty. Usage: deploy/set-access-code.sh user@host
# The code is typed at a prompt, so it stays out of shell history and process lists.
# Press return with nothing typed to remove the code and open signup to anyone, or type "generate" for a random one.
set -eu
host=${1:?Usage: deploy/set-access-code.sh user@host}
printf 'New access code (empty removes it, "generate" makes a random one): '
stty -echo 2>/dev/null || true
trap 'stty echo 2>/dev/null || true' EXIT
IFS= read -r code
stty echo 2>/dev/null || true
echo
if [ "$code" = generate ]; then
	code=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 16)
	echo "Generated code: $code"
elif [ -n "$code" ] && [ ${#code} -lt 10 ]; then
	echo "Warning: that code is short. Wrong guesses are limited, but a longer code is safer." >&2
fi
case $code in
*[!A-Za-z0-9._@:+=,/-]*) echo "Use only letters, digits and . _ @ : + = , / - so the env file reads it back unchanged." >&2; exit 1 ;;
esac
# The code travels on stdin; the remote script rewrites the line without sed so no character needs escaping.
printf '%s\n' "$code" | ssh "$host" 'sudo sh -c "
set -eu
IFS= read -r code
f=/etc/gifty/env
tmp=\$(mktemp)
grep -v \"^GIFTY_ACCESS_CODE=\" \$f > \$tmp || true
[ -z \"\$code\" ] || echo \"GIFTY_ACCESS_CODE=\$code\" >> \$tmp
cat \$tmp > \$f
rm -f \$tmp
systemctl restart gifty
sleep 1
systemctl is-active gifty
"'
if [ -n "$code" ]; then echo "Access code changed on $host"; else echo "Access code removed on $host"; fi
