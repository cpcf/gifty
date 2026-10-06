# Deploying Gifty

Gifty runs at https://gifty.connorfleming.co.uk on one Lightsail instance in eu-west-2 (AWS CLI profile `gifty`). Caddy terminates HTTPS and proxies to Gifty on 127.0.0.1:8080. Email goes through Amazon SES.

## Everyday deploy

```sh
deploy/deploy.sh ubuntu@gifty.connorfleming.co.uk
```

It runs the tests, builds a Linux binary, copies it with the service, backup timer and Caddy config, and restarts Gifty. Data is not touched.

## What exists

| Piece | Where |
| --- | --- |
| Server | Lightsail `gifty` (`nano_3_0`, Ubuntu 24.04), static IP `gifty-ip`, ports 22/80/443, daily snapshots at 02:00 UTC |
| SSH | `ubuntu@gifty.connorfleming.co.uk` with `~/.ssh/id_ed25519` (Lightsail key pair `gifty-ed25519`) |
| Data | `/var/lib/gifty/gifty.json`; daily copies in `/var/lib/gifty/backups` kept 14 days (`gifty-backup.timer`) |
| Config and secrets | `/etc/gifty/env` (root:gifty, 640); see `env.example`. Not in the repository |
| DNS | Route 53 zone `gifty.connorfleming.co.uk`, delegated by four NS records named `gifty` in Squarespace. The rest of connorfleming.co.uk stays at Squarespace |
| Records | `A` app; three DKIM `CNAME`s; `MX` and SPF `TXT` on `mail.gifty…` (MAIL FROM); `TXT` `_dmarc.gifty…` (`p=none`) |
| Email | SES identity `gifty.connorfleming.co.uk`, sender `noreply@gifty.connorfleming.co.uk`; production access granted (no recipient verification needed); IAM user `gifty-ses-smtp` may only send from that address |
| Bounces and complaints | Account suppression list; SNS topic `gifty-ses-feedback` emails the owner |
| Cost alert | Budget "Gifty monthly", $10 |

## Rebuilding from scratch

1. `aws lightsail create-instances … --blueprint-id ubuntu_24_04 --bundle-id nano_3_0 --user-data file://deploy/setup.sh`, then attach a static IP and open ports 22, 80 and 443. `setup.sh` adds swap (the 512 MB plan runs out of memory during upgrades without it), installs Caddy and creates the `gifty` user and directories. It is safe to rerun: `ssh ubuntu@host sudo sh -s < deploy/setup.sh`.
2. Point the `A` record at the static IP.
3. Write `/etc/gifty/env` from `env.example`. The SMTP password is derived from the IAM access key with the SES SigV4 algorithm for eu-west-2.
4. Run `deploy/deploy.sh`. Caddy gets a certificate once DNS resolves.
5. Restore data by stopping Gifty, copying a backup to `/var/lib/gifty/gifty.json` (owner `gifty`, mode 600) and starting it again.

## Still to do

- Once mail has been arriving reliably for a while, tighten DMARC to `p=quarantine`.
