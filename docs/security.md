# Security

Gifty is built for a small, trusted audience, but it is meant to be safe on the public internet. This page describes what it does and what it doesn't.

## What it protects

- **Passwords:** salted PBKDF2-HMAC-SHA256, 600,000 iterations, 12 to 256 characters. Hashing runs outside the state lock, at most two at a time, and unknown addresses are hashed too so timing doesn't reveal which accounts exist.
- **Sessions:** 256-bit random tokens stored as SHA-256 digests, in HttpOnly, SameSite=Lax cookies (Secure behind HTTPS). Sign-in issues a fresh token; sign-out, password reset and account deletion end sessions.
- **Other tokens:** confirmation, reset and invitation tokens are 256-bit random. Confirmation and reset links are stored as digests, expire (48 hours and one hour) and work once. Unsubscribe links are derived from the server key in `gifty.key`, so they aren't stored.
- **Authorisation:** every exchange and wish endpoint checks membership or ownership; organiser actions check the owner; admin endpoints need a confirmed address in `GIFTY_ADMINS`. Assignments are never serialised to the client except to the person they are for. A confirmed address is required for admin so nobody becomes administrator by signing up with an administrator's address first.
- **Cross-site requests:** a POST must name its own origin (`Origin` matching the host, or `Sec-Fetch-Site` of `same-origin` or `none`). One-click unsubscribe is the only exception, and its token is its credential.
- **Content:** user content is escaped when rendered, the content security policy allows only the app's own scripts and styles (and `blob:` images, for previewing a photo before it is saved), and the headers include `X-Content-Type-Options`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Permissions-Policy` and `Cross-Origin-Opener-Policy`. API responses are `no-store`. Idea photos are stored in the data file and served from `/image/` under the same origin, so `img-src 'self'` covers them and no image is fetched from elsewhere; a photo is accepted only if its bytes match its declared type by magic number (JPEG, PNG, GIF or WebP), and it is sent with `nosniff` and `Cache-Control: private, no-store`. A photo is visible only to its owner and to the person buying for them in an exchange the idea is shown in, the same rule as the wish list.
- **Input:** JSON only, unknown fields rejected, bodies capped at 32 KB (1 MB for the idea form, which can carry a photo). Email addresses must round-trip through `mail.ParseAddress` unchanged, and subjects are stripped of line breaks and encoded, so headers can't be injected.
- **Storage:** the data file is written 0600 in a 0700 directory by atomic rename. The systemd unit in `deploy/` runs as an unprivileged user with a read-only system and loopback-only binding.
- **Email:** SMTP requires STARTTLS before credentials are sent. Account emails are capped at 5 per person per day and 100 an hour site-wide, exchange emails at 20 per person per day.

## Abuse limits

- **Per client:** 30 sign-in, signup, reset, confirmation and unsubscribe attempts per 15 minutes, and 300 other writes. Clients are identified by connection address (IPv6 by its /64); behind a reverse proxy set `GIFTY_TRUST_PROXY` so the proxy's `X-Forwarded-For` is used.
- **Per account:** 10 sign-in attempts per 15 minutes, counted before the password is hashed. A good sign-in or a password reset clears the count.
- **Access code:** with `GIFTY_ACCESS_CODE` set, signing in and signing up need it (or, for signup, an open invitation). Wrong codes share one budget of 20 per 15 minutes across every client; once it is spent even the right code is refused until the window ends. Use a long random code (`deploy/set-access-code.sh` can generate one).
- **Hashing:** requests only reach the password hasher if they could succeed: past the gate, with a known reset token, or with a session. Anonymous callers can't keep the hashing slots busy.
- **Size:** 20 active exchanges per organiser, 100 people per exchange, 100 ideas per person, five photos per idea of up to 512 KB each and 16 MB of photos per person, and site-wide ceilings of 2,000 accounts and 5,000 exchanges.
- The per-client limiter tracks at most 10,000 keys in memory and forgets everything on restart.

## Monitoring

- **Log:** failed sign-ins, wrong or missing access codes, locked accounts, rate limits, refused confirmation, reset and unsubscribe links, and refused cross-site posts are each logged as `security: <event> ip=<address>`. Passwords, emails and codes are never logged. `journalctl -u gifty | grep security:` shows them.
- **Email:** with `GIFTY_ALERT_EMAIL` set, 40 events in ten minutes send a warning (at most one an hour), and a summary of the day's events and busiest addresses goes out when the UTC date turns over, if anything happened. The counts are in memory, so a restart starts them afresh.
- **Banning:** `deploy/fail2ban/` bans an address at the firewall after 10 such events in ten minutes, and after 20 400, 404 or 405 responses in five minutes in Caddy's access log (`/var/log/caddy/access.log`, with invitation codes and unsubscribe links blanked). Bans start at an hour and grow for repeat offenders up to a week. Add the addresses you administer from to `ignoreip` in `deploy/fail2ban/jail.d/gifty.conf`, and see who is banned with `sudo fail2ban-client status gifty`.
- Banning by address does little against an attacker with many addresses; the per-account and access-code limits above are what stop that.

## Known limits

- Anyone who can create an account can try to lock its owner out of sign-in for 15 minutes by failing ten times; resetting the password clears it.
- Someone who signs up with an address they don't own can occupy it. They can never confirm it, but the real owner can't confirm it either until they take the account over with a password reset, which proves the inbox.
- Signup reveals whether an address already has an account (the owner is emailed a notice), so only people past the access gate can probe.
- A flood from people who know the access code or hold an invitation can still occupy the hashing slots. Put edge rate limiting in front if that matters to you.
- The data file holds assignments in the clear: the server operator can read them. `GIFTY_SMTP_HOST=log` writes confirmation and reset links to the server log, so use it only for development.
- A public deployment over plain HTTP would send session cookies unencrypted. Gifty logs a warning when cookies aren't marked Secure.
- Sessions last 30 days and aren't rotated while in use.

## Messages, claims and the reveal

- A conversation belongs to one giver and their recipient. Each person is sent only the conversations they are in, and nothing in them names the other side. The server operator can read them, like assignments.
- A claim on an idea is stored on the idea but only ever sent to givers: as "mine", or "other" for anyone else. The owner is never sent it.
- Keep-apart pairs are sent only to the organiser, and only before the draw. They are not secret from the server operator.
- Assignments are sent to members only after the organiser reveals them, from the day before the exchange date. That is one way. Once revealed, the exchange page names everyone's recipient, so the covered ticket no longer hides it.
- Keep-apart pairs limit the draw, so each person can be in only one. Several pairs on one person would let the organiser (who also takes part) work out or force who they, or anyone, buys for or is bought for by.
- Deleting an exchange deletes ideas meant only for it, so they are never shown more widely than their owner chose. Photos of sorted ideas stop being served to givers.
