// The pure date and wording helpers in web/public/features/birthday-dates.js, run without a browser in several time zones. A birthday must
// never slip a day because of where the visitor is. No server needed: `node tests/unit/birthday-dates.mjs`.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import * as h from "../../web/public/features/birthday-dates.js";
const filename = fileURLToPath(import.meta.url);
const zones = [
  "UTC",
  "Pacific/Pago_Pago",
  "Pacific/Kiritimati",
  "America/Los_Angeles",
  "Europe/London",
  "Asia/Kolkata",
];

if (process.argv[2] !== "child") {
  for (const TZ of zones)
    execFileSync(process.execPath, [filename, "child"], {
      env: { ...process.env, TZ },
      stdio: "inherit",
    });
  console.log(`PASS unit: birthday/date helpers in ${zones.length} time zones`);
  process.exit(0);
}
const day = (y, m, d) => Date.UTC(y, m - 1, d);
const iso = (ts) => new Date(ts).toISOString().slice(0, 10);

// Leap years.
for (const [y, want] of [
  [1900, false],
  [2000, true],
  [2024, true],
  [2026, false],
  [2027, false],
  [2028, true],
  [2100, false],
  [2400, true],
])
  assert.equal(h.leapYear(y), want, `leapYear(${y})`);

// A 29 February is kept on the 28th in years without one, and a day-of-month never rolls into March.
const leap = { month: 2, day: 29 };
for (const [y, want] of [
  [2028, "2028-02-29"],
  [2027, "2027-02-28"],
  [2100, "2100-02-28"],
  [2400, "2400-02-29"],
])
  assert.equal(iso(h.birthdayOn(leap, y)), want);
assert.equal(iso(h.birthdayOn({ month: 12, day: 31 }, 2026)), "2026-12-31");
assert.equal(iso(h.birthdayOn({ month: 1, day: 1 }, 2026)), "2026-01-01");

// The next birthday: today counts, yesterday doesn't, and the year turns over.
const b = { month: 10, day: 25 };
assert.equal(iso(h.birthdayNext(b, day(2026, 10, 8))), "2026-10-25");
assert.equal(iso(h.birthdayNext(b, day(2026, 10, 25))), "2026-10-25");
assert.equal(iso(h.birthdayNext(b, day(2026, 10, 26))), "2027-10-25");
assert.equal(iso(h.birthdayNext(b, day(2026, 12, 31))), "2027-10-25");
assert.equal(
  iso(h.birthdayNext({ month: 1, day: 1 }, day(2026, 12, 31))),
  "2027-01-01",
);
assert.equal(iso(h.birthdayNext(leap, day(2027, 3, 1))), "2028-02-29"); // the next leap day, not the 28th
assert.equal(iso(h.birthdayNext(leap, day(2027, 1, 15))), "2027-02-28");
assert.equal(iso(h.birthdayNext(leap, day(2028, 2, 29))), "2028-02-29");
assert.equal(iso(h.birthdayNext(leap, day(2028, 3, 1))), "2029-02-28");

// Counting days across month, year and leap-day boundaries (and daylight saving, which UTC midnights ignore).
assert.equal(h.daysBetween(day(2026, 10, 8), day(2026, 10, 8)), 0);
assert.equal(h.daysBetween(day(2026, 10, 8), day(2026, 10, 15)), 7);
assert.equal(h.daysBetween(day(2026, 12, 31), day(2027, 1, 1)), 1);
assert.equal(h.daysBetween(day(2027, 2, 28), day(2027, 3, 1)), 1);
assert.equal(h.daysBetween(day(2028, 2, 28), day(2028, 3, 1)), 2);
assert.equal(h.daysBetween(day(2026, 3, 28), day(2026, 3, 30)), 2); // a clocks-change weekend in Europe
assert.equal(h.daysBetween(day(2026, 10, 24), day(2026, 10, 26)), 2);

// Wording.
assert.equal(h.inDays(0), "Today");
assert.equal(h.inDays(1), "Tomorrow");
assert.equal(h.inDays(6), "in 6 days");
assert.equal(h.inDays(30), "in 30 days");
for (const [n, want] of [
  [1, "1st"],
  [2, "2nd"],
  [3, "3rd"],
  [4, "4th"],
  [10, "10th"],
  [11, "11th"],
  [12, "12th"],
  [13, "13th"],
  [14, "14th"],
  [21, "21st"],
  [22, "22nd"],
  [23, "23rd"],
  [100, "100th"],
  [101, "101st"],
  [111, "111th"],
  [112, "112th"],
  [113, "113th"],
  [121, "121st"],
])
  assert.equal(h.ord(n), want, `ord(${n})`);

// Dates are formatted in UTC, so they read the same wherever the visitor is.
assert.match(h.birthdayText({ month: 10, day: 25 }), /25|Oct/);
assert.doesNotMatch(
  h.birthdayText({ month: 10, day: 25 }),
  /\b19\d\d|20\d\d\b/,
);
assert.match(h.birthdayText({ month: 10, day: 25, year: 1990 }), /1990/);
assert.match(h.birthdayText({ month: 1, day: 1 }), /\b1\b|Jan/);
assert.match(h.birthdayText({ month: 12, day: 31 }), /31/);
assert.match(
  h.fmt({ day: "numeric", month: "long" }, day(2026, 3, 1)),
  /\b1\b/,
); // not 28 February or 2 March
assert.equal(h.monthName(0).length > 2, true);
assert.equal(h.monthName(11) !== h.monthName(0), true);
assert.equal(new Set([0, 1, 2, 3, 4, 5, 6].map(h.dayName)).size, 7);
assert.match(h.dayName(0), /^Mon/i); // weeks start on Monday

// Today is the visitor's own calendar day, as a UTC midnight.
const now = new Date(),
  t = h.utcToday(),
  d = new Date(t);
assert.equal(
  d.getUTCHours() +
    d.getUTCMinutes() +
    d.getUTCSeconds() +
    d.getUTCMilliseconds(),
  0,
);
assert.deepEqual(
  [d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()],
  [now.getFullYear(), now.getMonth(), now.getDate()],
);

// Turning N: only with a year, and only for friends.
assert.equal(h.turns({ b: { year: 1990 } }, day(2026, 10, 25)), 36);
assert.equal(h.turns({ b: {} }, day(2026, 10, 25)), 0);
assert.equal(h.turns({ b: { year: 1990 }, me: true }, day(2026, 10, 25)), 0);
