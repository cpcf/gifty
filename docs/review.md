# Review

Last reviewed 6 October 2026, after the redesign, email, reminders, scoped wish lists, access gate and deployment.

## Automated checks

All of these passed on the reviewed code:

- `go test -race ./...` with 79.3% statement coverage (`go test -cover ./...`) and `go vet ./...`. Tests cover:
  - access control and privacy between members and outsiders
  - draws over 3–100 people
  - invitations, membership locking, persistence and save rollback
  - wish scoping per exchange, including that givers never learn which other exchanges an idea is in
  - email confirmation, password reset, the outbox and its retries, one-click unsubscribe and header injection
  - reminder schedules, overrides, "I've got my gift" and skipping late reminders
  - the access gate
  - password hashing outside the state lock
  - abuse limits
- Three browser suites in Chrome with Playwright, each running axe WCAG 2 A/AA and 2.1 AA checks on the pages it visits and failing on any uncaught script error:
  - `tests/browser.cjs`: signup, invitations, closing entries with three people, each person drawing a name, the covered ticket and its keyboard reveal (no name in the page while covered), wish lists, sign-out and sign-in, at 1440, 390, 375 and 320px, with no horizontal overflow.
  - `tests/email.cjs`: confirmation, account settings, the draw-a-name email (which must not name a recipient), reminder editing, keyboard focus after saving, wish scoping, unsubscribe and password reset. It runs against a server with `GIFTY_SMTP_HOST=log`.
  - `tests/gate.cjs`: the invite-only page, refusal of direct API signups, wrong and right codes, invited guests bypassing the code, and security headers. It runs against a server with an access code set.
- Impeccable's design detector reports no findings on `web/`.

Automated accessibility checks are evidence, not a certification; no screen-reader testing has been done.

### Running the browser suites

Install `playwright` and `@axe-core/playwright` (outside the repository is fine; point `NODE_PATH` at them). Set `GIFTY_CHROME` to a Chrome binary to use an installed browser, and `GIFTY_SCREENSHOTS` for where screenshots go. Start a separate server for each suite, then:

```sh
GIFTY_ADDR=127.0.0.1:8088 go run . &
node tests/browser.cjs

GIFTY_ADDR=127.0.0.1:8089 GIFTY_SMTP_HOST=log GIFTY_DATA=/tmp/mail.json go run . > /tmp/mail.log 2>&1 &
GIFTY_TEST_URL=http://127.0.0.1:8089 GIFTY_MAIL_LOG=/tmp/mail.log node tests/email.cjs

GIFTY_ADDR=127.0.0.1:8090 GIFTY_ACCESS_CODE='test gate code' GIFTY_DATA=/tmp/gate.json go run . &
GIFTY_TEST_URL=http://127.0.0.1:8090 node tests/gate.cjs
```

Each suite signs up several accounts from one address, so restart its server between repeated runs to reset the 30-attempt rate limit.

## Security review

Reviewed against abuse rather than only correctness. Fixed:

1. Password hashing (about 320 ms on the server) ran inside the global state lock, so repeated sign-in attempts could stall every user. It now runs outside the lock, after the rate limit, at most two at a time.
2. Confirmation and reset emails could be requested repeatedly to flood someone's inbox. They are capped at 5 per person per day and 100 an hour site-wide.
3. Organisers are limited to 20 active exchanges, and the rate limiter tracks at most 10,000 addresses, bounding data-file and memory growth.
4. Added `Permissions-Policy` and `Cross-Origin-Opener-Policy`.

Checked and unchanged:

- Writes need same-origin JSON requests; one-click unsubscribe is protected by its own random token.
- Exchange details, invite codes and other members' email addresses stay within the right audience.
- User content is escaped in the page; email subjects are stripped of line breaks and encoded.
- Tokens are 256-bit; reset and confirmation links expire and work once.
- Gifty listens only on localhost behind Caddy, which supplies HTTPS and HSTS.
- SSH is key-only, automatic security updates are on, and data files are private to the service user.

Accepted for now:

- Signup reveals whether an email already has an account; with the access gate on, only people past it can probe.
- SSH is reachable from any address, though key-only.
- There is no way to delete an account; add one before wider use.

## Deployment

Live at https://gifty.connorfleming.co.uk on AWS Lightsail with Amazon SES; see [deploy/README.md](../deploy/README.md). New accounts need the access code or an invitation link. SES production access was requested on 6 October 2026 and is under review; until it is granted, SES delivers only to verified addresses.

## Boundaries

One process and one data file, for modest groups (up to 100 people per exchange). No exclusion matching and no invitations by email. The server operator can read stored assignments; privacy is enforced between users, not against whoever runs the server.
