# Testing

## Go

```sh
tests/run-all.sh          # everything below, each browser suite on its own server and data file
tests/run-all.sh quick    # the same without the (slow) race detector
```

`tests/run-all.sh fast` skips the browser suites (Go tests, vet, gofmt, syntax and unit tests only), and `tests/run-all.sh auto` runs `quick` when Playwright and axe are installed and otherwise `fast`, saying so. Set `FAIL_FAST=1` to stop at the first failure.

### Automatic runs

- **Commit hook:** `tests/install-hooks.sh` points this clone at `.githooks/`, whose `pre-commit` runs `tests/run-all.sh auto` (with `FAIL_FAST`) when a commit touches code (`.go`, `web/`, `tests/`, `go.mod`, the hooks or the workflow) and skips docs-only commits. A failure stops the commit. `SKIP_TESTS=1 git commit ...` or `git commit --no-verify` skips it once. It tests the working tree, so unstaged changes are part of the run (it says so). Without Playwright it runs only the non-browser tests and prints a warning saying that. It leaves out the race detector, which takes a few minutes, so that stays for CI.
- **CI:** `.github/workflows/test.yml` runs everything, including `go test -race` and every browser suite in Chromium, on each push and pull request. It installs Playwright and axe itself. It has not been run on GitHub yet, so treat the first run as its own test.
- Browser suites use a fixed locale (`en-GB`) so results do not depend on the machine; set `GIFTY_LOCALE=en-US` to run them in another (the two newer suites are checked in both).

That is, in turn:

```sh
go test -race ./...
go vet ./...
gofmt -l .
node --check web/*.js tests/*.cjs
node tests/unit.cjs
```

Statement coverage is about 88% (`go test -cover ./...`). The tests cover:

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
- the new endpoints refuse everyone without a session and every POST that doesn't come from the page; refuse broken JSON, unknown fields, other content types and unknown paths; and answer unknown people and ideas with the same plain 404
- friend, request and waiting limits; cancelling and declining only your own requests; claiming an idea you hold after it was sorted; friends-only ideas and their photos staying from givers (and a giver's claim ending when an idea is kept from givers, while a friend's survives a change of exchanges)
- group gifts through their life (edit can't change who it is for, removing and leaving, archiving), white elephants through theirs (leaving and removal before and after entries close, a deleted account keeping its place, reminders that stop when the gift is ready)
- ideas: limits, validation, edits that keep what wasn't sent; friends, birthdays and kinds surviving a restart; a data file from before any of this still loading
- keeping pairs apart (feasibility, tight rules, 100 people, pruning, one pair per person, organiser-only), anonymous messages (privacy between members, limits, email only when a conversation changes hands), claims and sorted ideas (never shown to the owner, cleaned up with accounts and exchanges), reveal day and the calendar file

## Browser

Eight suites drive Chrome with Playwright. Each runs axe (WCAG 2 A/AA and 2.1 AA) on the pages it visits and fails on any uncaught script error.

- `tests/browser.cjs`: signup, invitations, closing entries with three people, each person drawing a name, the covered ticket and its keyboard reveal (no name in the page while covered), wish lists, sign-out and sign-in, at 1440, 390, 375 and 320px with no horizontal overflow.
- `tests/email.cjs`: confirmation, account settings, the draw-a-name email (which must not name a recipient), reminder editing, keyboard focus after saving, wish scoping, unsubscribe and password reset. It needs a server with `GIFTY_SMTP_HOST=log`.
- `tests/admin.cjs`: the admin page (hidden until the admin address is confirmed, and from everyone else), removing an account, and deleting your own account with a wrong and a right password. It needs a server with `GIFTY_SMTP_HOST=log`, `GIFTY_ADMINS=boss@example.com` and a fresh data file.
- `tests/features.cjs`: four people, keep apart, anonymous messages with focus handling, claiming and sorting ideas, the calendar file and reveal day, with axe on each state and a phone-width check. It needs a fresh data file and no mail; the exchange is dated today so the reveal is available.
- `tests/friends.cjs`: four people (and a fifth arriving through a signed-out friend link) becoming friends, a birthday on the calendar (desktop, 390px and 320px, no overflow, day buttons at least 44px), the three idea audiences, marking an idea (focus after marking, other friends see it taken, the owner's page contains no trace), a white elephant from creation to each person drawing a number, and a group gift that the person it is for can't join or see. It needs a fresh data file and no mail.
- `tests/friends-more.cjs`: what the first friends suite doesn't reach. Four birthdays on one day (three chips and "+1 more" on desktop, three stripes on a phone, everyone named in words), a 29 February birthday on 28 February in a plain year and 29 February in a leap one, year labels on the month buttons at the end of December, arrow keys across a month end, declining and cancelling requests, putting an idea down (the holder is told, others stop seeing it, putting it back), changing who sees an idea (marks end), removing a friend (cancel, confirm, marks released, the page gone), replacing the friend link, hiding and removing a birthday and the form's mistakes, the birthdays-soon notice, the kind picker with no friends, editing a white elephant's steals, a group gift's invitation preview and leaving it, and every new page and endpoint signed out. It needs a fresh data file and no mail.
- `tests/gate.cjs`: the invite-only page in place of both signup and sign-in, refusal of direct API signups and logins, wrong and right codes, invited guests getting past the code, and security headers. It needs a server with an access code set.

`tests/unit.cjs` runs the pure date and wording helpers in `web/friends.js` (leap years, the next birthday, counting days, ordinals, UTC formatting) in six time zones, including ones a day either side of UTC, with no browser or server. It was checked by breaking the code on purpose and seeing it fail.

Automated accessibility checks are evidence, not a certification; no screen-reader testing has been done.

### What is not tested automatically

- The SMTP sender (a real server over STARTTLS), the mail and alert loops and `main` itself: they are exercised by hand, and the log sender only in the email suite.
- Deployment: `deploy/` scripts, the Caddy config and fail2ban filters.
- How things look. The suites check structure, text, focus, contrast and overflow, not appearance; there are no screenshot comparisons, and screenshots are only saved on request (`GIFTY_SCREENSHOTS`).
- Real phones and browsers other than Chrome; copy-to-clipboard and the native share sheet; photo drag and drop and paste.
- Time passing for real: reminders and birthday emails are driven by calling the scheduler with chosen dates.
- Screen readers, as above.

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
