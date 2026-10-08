# Deploying Gifty

This is one way to run Gifty for yourself: a single small Linux server (the scripts assume an AWS Lightsail Ubuntu 24.04 instance, but any Ubuntu host works), Caddy for HTTPS, and Amazon SES for email. Use whatever you like; none of it is required, and Gifty also runs fine with `go run .`.

## What the scripts do

| Piece | Details |
| --- | --- |
| `setup.sh` | Adds swap, installs Caddy and creates the `gifty` user and directories. Safe to rerun |
| `deploy.sh user@host` | Runs the tests, builds a Linux binary, installs it with the service, backup timer and Caddy config, and restarts Gifty. Data is not touched |
| `set-access-code.sh user@host` | Prompts for a new `GIFTY_ACCESS_CODE` (empty removes it, `generate` makes a random one), updates `/etc/gifty/env` and restarts Gifty |
| `Caddyfile` | Serves `GIFTY_DOMAIN`, gets a certificate and proxies to Gifty on 127.0.0.1:8080. Writes an access log to `/var/log/caddy`, with invitation codes, friend links and unsubscribe links blanked |
| `fail2ban/` | Filters and jails that ban addresses causing repeated `security:` events in the Gifty journal or 400/404/405 responses in the access log. `deploy.sh` installs them. Put your own address in `fail2ban/jail.d/gifty.local` first (see Monitoring) |
| `gifty.service` | Runs Gifty as the `gifty` user with `/etc/gifty/env` for configuration, trusting Caddy for client addresses (`GIFTY_TRUST_PROXY`) |
| `gifty-backup.*` | Daily copy of the data file and the photos directory to `/var/lib/gifty/backups`, kept 14 days. It does not copy `gifty.key` |
| `env.example` | Every setting the server reads; copy to `/etc/gifty/env` (root:gifty, mode 640) and never commit the real file |

## Setting up

1. Create a server, point a DNS `A` record for your domain at it, and open ports 80 and 443. Open port 22 only to the addresses you administer from.
2. Run `ssh user@host sudo sh -s < deploy/setup.sh`.
3. Write `/etc/gifty/env` from `env.example`: set `GIFTY_DOMAIN`, `GIFTY_BASE_URL`, `GIFTY_MAIL_FROM`, the SMTP credentials and, if you want a private instance, `GIFTY_ACCESS_CODE`.
4. Run `deploy/deploy.sh user@host`. Caddy gets a certificate once DNS resolves.
5. To restore data, stop Gifty, copy a backup to `/var/lib/gifty/gifty.json` (owner `gifty`, mode 600), copy the same day's `photos-<date>` directory's files into `/var/lib/gifty/photos`, and start it again. Gifty removes photo files the data file doesn't mention when it starts, so copying in extra photos is harmless. `/var/lib/gifty/gifty.key` stays where it is; on a new server Gifty creates a fresh one, which only means people re-enter the access code and old unsubscribe links stop working.

## Email with Amazon SES

Verify a domain identity, publish the DKIM records it gives you, and set a custom MAIL FROM domain, SPF and a DMARC record. New SES accounts start in the sandbox, where mail only reaches verified addresses; request production access before inviting people. Create an IAM user allowed to `ses:SendRawEmail` only from your sender address, and derive the SMTP password from its access key with the SES SigV4 algorithm for your region. Set up bounce and complaint notifications (an SNS topic that emails you works), and once mail flows reliably consider tightening DMARC from `p=none` to `p=quarantine`.

Any other SMTP provider that offers STARTTLS works with the same settings.

## Administrators

List administrator email addresses in `GIFTY_ADMINS`. An administrator must have a confirmed email address, so email must be on. They get an Admin page to remove accounts and exchanges. It never shows who is buying for whom.

## Monitoring

Set `GIFTY_ALERT_EMAIL` in `/etc/gifty/env` (email must be on) to get a warning when failed sign-ins and similar events spike, and a daily summary. `deploy.sh` also installs fail2ban with the jails in `deploy/fail2ban/`. So that you can't ban yourself, create `fail2ban/jail.d/gifty.local` before deploying, with the addresses you administer from. Git ignores the file, so your address never ends up in the repository, and `deploy.sh` installs it when it is there:

```ini
[DEFAULT]
ignoreip = 127.0.0.1/8 ::1 203.0.113.9
```

Afterwards:

```sh
sudo fail2ban-client status gifty        # who is banned for app events
sudo fail2ban-client status gifty-web    # who is banned for probing
sudo fail2ban-client set gifty unbanip 203.0.113.9
```

Banning works on the address Caddy sees, so it needs the firewall on the host (nftables), not only the Lightsail firewall. [docs/security.md](../docs/security.md#monitoring) lists what is logged.
