# Gifty

A gift-exchange app for friends and family: a Go standard-library server with a buildless HTML/CSS/JavaScript interface, one process and one data file.

## Run

Requires Go 1.26.2 or newer.

```sh
go run .
```

Open http://127.0.0.1:8080. Create an account, create an exchange, and send the invitation link to your participants. Three people must join before the organiser can close entries. Closing entries assigns each person exactly one recipient, never themselves. Each person then opens the exchange and draws a name (a short animation; the assignment is already made), and their ticket stays covered until they press and hold it.

For a standalone binary with embedded web assets:

```sh
go build -o gifty .
./gifty
```

## Features

- Accounts with sign-in, sign-out and persistent 30-day sessions.
- Exchanges with a date, budget, currency and shared note.
- Shareable invitation links, with replacement links, joining, leaving and organiser removal until entries close.
- A private, cryptographically random draw when the organiser closes entries. Membership and details are fixed at that point and there is no redraw. Nobody is assigned themselves.
- A cosmetic "Draw a name" step: each person opens the exchange and watches the raffle machine land on a name, although the assignment was already made. Their ticket stays covered, with no name in the page, until they press and hold it (by keyboard or screen reader, activate to show and again to hide).
- Gift ideas with up to five optional photos, link, note and rough price. An idea can be shown in every exchange or only in chosen ones, and the person buying for you only sees ideas meant for their exchange. Photos are resized in the browser, stored in the data file and served from the app's own origin, so no image is ever fetched from elsewhere. The recipient's list stays visible under the covered ticket for shopping, so an idea that mentions a name can give it away.
- Optional email: address confirmation, password reset, a notice when it is time to draw, and reminders before the exchange (see [Email](#email)).
- Account deletion from the account page, and an admin page for the addresses in `GIFTY_ADMINS` to remove accounts and exchanges without touching the data file.
- Archiving, and deletion of archived exchanges by their organiser. Responsive layouts, keyboard-accessible dialogs and inline form errors.

Not included: exclusion rules, gift reservations and invitations by email (links are shared by hand). The server operator can read stored assignments, though the admin page never shows them: privacy is enforced between app users, not against whoever runs the server.

## Configuration

| Setting | Default | Purpose |
| --- | --- | --- |
| `GIFTY_ADDR` | `127.0.0.1:8080` | Listening address. Use `0.0.0.0:8080` when a container or remote proxy needs access |
| `GIFTY_DATA` | `data/gifty.json` | Data file. The server secret is kept beside it in `gifty.key` |
| `GIFTY_BASE_URL` | unset | Public address used in email links, e.g. `https://gifty.example.com` |
| `GIFTY_SECURE_COOKIES` | `true` when `GIFTY_BASE_URL` is https, else `false` | Overrides the cookie `Secure` flag |
| `GIFTY_TRUST_PROXY` | `false` | Take the client address from `X-Forwarded-For` on loopback connections, for a local reverse proxy that appends it (Caddy does). Without it every caller behind the proxy shares one rate limit |
| `GIFTY_ACCESS_CODE` | unset | Makes the instance invite-only: signing up needs this code or an open invitation link, and so does signing in. Invited guests are let past it when they join, and a password reset also lets someone back in |
| `GIFTY_ADMINS` | unset | Comma-separated administrator emails (a confirmed address is required) |
| `GIFTY_SMTP_HOST` | unset | SMTP server; email is off when unset. `log` prints emails to the log instead |
| `GIFTY_SMTP_PORT` | `587` | SMTP port; the server must offer STARTTLS |
| `GIFTY_SMTP_USER`, `GIFTY_SMTP_PASSWORD` | unset | SMTP credentials |
| `GIFTY_MAIL_FROM` | unset | Sender, e.g. `Gifty <noreply@gifty.example.com>` |
| `GIFTY_ALERT_EMAIL` | unset | Address that gets a warning when failed sign-ins and similar events spike, and a daily summary. Needs email on |

## Hosting

Run one server process per data file. The file is written to a synced temporary file and atomically renamed under a process mutex, and a failed save rolls memory back. It holds account details, password hashes, wish lists and assignments, so keep it on a persistent local volume, back it up securely and never serve it. `gifty.key` holds the secret behind access-gate cookies and unsubscribe links. It is kept out of the data file so a copy or backup of the data doesn't include it. Losing it only means people re-enter the access code and old unsubscribe links stop working. Scaling beyond one process needs a shared transactional database first.

For other people to use invitations, deploy behind an HTTPS reverse proxy that preserves the original Host header, set secure cookies, and open Gifty at its public hostname before copying links. A loopback invitation only works on your own computer. [deploy/README.md](deploy/README.md) describes one way to do this.

## Email

Email addresses identify accounts. With email on, Gifty sends a confirmation link at signup, password-reset links, a notice when it is time to draw a name and reminders before the exchange.

- Reminders are any number of days, weeks or calendar months before the date, up to 8. The organiser sets each exchange's default (a week and a day before unless changed). Each person can set their own for every exchange on their account page, or for one exchange on its page, and ticking "I've got my gift" stops reminders for that exchange.
- Exchange emails go only to confirmed addresses, never name a recipient and carry a one-click unsubscribe link.
- Messages are queued in the data file in the same save as the change that caused them, sent in the background and retried with backoff.

Try it locally with `GIFTY_SMTP_HOST=log go run .`, which prints each email, including its links, to the log.

## Security

Passwords use salted PBKDF2-HMAC-SHA256 with 600,000 iterations. Session tokens are random and stored as SHA-256 digests; cookies are HttpOnly and SameSite=Lax. Writes must come from the app's own origin, request bodies are bounded, rendered user content is escaped and a restrictive content security policy is sent. Sign-in, signup and token endpoints are rate limited per client and per account. [docs/security.md](docs/security.md) describes the model and its known limits.

## Verify

```sh
go test -race ./...
go vet ./...
node --check web/app.js
```

The Go tests cover access control, draws over 3–100 participants, invitations, wish ownership and scoping, validation, cross-origin rejection, persistence, storage-failure rollback, email, reminders, the access gate and abuse limits. Browser suites in `tests/` drive Chrome with Playwright and axe; [docs/testing.md](docs/testing.md) says how to run them.

## Design

[docs/surface.md](docs/surface.md) describes the design direction and each screen. The interface uses Public Sans (SIL Open Font License, `web/public-sans-OFL.txt`), served from the binary. No external requests, trackers, raster images or production JavaScript dependencies are loaded.

## Licence

MIT; see [LICENSE](LICENSE). Public Sans keeps its own licence (`web/public-sans-OFL.txt`).
