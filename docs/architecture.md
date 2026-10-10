# Architecture

Gifty remains one standard-library Go executable with embedded public assets and a buildless frontend. `go run .` and `go build -o gifty .` use the same entry point as deployment.

## Backend

`main.go` reads environment settings, constructs `app.Config`, starts the background workers and HTTP server, and handles shutdown. The single application package, `internal/app`, exposes only `Config`, `New`, `Handler` and `StartBackground`; domain types and state are private. `application.go` holds the shared state and construction, while `config.go` applies startup settings.

Files follow responsibilities: HTTP and routes; persistence; accounts and authentication; exchanges, kinds and drawing; wishes and claims; friends and birthdays; messages and calendar output; photos; notification rules and mail delivery; security monitoring. Response projections remain explicit in `publicUser`, `publicWish`, `wishesFor`, `wishesForFriend` and `exchange`. Account and exchange cleanup stays in `removeUser`, `forgetUser` and `removeExchange`.

`serveAPI` performs method, origin and body checks. Authentication preparation rate limits and hashes passwords outside the state lock, with the existing two-slot limit. `serveLocked` acquires the transaction snapshot for writes, dispatches under the state lock, saves state and queued notifications together, restores state if saving fails, and sweeps unused photos only after a successful save. Responses are buffered under the lock and flushed after releasing it. Extracted handlers are called only through this boundary; they do not save independently. Image and calendar handlers retain their own shared protections and buffering.

The JSON field names and persistence format are unchanged. `save` still writes, syncs and atomically renames a temporary file. Notification scheduling and outbox delivery retain their existing save ordering; SMTP runs outside the state lock.

## Frontend

`web/embed.go` embeds only `web/public`. Static serving enumerates that filesystem, including nested modules, and retains security headers, content types, ETags, HEAD/range handling and revalidation. `/` serves the page; `/index.html` stays unavailable. Original stylesheet, font, licence and favicon URLs remain aliases to their moved assets.

`web/public/app.js` composes page entry points and initializes features before loading configuration and the session. Features never import the entry point. `core/` owns the API, session/navigation state, routing and shared in-flight operation guards. The router receives page entry points from composition, so dependencies flow from features into core and UI without a feature/router cycle. Its generation counter still protects rendering, title and focus from stale navigation responses.

`ui/` contains DOM/format helpers, navigation markup, toasts, dialogs, form validation/submission and wish-card rendering. `features/` owns page-specific data and events: accounts, authentication, exchanges and recipient tickets, wishes, friends/calendar, photos and reminders. Birthday/date helpers are pure and imported directly by unit tests. Friend projections expose only deliberate getters for the router title and photo viewer. Photo lists stay with the photo feature. The stylesheet remains one file, in its original cascade order; visual design is unchanged. `web/package.json` marks JavaScript as modules for local tooling and is outside the embedded public tree.

## Adding features and testing

Add domain types, behaviour and named handlers beside the closest existing responsibility in `internal/app`; route through the current protections and persistence boundary. Add frontend state/events to the owning feature and register page entry points in `app.js`. Reuse core/UI helpers explicitly; avoid imports from core or UI into features.

Go behaviour tests live beside implementation, with shared fixtures in `test_helpers_test.go`. `tests/unit` contains pure module tests, `tests/browser` contains scenarios, and `tests/support` contains shared browser helpers. `tests/run-all.sh` remains the verification entry point, including race detection, recursive syntax checks, six time zones and seven browser suites.
