// Dates are handled as UTC midnights of the visitor's own calendar day, so a birthday never slips a day with the time zone.
const utcToday = () => {
  const n = new Date();
  return Date.UTC(n.getFullYear(), n.getMonth(), n.getDate());
};

const leapYear = (y) => y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0);

const fmt = (opts, ts) =>
  new Intl.DateTimeFormat(undefined, { timeZone: "UTC", ...opts }).format(
    new Date(ts),
  );

const monthName = (m) => fmt({ month: "long" }, Date.UTC(2000, m, 1));

// 1 Jan 2024 was a Monday.
const dayName = (i) => fmt({ weekday: "short" }, Date.UTC(2024, 0, 1 + i));

const daysBetween = (a, b) => Math.round((b - a) / 864e5);

const inDays = (n) =>
  n === 0 ? "Today" : n === 1 ? "Tomorrow" : `in ${n} days`;

const ord = (n) => {
  const s = ["th", "st", "nd", "rd"],
    v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
};

// A 29 February is kept on the 28th in years without one.
function birthdayOn(b, y) {
  return Date.UTC(
    y,
    b.month - 1,
    b.month === 2 && b.day === 29 && !leapYear(y) ? 28 : b.day,
  );
}

function birthdayNext(b, today) {
  const y = new Date(today).getUTCFullYear(),
    d = birthdayOn(b, y);
  return d >= today ? d : birthdayOn(b, y + 1);
}

const birthdayText = (b) =>
  fmt({ day: "numeric", month: "long" }, Date.UTC(2000, b.month - 1, b.day)) +
  (b.year ? " " + b.year : "");

const turns = (e, ts) =>
  e.b.year && !e.me ? new Date(ts).getUTCFullYear() - e.b.year : 0;

export {
  leapYear,
  ord,
  birthdayOn,
  utcToday,
  fmt,
  monthName,
  dayName,
  birthdayNext,
  daysBetween,
  turns,
  inDays,
  birthdayText,
};
