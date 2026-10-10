const date = (s) =>
  new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  }).format(new Date(s + "T12:00:00Z"));

const money = (e) =>
  new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: e.currency,
    maximumFractionDigits: 2,
  }).format(Number(e.budget));

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

const status = (e) =>
  e.archived
    ? ["archived", "Archived"]
    : e.drawn
      ? ["drawn", "Entries closed"]
      : ["open", "Open to join"];

const kindLabel = (k) =>
  k === "elephant"
    ? "White elephant"
    : k === "group"
      ? "Group gift"
      : "Secret Santa";

const kindTag = (e) =>
  e.kind && e.kind !== "secret"
    ? ` <span class="status">${kindLabel(e.kind)}</span>`
    : "";

const statusTag = (e) => {
  const [c, t] = status(e);
  return `<span class="status ${c}">${t}</span>`;
};

// Tickets: each person gets a colour in turn and a zero-padded number that matches the order of the list.
const hues = ["y", "p", "b", "m", "o"];

// Tickets: each person gets a colour in turn and a zero-padded number that matches the order of the list.
const hue = (i) => hues[i % hues.length];

// Tickets: each person gets a colour in turn and a zero-padded number that matches the order of the list.
const pad = (n) => String(n).padStart(2, "0");

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

const still = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

export {
  pad,
  hue,
  plural,
  kindLabel,
  date,
  money,
  statusTag,
  kindTag,
  still,
  wait,
};
