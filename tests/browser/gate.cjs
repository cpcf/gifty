const { createAccount } = require("../support/browser.cjs");
const {
  launchBrowser,
  createContext,
  audit: auditPage,
} = require("../support/browser.cjs");
// Access gate against a server started with GIFTY_ACCESS_CODE='test gate code'.
// GIFTY_TEST_URL=http://127.0.0.1:8090 node tests/browser/gate.cjs

const assert = require("assert/strict");
const fs = require("node:fs");
const base = process.env.GIFTY_TEST_URL || "http://127.0.0.1:8090",
  out =
    process.env.GIFTY_SCREENSHOTS ||
    require("node:path").join(require("node:os").tmpdir(), "gifty-browser");
fs.mkdirSync(out, { recursive: true });
const audit = (p) => auditPage(p, 200);
(async () => {
  const b = await launchBrowser();
  const errors = [];
  const p = await (
    await createContext(b, { viewport: { width: 390, height: 844 } })
  ).newPage();
  p.on("pageerror", (e) => errors.push(e.message));
  await p.goto(base);
  await p
    .getByRole("heading", { name: "Gifty is invite-only for now" })
    .waitFor();
  assert.equal(
    await p.getByRole("link", { name: "Create an account" }).count(),
    0,
  );
  await audit(p);
  await p.screenshot({ path: out + "/gate-mobile.png" });
  await p.goto(base + "/#signup");
  await p
    .getByRole("heading", { name: "Gifty is invite-only for now" })
    .waitFor();
  // The API refuses signup without access, whatever the page shows.
  const direct = await p.evaluate(
    async () =>
      (
        await fetch("/api/signup", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: "x",
            email: "x@example.com",
            password: "a long enough password",
          }),
        })
      ).status,
  );
  assert.equal(direct, 403);
  // Signing in needs the code too, in the page and at the API.
  await p.goto(base + "/#login");
  await p
    .getByRole("heading", { name: "Gifty is invite-only for now" })
    .waitFor();
  const directLogin = await p.evaluate(
    async () =>
      (
        await fetch("/api/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            email: "x@example.com",
            password: "a long enough password",
          }),
        })
      ).status,
  );
  assert.equal(directLogin, 403);
  await p.getByLabel("Access code", { exact: true }).fill("wrong code");
  await p.getByRole("button", { name: "Continue" }).click();
  await p.getByText("That code isn’t right.").waitFor();
  await p.getByLabel("Access code", { exact: true }).fill("test gate code");
  await p.getByRole("button", { name: "Continue" }).click();
  await p.getByRole("heading", { name: "Sign in" }).waitFor();
  await p
    .locator("header")
    .getByRole("link", { name: "Create an account" })
    .click();
  await p.getByRole("heading", { name: "Create an account" }).waitFor();
  await createAccount(p, {
    name: "Ana",
    email: "ana" + Date.now() + "@example.com",
    password: "a long enough password",
  });
  await p.getByRole("heading", { name: "Exchanges", exact: true }).waitFor();
  const e = await p.evaluate(async () =>
    (
      await fetch("/api/exchanges", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "Gate test",
          date: "2099-12-20",
          budget: "20",
          currency: "GBP",
          note: "",
        }),
      })
    ).json(),
  );
  // An invited guest goes straight through without the code.
  const g = await (
    await createContext(b, { viewport: { width: 390, height: 844 } })
  ).newPage();
  g.on("pageerror", (e) => errors.push(e.message));
  await g.goto(base + "/#invite/" + e.invite);
  await g.getByRole("link", { name: "Create an account to join" }).click();
  await g.getByRole("heading", { name: "Create an account" }).waitFor();
  await createAccount(g, {
    name: "Ben",
    email: "ben" + Date.now() + "@example.com",
    password: "a long enough password",
  });
  await g.getByRole("heading", { name: "Gate test" }).waitFor();
  const h = await (await fetch(base + "/")).headers;
  assert.equal(h.get("cross-origin-opener-policy"), "same-origin");
  assert(h.get("permissions-policy").includes("camera=()"));
  assert.deepEqual(errors, []);
  console.log("PASS gate");
  await b.close();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
