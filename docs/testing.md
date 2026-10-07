# Testing

## Go

```sh
go test -race ./...
go vet ./...
node --check web/app.js
```

Statement coverage is about 87% (`go test -cover ./...`). The tests cover:

- access control and privacy between members and outsiders
- draws over 3–100 people
- invitations, membership locking, persistence and save rollback
- wish scoping per exchange, including that givers never learn which other exchanges an idea is in
- email confirmation, password reset, the outbox and its retries, one-click unsubscribe and header injection
- reminder schedules, overrides, "I've got my gift" and skipping late reminders
- the access gate, including that sign-in and signup are refused before any hashing without it
- password hashing outside the state lock
- abuse limits: per-client and per-account attempts, the shared budget for wrong access codes, write and token limits, email caps and the limiter's bounds
- origin checks on writes
- security events being logged without credentials, and burst alerts and daily summaries being emailed
- the server key file
- friends: requests and acceptance (own link, bad link, replaced link, duplicates, caps, mutual requests, ending it), birthdays (validation, 29 February, hidden birthdays, the date arithmetic), the ideas friends can and cannot see, marks (never sent to the owner or the person a group gift is for, seen by other friends and by givers, released on sorting, unfriending and deletion, lapsing after the birthday, photos limited to the chosen audience), birthday emails (once each, opt-outs, no hint of gifts) and the birthday calendar file
- white elephant (kind fixed at creation, steals validation, uniform pick order, no recipients, messages and keep apart refused, closed to new people) and group gifts (friends only, never visible to the person they are for, reminders that skip what is already late and never reach or name them, removed with that person's account)
- keeping pairs apart (feasibility, tight rules, 100 people, pruning, one pair per person, organiser-only), anonymous messages (privacy between members, limits, email only when a conversation changes hands), claims and sorted ideas (never shown to the owner, cleaned up with accounts and exchanges), reveal day and the calendar file

## Browser

Six suites drive Chrome with Playwright. Each runs axe (WCAG 2 A/AA and 2.1 AA) on the pages it visits and fails on any uncaught script error.

- `tests/browser.cjs`: signup, invitations, closing entries with three people, each person drawing a name, the covered ticket and its keyboard reveal (no name in the page while covered), wish lists, sign-out and sign-in, at 1440, 390, 375 and 320px with no horizontal overflow.
- `tests/email.cjs`: confirmation, account settings, the draw-a-name email (which must not name a recipient), reminder editing, keyboard focus after saving, wish scoping, unsubscribe and password reset. It needs a server with `GIFTY_SMTP_HOST=log`.
- `tests/admin.cjs`: the admin page (hidden until the admin address is confirmed, and from everyone else), removing an account, and deleting your own account with a wrong and a right password. It needs a server with `GIFTY_SMTP_HOST=log`, `GIFTY_ADMINS=boss@example.com` and a fresh data file.
- `tests/features.cjs`: four people, keep apart, anonymous messages with focus handling, claiming and sorting ideas, the calendar file and reveal day, with axe on each state and a phone-width check. It needs a fresh data file and no mail; the exchange is dated today so the reveal is available.
- `tests/friends.cjs`: four people (and a fifth arriving through a signed-out friend link) becoming friends, a birthday on the calendar (desktop, 390px and 320px, no overflow, day buttons at least 44px), the three idea audiences, marking an idea (focus after marking, other friends see it taken, the owner's page contains no trace), a white elephant from creation to each person drawing a number, and a group gift that the person it is for can't join or see. It needs a fresh data file and no mail.
- `tests/gate.cjs`: the invite-only page in place of both signup and sign-in, refusal of direct API signups and logins, wrong and right codes, invited guests getting past the code, and security headers. It needs a server with an access code set.

Automated accessibility checks are evidence, not a certification; no screen-reader testing has been done.

### Running the browser suites

The suites create accounts and exchanges, so only run them against disposable data. Install `playwright` and `@axe-core/playwright` outside the repository and point `NODE_PATH` at them:

```sh
npm install --prefix /tmp/gifty-browser playwright @axe-core/playwright
export NODE_PATH=/tmp/gifty-browser/node_modules
# Use an installed Chrome, or install Playwright's Chromium and leave this unset.
export GIFTY_CHROME='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
```

Start a separate server for each suite, then run it:

```sh
GIFTY_ADDR=127.0.0.1:8088 GIFTY_DATA=/tmp/gifty-browser/browser.json go run . &
GIFTY_TEST_URL=http://127.0.0.1:8088 node tests/browser.cjs

GIFTY_ADDR=127.0.0.1:8089 GIFTY_SMTP_HOST=log GIFTY_DATA=/tmp/gifty-browser/mail.json go run . > /tmp/gifty-browser/mail.log 2>&1 &
GIFTY_TEST_URL=http://127.0.0.1:8089 GIFTY_MAIL_LOG=/tmp/gifty-browser/mail.log node tests/email.cjs

GIFTY_ADDR=127.0.0.1:8090 GIFTY_ACCESS_CODE='test gate code' GIFTY_DATA=/tmp/gifty-browser/gate.json go run . &
GIFTY_TEST_URL=http://127.0.0.1:8090 node tests/gate.cjs

GIFTY_ADDR=127.0.0.1:8091 GIFTY_SMTP_HOST=log GIFTY_ADMINS=boss@example.com GIFTY_DATA=/tmp/gifty-browser/admin.json go run . > /tmp/gifty-browser/admin.log 2>&1 &
GIFTY_TEST_URL=http://127.0.0.1:8091 GIFTY_MAIL_LOG=/tmp/gifty-browser/admin.log node tests/admin.cjs

GIFTY_ADDR=127.0.0.1:8092 GIFTY_DATA=/tmp/gifty-browser/features.json go run . &
GIFTY_TEST_URL=http://127.0.0.1:8092 node tests/features.cjs

GIFTY_ADDR=127.0.0.1:8093 GIFTY_DATA=/tmp/gifty-browser/friends.json go run . &
GIFTY_TEST_URL=http://127.0.0.1:8093 node tests/friends.cjs
```

Each suite defaults to the port above (8088 to 8093); `GIFTY_TEST_URL` overrides it and `GIFTY_SCREENSHOTS` sets where screenshots go. Each suite signs up several accounts from one address, so restart its server between runs to reset the 30-attempt rate limit.
