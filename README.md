# Gifty

A working gift-exchange app with a Go standard-library server and a buildless HTML/CSS/JavaScript interface.

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

## What works

- Account creation, sign-in, sign-out and persistent 30-day sessions.
- Exchanges with a date, budget, currency and shared note.
- Shareable invitation links, replacement links, joining, leaving and organiser removal before entries close.
- Private, cryptographically random assignment when the organiser closes entries. Membership and details are fixed at that point; there is no redraw button.
- Each person draws a name in the app (a short, cosmetic animation: the assignment is already made). Their ticket is covered by default, with no name in the page, until they press and hold it (keyboard and screen readers: activate to show, again to hide). The recipient's wish list stays visible under the covered ticket so it can be used while shopping, so it can show a name if an idea mentions one.
- Personal gift suggestions with optional product links and notes; create, edit and remove ideas.
- Recipient wish lists that stay current as ideas change.
- Archiving, responsive layouts, keyboard-accessible dialogs and inline form errors.

## Storage and hosting

| Setting | Default | Purpose |
| --- | --- | --- |
| `GIFTY_ADDR` | `127.0.0.1:8080` | Listening address |
| `GIFTY_DATA` | `data/gifty.json` | Persistent data file |
| `GIFTY_SECURE_COOKIES` | `false` | Set to `true` behind HTTPS |
| `GIFTY_SMTP_HOST` | unset | SMTP server; email is off when unset. `log` prints emails to the log instead |
| `GIFTY_SMTP_PORT` | `587` | SMTP port; the server must offer STARTTLS |
| `GIFTY_SMTP_USER`, `GIFTY_SMTP_PASSWORD` | unset | SMTP credentials |
| `GIFTY_MAIL_FROM` | unset | Sender, e.g. `Gifty <noreply@gifty.example.com>` |
| `GIFTY_BASE_URL` | unset | Public address used in email links, e.g. `https://gifty.example.com` |
| `GIFTY_ACCESS_CODE` | unset | When set, new accounts need this code or an open invitation link; existing accounts can always sign in |

Run one server process per data file. Data is protected by a process mutex and written with a synced temporary file and atomic rename. Failed saves roll back memory. Keep the data file on a persistent local volume and back it up securely. It contains account details, password hashes, wish lists and assignments. It must never be served as a static asset. Horizontal scaling needs a shared transactional database first.

For other people to use invitations, deploy the binary behind an HTTPS reverse proxy, preserve the original Host header, set secure cookies, and open Gifty at its public hostname before copying links. A loopback invitation only works on your own computer. Set `GIFTY_ADDR=0.0.0.0:8080` when a container or remote proxy needs access. No public deployment is configured in this repository.

Passwords use salted PBKDF2-HMAC-SHA256 with 600,000 iterations. Session tokens are random and stored as SHA-256 digests. Cookies are HttpOnly and SameSite=Lax. The server checks write origins, limits authentication attempts, bounds request bodies, escapes rendered user content and sends a restrictive content security policy. The authentication limiter uses the connection IP; a reverse proxy shares that limit unless deployed with suitable edge rate limiting and a trusted client-IP design.

Email addresses identify accounts. With email on, Gifty sends a confirmation link at signup, password-reset links, a notice when it is time to draw a name and reminders before the exchange. Reminders are any number of days, weeks or calendar months before the date (up to 8). The organiser sets each exchange’s default (a week and the day before unless changed); each person can set their own for every exchange on their account page or for one exchange on its page, and ticking “I’ve got my gift” stops reminders for that exchange. Wish-list ideas can be shown in every exchange or only in chosen ones; givers only see ideas meant for their exchange. Exchange emails go only to confirmed addresses, never name a recipient and carry a one-click unsubscribe link. Messages are queued in the data file in the same save as the change that caused them, sent in the background and retried with backoff. Try it locally with `GIFTY_SMTP_HOST=log go run .`. Invitations are still shared manually. Exchange exclusions and gift reservations are outside this version. The administrator can read stored assignments; privacy is enforced between app users, not against the server operator.

## Verify

```sh
go test -race ./...
go vet ./...
node --check web/app.js
```

Go tests cover access control, draws over 3–100 participants, invitations, wish ownership and scoping, validation, cross-origin rejection, persistence, storage-failure rollback, email, reminders, the access gate and abuse limits. Browser suites in `tests/` drive Chrome with Playwright and axe; how to run them and what they cover is in [docs/review.md](docs/review.md).

## Design

[PRODUCT.md](PRODUCT.md) records product decisions. [DESIGN.md](DESIGN.md) documents the implemented interface, guided by [Impeccable](https://github.com/pbakaus/impeccable). The interface uses Public Sans (SIL Open Font License, `web/public-sans-OFL.txt`), served from the binary. No external requests, trackers, raster images or production JavaScript dependencies are loaded.

The optional browser test uses Playwright and axe-core, separate from the Go app:

```sh
npm install --prefix /tmp/gifty-browser playwright @axe-core/playwright
# In a separate terminal, use a disposable data file:
GIFTY_ADDR=127.0.0.1:8088 GIFTY_DATA=/tmp/gifty-browser/test-data.json go run .
# Use installed Chrome, or install Playwright Chromium and omit GIFTY_CHROME.
NODE_PATH=/tmp/gifty-browser/node_modules \
GIFTY_CHROME='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' \
node tests/browser.cjs
```

`GIFTY_TEST_URL` overrides the test server URL. `GIFTY_SCREENSHOTS` sets the output directory. The browser test creates accounts and exchanges; only run it against disposable data.
