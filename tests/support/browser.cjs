const { chromium } = require("playwright");
const AxeBuilder = require("@axe-core/playwright").default;
const assert = require("node:assert/strict");

function launchBrowser() {
  return chromium.launch({
    ...(process.env.GIFTY_CHROME
      ? { executablePath: process.env.GIFTY_CHROME }
      : {}),
    headless: true,
  });
}

function createContext(browser, options = {}) {
  return browser.newContext({
    locale: process.env.GIFTY_LOCALE || "en-GB",
    ...options,
  });
}

async function openPage(browser, errors, options = {}) {
  const context = await createContext(browser, options);
  const page = await context.newPage();
  page.on("pageerror", (error) => errors.push(error.message));
  return page;
}

async function audit(page, settle = 250) {
  await page.waitForTimeout(settle);
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  assert.deepEqual(
    result.violations.map((v) => ({
      id: v.id,
      targets: v.nodes.map((n) => n.target),
    })),
    [],
  );
}

// The scenario still chooses the page, navigation and expected result. This helper only fills the signup form.
async function createAccount(page, { name, email, password }) {
  await page.getByLabel("Your name").fill(name);
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page
    .getByRole("button", { name: "Create an account", exact: true })
    .click();
}

module.exports = {
  launchBrowser,
  createContext,
  openPage,
  audit,
  createAccount,
};
