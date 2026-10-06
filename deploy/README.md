# Deploying Gifty

This is one way to run Gifty for yourself: a single small Linux server (the scripts assume an AWS Lightsail Ubuntu 24.04 instance, but any Ubuntu host works), Caddy for HTTPS, and Amazon SES for email. Use whatever you like; none of it is required, and Gifty also runs fine with `go run .`.

## What the scripts do

| Piece | Details |
| --- | --- |
| `setup.sh` | Adds swap, installs Caddy and creates the `gifty` user and directories. Safe to rerun |
| `deploy.sh user@host` | Runs the tests, builds a Linux binary, installs it with the service, backup timer and Caddy config, and restarts Gifty. Data is not touched |
| `Caddyfile` | Serves `GIFTY_DOMAIN`, gets a certificate and proxies to Gifty on 127.0.0.1:8080 |
| `gifty.service` | Runs Gifty as the `gifty` user with `/etc/gifty/env` for configuration |
| `gifty-backup.*` | Daily copy of the data file to `/var/lib/gifty/backups`, kept 14 days |
| `env.example` | Every setting the server reads; copy to `/etc/gifty/env` (root:gifty, mode 640) and never commit the real file |

## Setting up

1. Create a server, point a DNS `A` record for your domain at it, and open ports 80 and 443. Open port 22 only to the addresses you administer from.
2. Run `ssh user@host sudo sh -s < deploy/setup.sh`.
3. Write `/etc/gifty/env` from `env.example`: set `GIFTY_DOMAIN`, `GIFTY_BASE_URL`, `GIFTY_MAIL_FROM`, the SMTP credentials and, if you want a private instance, `GIFTY_ACCESS_CODE`.
4. Run `deploy/deploy.sh user@host`. Caddy gets a certificate once DNS resolves.
5. To restore data, stop Gifty, copy a backup to `/var/lib/gifty/gifty.json` (owner `gifty`, mode 600) and start it again.

## Email with Amazon SES

Verify a domain identity, publish the DKIM records it gives you, and set a custom MAIL FROM domain, SPF and a DMARC record. New SES accounts start in the sandbox, where mail only reaches verified addresses; request production access before inviting people. Create an IAM user allowed to `ses:SendRawEmail` only from your sender address, and derive the SMTP password from its access key with the SES SigV4 algorithm for your region. Set up bounce and complaint notifications (an SNS topic that emails you works), and once mail flows reliably consider tightening DMARC from `p=none` to `p=quarantine`.

Any other SMTP provider that offers STARTTLS works with the same settings.

## Administrators

List administrator email addresses in `GIFTY_ADMINS`. An administrator must have a confirmed email address, so email must be on. They get an Admin page to remove accounts and exchanges. It never shows who is buying for whom.
