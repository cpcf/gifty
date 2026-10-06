# Gifty

A working gift-exchange app with a Go standard-library server and a buildless HTML/CSS/JavaScript interface.

## Run

Requires Go 1.26.2 or newer.

```sh
go run .
```

Open http://127.0.0.1:8080. Create an account, create an exchange, and send the invitation link to your participants. Three people must join before the organiser can draw names. Each person gets exactly one recipient, never themselves.

For a standalone binary with embedded web assets:

```sh
go build -o gifty .
./gifty
```

## What works

- Account creation, sign-in, sign-out and persistent 30-day sessions.
- Exchanges with a date, budget, currency and shared note.
- Shareable invitation links, replacement links, joining, leaving and organiser removal before the draw.
- Private, cryptographically random draws. Membership and details lock after drawing; there is no redraw button.
- Personal gift suggestions with optional product links and notes; create, edit and remove ideas.
- Recipient wish lists that stay current as ideas change.
- Archiving, responsive layouts, keyboard-accessible dialogs and inline form errors.

## Storage and hosting

| Setting | Default | Purpose |
| --- | --- | --- |
| `GIFTY_ADDR` | `127.0.0.1:8080` | Listening address |
| `GIFTY_DATA` | `data/gifty.json` | Persistent data file |
| `GIFTY_SECURE_COOKIES` | `false` | Set to `true` behind HTTPS |

Run one server process per data file. Data is protected by a process mutex and written with a synced temporary file and atomic rename. Failed saves roll back memory. Keep the data file on a persistent local volume and back it up securely. It contains account details, password hashes, wish lists and assignments. It must never be served as a static asset. Horizontal scaling needs a shared transactional database first.

For other people to use invitations, deploy the binary behind an HTTPS reverse proxy, preserve the original Host header, set secure cookies, and open Gifty at its public hostname before copying links. A loopback invitation only works on your own computer. Set `GIFTY_ADDR=0.0.0.0:8080` when a container or remote proxy needs access. No public deployment is configured in this repository.

Passwords use salted PBKDF2-HMAC-SHA256 with 600,000 iterations. Session tokens are random and stored as SHA-256 digests. Cookies are HttpOnly and SameSite=Lax. The server checks write origins, limits authentication attempts, bounds request bodies, escapes rendered user content and sends a restrictive content security policy. The authentication limiter uses the connection IP; a reverse proxy shares that limit unless deployed with suitable edge rate limiting and a trusted client-IP design.

Email addresses identify accounts; this version does not send emails, verify email ownership or provide password recovery. Invitations are shared manually. Exchange exclusions, scheduled reminders and gift reservations are outside this version. The administrator can read stored assignments; privacy is enforced between app users, not against the server operator.

## Verify

```sh
go test -race ./...
go vet ./...
node --check web/app.js
```

Tests cover access control, draws over 3–100 participants, invitation lifecycle, wish ownership, validation, cross-origin rejection, restart persistence, logout and storage-failure rollback. Browser verification is described in [docs/review.md](docs/review.md).

## Design

[PRODUCT.md](PRODUCT.md) records product decisions. [DESIGN.md](DESIGN.md) documents the implemented interface, guided by [Impeccable](https://github.com/pbakaus/impeccable). No third-party fonts, trackers, raster images or production JavaScript dependencies are loaded.

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
