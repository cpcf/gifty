import { state } from "../core/session.js";
import {
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
} from "../ui/format.js";
import { esc, $ } from "../ui/dom.js";
import { ord } from "./birthday-dates.js";
import { wishesHTML } from "../ui/wish-cards.js";
import { api } from "../core/api.js";
import { header } from "../ui/navigation.js";
import { birthdaysSoonHTML, loadFriends, groupSection } from "./friends.js";
import { field, errorBox, onSubmit } from "../ui/forms.js";
import { remindersHTML, refreshReminders } from "./reminders.js";
import { notify, clearToast } from "../ui/toast.js";
import { route } from "../core/router.js";
import { saving } from "../core/operations.js";
import { modal, confirmAction } from "../ui/dialog.js";

// Which drawn exchanges this person has opened on this device, so the recipient is revealed once.
const seen = {
  key: (id) => `gifty-seen-${state.user?.id}-${id}`,
  has(id) {
    try {
      return localStorage.getItem(this.key(id)) === "1";
    } catch {
      return true;
    }
  },
  add(id) {
    try {
      localStorage.setItem(this.key(id), "1");
    } catch {}
  },
};

// The raffle machine: lamps over a window that shows one ticket at a time. At the draw its reel runs through everyone, then a ticket is ejected from the slot.
const machine =
  '<div class="mach" aria-hidden="true"><div class="lamps">' +
  "<i></i>".repeat(9) +
  '</div><div class="win"><div class="strip"></div><div class="shutter"></div></div></div>';

// A folded ticket: the bottom half folds over the top. Covered, it shows no name and no colour, so nothing can be read over a shoulder.
function ticketHTML(
  name,
  num,
  h,
  {
    open = false,
    unfold = false,
    ready = false,
    example = false,
    label = "You drew",
  } = {},
) {
  return `<div class="fold ${open ? h : "n"}${unfold ? " unfold" : ""}"><div class="half top"${open ? "" : ' aria-hidden="true"'}><div class="face"><div class="stub">${open ? pad(num) : "??"}</div><div class="main"><span class="k">${label}</span><span class="name${open ? "" : " cover"}${open && name.length > 18 ? " long" : ""}"${example ? "" : ' id="to-name"'}>${open ? esc(name) : ""}</span></div></div></div><div class="half bottom"><div class="face front">${example ? "" : `<button type="button" class="ready-toggle${ready ? " on" : ""}" name="ready" data-action="ready">${ready ? "Gift ready · Undo" : "Mark gift as ready"}</button>`}</div><div class="face back"></div></div></div>`;
}

function ticketSection(e, { open = false, unfold = false } = {}) {
  const i = Math.max(
    0,
    e.members.findIndex((m) => m.id === e.recipient.id),
  );
  return `<section class="my-ticket${unfold ? " arriving" : ""}" aria-labelledby="to-heading"><h2 class="visually-hidden" id="to-heading" tabindex="-1">${open ? `You’re buying for ${esc(e.recipient.name)}` : "Your ticket"}</h2>${ticketHTML(e.recipient.name, i + 1, hue(i), { open, unfold, ready: e.ready })}<p class="visually-hidden" id="to-announce" aria-live="polite"></p>${open ? "" : '<button type="button" class="secondary full hold" data-action="hold" aria-pressed="false">Hold to see who you drew</button>'}</section>`;
}

// A white elephant has no recipient: your ticket carries your place in the order of picking, and nothing on it is covered.
function pickSection(e, { unfold = false } = {}) {
  const i = Math.max(
      0,
      e.members.findIndex((m) => m.id === state.user.id),
    ),
    n = e.order.indexOf(state.user.id) + 1;
  return `<section class="my-ticket${unfold ? " arriving" : ""}" aria-labelledby="to-heading"><h2 class="visually-hidden" id="to-heading" tabindex="-1">You pick ${ord(n)}</h2>${ticketHTML(ord(n), n, hue(i), { open: true, unfold, ready: e.ready, label: "You pick" })}<p class="hint pick-note">${n === 1 ? "You go first." : `${plural(n - 1, "person goes", "people go")} before you.`} Later numbers see more gifts before they choose.</p></section>`;
}

const revealAfter = (e, o = {}) =>
  e.kind === "elephant"
    ? pickSection(e, o)
    : ticketSection(e, { open: true, ...o }) + wishSection(e);

const wishSection = (e) =>
  `<section id="their-wishes"><h2 tabindex="-1">Their wish list</h2>${e.recipient.wishes?.length ? wishesHTML(e.recipient.wishes, false, !e.archived) : '<p class="muted">No ideas yet. Anything they add will appear here.</p>'}</section>`;

const sizeFold = (f) => {
  const t = f && $(".half.top", f);
  if (t) f.style.setProperty("--h", t.offsetHeight + "px");
};

// Covered tickets open only while held. Pointer: press and hold. Keyboard and screen readers: activate to show, activate again to hide.
let holdOn = false;

// Covered tickets open only while held. Pointer: press and hold. Keyboard and screen readers: activate to show, activate again to hide.
let holdVia = "";

// Covered tickets open only while held. Pointer: press and hold. Keyboard and screen readers: activate to show, activate again to hide.
let holdTimer;

// Covered tickets open only while held. Pointer: press and hold. Keyboard and screen readers: activate to show, activate again to hide.
let ptr = false;

// Covered tickets open only while held. Pointer: press and hold. Keyboard and screen readers: activate to show, activate again to hide.
let revealTimer;

function showTicket(on, via) {
  const f = $(".my-ticket .fold"),
    r = state.current?.recipient;
  if (!f || !r) return;
  const i = Math.max(
      0,
      state.current.members.findIndex((m) => m.id === r.id),
    ),
    n = $("#to-name"),
    btn = $('[data-action="hold"]');
  holdOn = on;
  holdVia = via || "";
  clearTimeout(holdTimer);
  n.textContent = on ? r.name : "";
  n.classList.toggle("cover", !on);
  n.classList.toggle("long", on && r.name.length > 18);
  $(".stub", f).textContent = on ? pad(i + 1) : "??";
  f.className = "fold " + (on ? hue(i) : "n");
  btn.setAttribute("aria-pressed", String(on));
  $(".half.top", f).toggleAttribute("aria-hidden", !on);
  $("#to-announce").textContent = on ? `You’re buying for ${r.name}` : "";
  if (on)
    holdTimer = setTimeout(
      () => showTicket(false),
      via === "key" ? 60000 : 20000,
    );
}

// A ticket shown after the first draw has no hold button, so cover it again when the person looks away.
function recover() {
  clearTimeout(revealTimer);
  const s = $(".my-ticket");
  if (s && !$('[data-action="hold"]') && state.current?.recipient)
    s.outerHTML = ticketSection(state.current);
}

async function exchanges(version) {
  const [items, fr] = await Promise.all([
    api("exchanges"),
    api("friends").catch(() => null),
  ]);
  if (version !== state.routeVersion) return;
  items.sort(
    (a, b) =>
      Number(a.archived) - Number(b.archived) ||
      a.date.localeCompare(b.date) ||
      a.name.localeCompare(b.name),
  );
  header("exchanges");
  $("#main").innerHTML =
    `<div class="page-head"><div><h1>Exchanges</h1></div>${items.length ? '<a href="#new" class="button">New exchange</a>' : ""}</div>${state.mailOn && !state.user.verified ? `<p class="notice">Confirm your email address to get emails about your exchanges. The link was sent to <strong class="wrap">${esc(state.user.email)}</strong>. <a href="#account">Email settings</a></p>` : ""}${birthdaysSoonHTML(fr)}${items.length ? `<table class="exchanges"><thead><tr><th scope="col">Exchange</th><th scope="col">Date</th><th scope="col">Limit</th><th scope="col">People</th><th scope="col">Status</th></tr></thead><tbody>${items.map((e) => `<tr class="${e.archived ? "archived" : ""}"><td><a href="#exchange/${e.id}" class="wrap">${esc(e.name)}</a>${e.owner === state.user.id ? '<span class="role">You’re the organiser</span>' : ""}${e.kind && e.kind !== "secret" ? `<span class="role">${kindLabel(e.kind)}</span>` : ""}${e.archived && e.owner === state.user.id ? `<button class="quiet" data-action="delete-exchange" data-id="${e.id}" data-name="${esc(e.name)}" aria-label="Delete ${esc(e.name)}">Delete</button>` : ""}${e.drawn && !e.archived && !seen.has(e.id) ? `<span class="unseen">Time to draw a ${e.kind === "elephant" ? "number" : "name"}.</span>` : ""}${unreadTalk(e) ? '<span class="unseen">New message.</span>' : ""}</td><td data-label="Date">${date(e.date)}</td><td data-label="Limit">${money(e)}</td><td data-label="People">${e.members.length}</td><td>${statusTag(e)}</td></tr>`).join("")}</tbody></table>` : `<section class="empty sheet"><h2>No exchanges yet</h2><p>Create one and share its invitation link. To join someone else’s exchange, open the link they sent you.</p><a href="#new" class="button">New exchange</a></section>`}`;
}

function exchangeFields(e = {}, friends = null) {
  const other = e.kind && e.kind !== "secret";
  return `${field("name", "Exchange name", "text", e.name || "", 'required maxlength="100" placeholder="e.g. Family Christmas 2026"')}<div class="row">${field("date", "Exchange date", "date", e.date || "", 'required min="' + new Date(Date.now() - new Date().getTimezoneOffset() * 6e4).toISOString().slice(0, 10) + '"')}${field("budget", other ? "Limit per gift" : "Spending limit per person", "number", e.budget || "25", 'required min="1" max="99999.99" step="0.01" inputmode="decimal"')}<div class="field"><label for="currency">Currency</label><select id="currency" name="currency">${["GBP", "USD", "EUR", "CAD", "AUD"].map((c) => `<option ${c === (e.currency || "GBP") ? "selected" : ""}>${c}</option>`).join("")}</select></div></div><div class="field" data-kind="elephant"${e.kind === "elephant" ? "" : " hidden"}><label for="steals">Steals per gift</label><input id="steals" name="steals" type="number" min="0" max="10" inputmode="numeric" value="${e.steals ?? 3}" class="short"><p class="hint">A rule for the room; Gifty doesn’t enforce it.</p></div>${friends ? `<div class="field" data-kind="group" hidden><label for="for">Who is it for?</label><select id="for" name="for">${friends.map((f) => `<option value="${esc(f.id)}">${esc(f.name)}</option>`).join("")}</select><p class="hint">They aren’t told and can’t see this exchange. The invitation link shows its name, so leave their name out if they might open it.</p></div>` : ""}<div class="field"><label for="note">Note for everyone <span class="optional">(optional)</span></label><textarea id="note" name="note" maxlength="2000" placeholder="Where and when you’re meeting, whether to wrap gifts, anything else.">${esc(e.note || "")}</textarea></div>`;
}

async function newExchange(version) {
  const fr = await loadFriends().catch(() => ({ friends: [] }));
  if (version !== state.routeVersion) return;
  header("exchanges");
  const kinds = [
    [
      "secret",
      "Secret Santa",
      "Everyone draws one name and buys for that person. They see that person’s wish list.",
    ],
    [
      "elephant",
      "White elephant",
      "Everyone brings one wrapped gift. The draw gives each person a number for the order of picking. Nobody buys for anyone in particular.",
    ],
    [
      "group",
      "Group gift",
      `Friends give one gift to a friend who doesn’t know. They share that friend’s list and mark what’s taken.${fr.friends.length ? "" : " Add a friend first."}`,
    ],
  ];
  $("#main").innerHTML =
    `<div class="narrow form-page"><a href="#exchanges" class="back">Back to exchanges</a><h1>New exchange</h1><p class="muted" id="kind-note"></p><form class="sheet" data-form="create">${errorBox()}<fieldset class="kinds"><legend>Kind of exchange</legend>${kinds.map(([v, t, h], i) => `<label class="check"><input type="radio" name="kind" value="${v}" ${i === 0 ? "checked" : ""} ${v === "group" && !fr.friends.length ? "disabled" : ""}><span><b>${t}</b><span class="hint">${h}</span></span></label>`).join("")}</fieldset>${exchangeFields({}, fr.friends)}<button class="full">Create exchange</button></form></div>`;
  syncKind($('form[data-form="create"]'));
}

// The create form shows only the fields that belong to the chosen kind.
function syncKind(form) {
  const k = form.elements.kind?.value || "secret";
  form
    .querySelectorAll("[data-kind]")
    .forEach((el) => (el.hidden = el.dataset.kind !== k));
  const l = form.querySelector('label[for="budget"]');
  if (l)
    l.textContent =
      k === "secret" ? "Spending limit per person" : "Limit per gift";
  const n = $("#kind-note");
  if (n)
    n.textContent =
      k === "group"
        ? "You’re included. Friends of the person it’s for can join with the invitation link. There’s no draw: you share their list and mark what each of you is getting."
        : `You’re included as one of the people taking part. You need at least 3 people, including you, to close entries${k === "elephant" ? " and give out pick numbers" : ""}.`;
}

const seenIn = (w, id) =>
  !(w.exchanges || []).length || w.exchanges.includes(id);

// What a white elephant or group gift says in place of the wish-list prompt.
const kindAbout = (e) =>
  e.kind === "elephant"
    ? `<section class="sheet"><h2>Bring a wrapped gift</h2><p>Up to ${money(e)}. Nobody buys for anyone in particular, so wish lists and messages don’t apply here.</p></section>`
    : `<section class="sheet"><h2>It’s a surprise</h2><p class="wrap">${esc(e.for.name)} can’t see this exchange, who is in it or what has been marked. Only friends of ${esc(e.for.name)} can join.</p></section>`;

async function detail(id, version) {
  const e = await api("exchanges/" + id);
  if (version !== state.routeVersion) return;
  state.current = e;
  header("exchanges");
  const owner = e.owner === state.user.id,
    open = !e.drawn && !e.archived,
    organising = owner && open,
    organiser =
      e.members.find((m) => m.id === e.owner)?.name || "The organiser",
    kind = e.kind || "secret",
    ele = kind === "elephant",
    grp = kind === "group",
    needed = grp ? 0 : Math.max(0, 3 - e.members.length),
    ideas = (state.user.wishes || []).filter(
      (w) => !w.status && seenIn(w, e.id),
    ).length;
  fresh = new Set(
    ["toRecipient", "fromGiver"].filter(
      (k) =>
        (e[k] || []).length &&
        !e[k][e[k].length - 1].mine &&
        e[k].length > heard.n(e.id, k),
    ),
  );
  const mine = Math.max(
      0,
      e.members.findIndex((m) => m.id === state.user.id),
    ),
    joining = open && sessionStorage.getItem("giftyJoined") === e.id;
  if (joining) sessionStorage.removeItem("giftyJoined");
  // Before entries close, the machine waits. After it, each person draws a name once per device (a bit of theatre: the names are already assigned), then the ticket stays covered until it is held.
  const dealt = ele ? e.drawn : !!e.recipient,
    to = grp
      ? groupSection(e)
      : !dealt
        ? `<section class="drumbox${joining ? " dropping" : ""}" aria-labelledby="to-heading"><div class="drum-wrap">${joining ? `<span class="chip ${hue(mine)}" aria-hidden="true">${esc(state.user.name)}</span>` : ""}${machine}</div><div><h2 id="to-heading">Entries still open</h2><p class="muted">${owner ? "Close entries once everyone has joined." : `${esc(organiser)} will close entries once everyone has joined.`} Then you each draw a ${ele ? "number" : "name"}.</p></div></section>`
        : !seen.has(e.id)
          ? `<section class="drumbox" aria-labelledby="to-heading"><div class="drum-wrap">${machine}</div><div><h2 id="to-heading" tabindex="-1">Time to draw a ${ele ? "number" : "name"}</h2><p>${owner ? "You" : esc(organiser)} closed entries for this exchange. ${ele ? "Draw a number to find your place in the order of picking." : "Draw a name to see who you’re buying for and their wish list. Only you can see it."}</p><button data-action="draw-name">Draw a ${ele ? "number" : "name"}</button></div></section>`
          : ele
            ? pickSection(e)
            : ticketSection(e) + wishSection(e);
  const side = organising
    ? `<aside><section class="sheet invite"><h2>Invitation link</h2><p>${grp ? `Anyone with this link can join if they’re a friend of ${esc(e.for.name)}.` : "Anyone with this link can join until entries close."}</p><label for="invite-link" class="visually-hidden">Invitation link</label><input id="invite-link" readonly value="${esc(location.origin + "/#invite/" + e.invite)}"><button class="secondary full" data-action="copy-invite">Copy link</button>${navigator.share ? '<button class="secondary full" data-action="share-invite">Share link</button>' : ""}<button class="quiet" data-action="rotate" aria-describedby="rotate-hint">Replace link</button><p class="hint" id="rotate-hint">The old link stops working. People who have joined stay in.</p></section>${ele || grp ? "" : apartHTML(e)}${grp ? kindAbout(e) : `<section class="sheet"><h2>Close entries</h2><p>${ele ? "Each person is given a number for the order of picking." : "Each person is given one recipient, never themselves."} After this, nobody can be removed, the details can’t be edited and it can’t be redone. Then everyone draws a name.</p><button class="full" data-action="draw" ${needed ? 'disabled aria-describedby="draw-needs"' : ""}>Close entries</button>${needed ? `<p class="hint" id="draw-needs">Needs ${plural(needed, "more person", "more people")}.</p>` : ""}</section>`}</aside>`
    : `<aside>${ele || grp ? kindAbout(e) : `${revealHTML(e)}<section class="sheet"><h2>Your wish list</h2><p>${e.drawn ? "The person buying for you" : "Whoever draws your name"} sees ${ideas ? `${plural(ideas, "idea", "ideas")}: your ideas for every exchange and any just for this one.` : "no ideas yet. Add some so they have something to go on."}</p>${ideas ? `<a class="button secondary" href="#wishes/${e.id}">Edit wish list</a>` : `<a class="button" href="#wishes/${e.id}/add">Add idea</a>`}</section>`}</aside>`;
  const main = `<div class="detail-main">${to}${talkHTML(e)}${e.note ? `<section><h2>Note from ${owner ? "you" : esc(organiser)}</h2><p class="note">${esc(e.note)}</p></section>` : ""}${peopleHTML(e)}${state.mailOn && !e.archived ? remindersHTML(e) : ""}</div>`,
    after =
      owner && !e.archived
        ? '<div class="after"><button class="quiet" data-action="archive">Archive exchange</button></div>'
        : owner && e.archived
          ? `<div class="after"><button class="quiet" data-action="delete-exchange" data-id="${e.id}" data-name="${esc(e.name)}">Delete exchange</button></div>`
          : !owner && open
            ? '<div class="after"><button class="quiet" data-action="leave">Leave exchange</button></div>'
            : "";
  // While the exchange is open, the side column holds what each person still has to do, so it comes first on phones.
  $("#main").innerHTML =
    `<a class="back" href="#exchanges">Back to exchanges</a><div class="page-head"><div><h1>${esc(e.name)}</h1>${statusTag(e)}${kindTag(e)}</div>${organising ? '<button class="quiet" data-action="edit-exchange">Edit details</button>' : ""}</div>${e.archived ? '<p class="notice">This exchange is archived. Invitations are closed.</p>' : ""}${revealedNotice(e)}<dl class="facts"><div><dt class="label">Date</dt><dd>${date(e.date)}${e.archived ? "" : `<a class="cal" href="/calendar/${e.id}" download>Add to calendar</a>`}</dd></div><div><dt class="label">${ele ? "Limit per gift" : "Spending limit"}</dt><dd>${money(e)}</dd></div>${ele ? `<div><dt class="label">Steals per gift</dt><dd>${e.steals}</dd></div>` : ""}${grp ? `<div><dt class="label">For</dt><dd class="wrap">${esc(e.for.name)}</dd></div>` : ""}<div><dt class="label">People</dt><dd>${e.members.length}</dd></div><div${!owner && organiser.length > 16 ? ' class="wide"' : ""}><dt class="label">Organiser</dt><dd class="wrap">${owner ? "You" : esc(organiser)}</dd></div></dl><div class="detail${open ? " side-first" : ""}">${open ? side + main : main + side}${after}</div>`;
  document.querySelectorAll(".fold").forEach(sizeFold);
  markHeard(e);
}

// Your own stub in the people list: marking the gift ready tears it off and a small slot under the row swallows it; undoing it ejects the stub and tapes it back on. It plays only here, in the browser of the person who ticked. Everyone else just sees a torn or a whole stub.
// Each time the stub is taped back on, the strip goes somewhere new (a placement). Marking it ready again cuts the tape at the perforation: the half on the stub goes into the machine and the half on the ticket stays as a scrap, so scraps build up. After the last placement the next untick peels them all off in one go and starts again. The count lives in this browser only.
const tapeSpots = 6;

// Your own stub in the people list: marking the gift ready tears it off and a small slot under the row swallows it; undoing it ejects the stub and tapes it back on. It plays only here, in the browser of the person who ticked. Everyone else just sees a torn or a whole stub.
// Each time the stub is taped back on, the strip goes somewhere new (a placement). Marking it ready again cuts the tape at the perforation: the half on the stub goes into the machine and the half on the ticket stays as a scrap, so scraps build up. After the last placement the next untick peels them all off in one go and starts again. The count lives in this browser only.
const taped = {
  mem: {},
  key: (id) => `gifty-taped-${state.user?.id}-${id}`,
  n(id) {
    try {
      return +localStorage.getItem(this.key(id)) || 0;
    } catch {
      return this.mem[id] || 0;
    }
  },
  bump(id) {
    const n = this.n(id) + 1;
    this.mem[id] = n;
    try {
      localStorage.setItem(this.key(id), String(n));
    } catch {}
  },
};

const tapePiece = (i, cls = "") =>
  `<i class="tp p${i}${cls ? " " + cls : ""}" aria-hidden="true"><b></b></i>`;

function tapePieces(id, ready) {
  const n = taped.n(id);
  if (!n) return "";
  const p = (n - 1) % tapeSpots;
  let h = "";
  for (let i = 0; i < p; i++) h += tapePiece(i, "scrap");
  return h + tapePiece(p, ready ? "scrap" : "");
}

async function tabTrick(on, peel = false) {
  const sec = $("#people"),
    li = $(".tk[data-self]", sec),
    stub = li && $(".stub", li);
  if (!stub || still()) return;
  const sr = sec.getBoundingClientRect(),
    r = stub.getBoundingClientRect(),
    w = r.width,
    h = r.height,
    x = r.left - sr.left,
    y = r.top - sr.top,
    n = taped.n(state.current.id),
    spot = (n - 1) % tapeSpots,
    tape = !on && n ? $(".tp:not(.scrap)", li) : null,
    clip = document.createElement("div"),
    slot = document.createElement("div");
  clip.className = "tab-clip";
  clip.setAttribute("aria-hidden", "true");
  Object.assign(clip.style, {
    left: x - 8 + "px",
    top: y - 14 + "px",
    width: w + 16 + "px",
    height: h + 19 + "px",
  });
  const ghost = document.createElement("div");
  ghost.className = "tab-ghost";
  ghost.textContent = stub.textContent.trim();
  Object.assign(ghost.style, {
    width: w + "px",
    height: h + "px",
    background: getComputedStyle(li).getPropertyValue("--c").trim(),
  });
  if (!on) ghost.style.transform = `translate(0,${h + 6}px)`;
  if (on && n) ghost.insertAdjacentHTML("beforeend", tapePiece(spot, "half"));
  clip.append(ghost);
  slot.className = "tab-slot";
  slot.setAttribute("aria-hidden", "true");
  Object.assign(slot.style, {
    left: x - 8 + "px",
    top: y + h + 1 + "px",
    width: w + 16 + "px",
  });
  sec.append(clip, slot);
  let old = [];
  if (!on) {
    li.classList.add("torn");
    if (tape) tape.style.opacity = 0;
    if (peel) {
      li.insertAdjacentHTML(
        "afterbegin",
        Array.from({ length: tapeSpots }, (_, i) => tapePiece(i, "scrap")).join(
          "",
        ),
      );
      old = [...li.querySelectorAll(".tp.scrap")];
    }
  }
  const at = (d) => `translate(0,${d}px)`,
    hold = (o, d, rot = "0deg") => ({
      offset: o,
      transform: `${at(d)} rotate(${rot})`,
    }),
    feed = [
      hold(0, 0),
      { offset: 0.14, transform: "translate(-3px,-7px) rotate(-8deg)" },
      hold(0.26, -9, "-2deg"),
      hold(0.4, h * 0.2),
      hold(0.46, h * 0.2),
      hold(0.62, h * 0.5),
      hold(0.68, h * 0.5),
      hold(0.86, h * 0.8),
      hold(1, h + 6),
    ],
    eject = [
      hold(0, h + 6),
      hold(0.18, h * 0.8),
      hold(0.24, h * 0.8),
      hold(0.42, h * 0.5),
      hold(0.48, h * 0.5),
      hold(0.66, h * 0.2),
      hold(0.72, h * 0.2),
      hold(0.88, -3, "-1.5deg"),
      hold(1, 0),
    ];
  for (const k of [...feed, ...eject]) k.easing = "ease-in-out";
  try {
    await slot.animate(
      [{ transform: "scaleX(0)" }, { transform: "scaleX(1)" }],
      { duration: 180, easing: "ease-out", fill: "both" },
    ).finished;
    await ghost.animate(on ? feed : eject, {
      duration: on ? 1000 : 1100,
      fill: "both",
    }).finished;
    if (on) {
      await slot.animate(
        [
          { transform: "scaleX(1) scaleY(1)" },
          { transform: "scaleX(1) scaleY(1.8)" },
          { transform: "scaleX(1) scaleY(1)" },
        ],
        { duration: 150 },
      ).finished;
      li.animate(
        [
          { transform: "translateX(0)" },
          { transform: "translateX(-3px)" },
          { transform: "translateX(3px)" },
          { transform: "translateX(0)" },
        ],
        { duration: 180 },
      );
    } else {
      li.classList.remove("torn");
      clip.remove();
      if (old.length)
        await Promise.all(
          old.map(
            (el, i) =>
              el.animate(
                [
                  { transform: "none", opacity: 1 },
                  {
                    transform: "translate(2px,-3px) rotate(4deg)",
                    opacity: 1,
                    offset: 0.25,
                  },
                  {
                    transform: "translate(34px,-30px) rotate(38deg)",
                    opacity: 0,
                  },
                ],
                {
                  duration: 520,
                  delay: i * 40,
                  easing: "ease-in",
                  fill: "both",
                },
              ).finished,
          ),
        );
      old.forEach((el) => el.remove());
      if (tape)
        await tape.animate(
          [
            { opacity: 0, transform: "scale(1.8)" },
            { opacity: 1, transform: "scale(.92)", offset: 0.6 },
            { opacity: 1, transform: "scale(1)" },
          ],
          { duration: 260, easing: "ease-out" },
        ).finished;
      if (tape) tape.style.opacity = "";
    }
    await slot.animate(
      [{ transform: "scaleX(1)" }, { transform: "scaleX(0)" }],
      { duration: 200, easing: "ease-in", fill: "both" },
    ).finished;
  } catch {
  } finally {
    if (!on) li.classList.remove("torn");
    if (tape) tape.style.opacity = "";
    old.forEach((el) => el.remove());
    clip.remove();
    slot.remove();
  }
}

// Keep apart: the organiser's private list of pairs who shouldn't draw each other.
function apartHTML(e) {
  if (!e.apart || e.members.length < 2) return "";
  const nm = (id) => esc(e.members.find((m) => m.id === id)?.name || "");
  return `<section class="sheet" id="apart"><h2 tabindex="-1">Keep apart</h2><p>People who shouldn’t draw each other, like couples. Only you can see this list.</p>${
    e.apart.length
      ? `<ul class="apart-list">${e.apart
          .map((p, i) => {
            const [x, y] = [...p].sort(
              (a, b) =>
                e.members.findIndex((m) => m.id === a) -
                e.members.findIndex((m) => m.id === b),
            );
            return `<li><span class="wrap">${nm(x)} and ${nm(y)}</span><button type="button" class="quiet" data-action="apart-remove" data-i="${i}" aria-label="Remove: ${nm(x)} and ${nm(y)}">Remove</button></li>`;
          })
          .join("")}</ul>`
      : ""
  }${freeToPair(e).length >= 2 ? '<button type="button" class="secondary full" data-action="apart-add">Keep two people apart</button>' : '<p class="hint">Everyone else is already in a pair. Each person can be in one.</p>'}</section>`;
}

const freeToPair = (e) =>
  e.members.filter((m) => !e.apart.some((p) => p.includes(m.id)));

// Reveal day: once the date is near the organiser can show everyone who bought for whom.
const revealHTML = (e) =>
  e.canReveal
    ? `<section class="sheet"><h2>Reveal who had whom</h2><p>Everyone in the exchange will see who bought for whom. Do it when the gifts have been given: it can’t be undone.</p><button type="button" class="secondary full" data-action="reveal">Reveal names</button></section>`
    : "";

function revealedNotice(e) {
  if (!e.revealed) return "";
  const nm = (id) => esc(e.members.find((m) => m.id === id)?.name || "");
  const g = e.pairs.find((p) => p.recipient === state.user.id),
    r = e.pairs.find((p) => p.giver === state.user.id);
  return `<p class="notice">The organiser has revealed the names.${g ? ` ${nm(g.giver)} bought for you.` : ""}${r ? ` You bought for ${nm(r.recipient)}.` : ""} Everyone’s is in the list of people.</p>`;
}

// Anonymous messages: a giver can ask the person they buy for a question, and that person answers without learning who asked. The count each person has read is kept in this browser, to flag new messages.
const heard = {
  mem: {},
  key: (id, k) => `gifty-heard-${state.user?.id}-${id}-${k}`,
  n(id, k) {
    try {
      return (
        +localStorage.getItem(this.key(id, k)) || this.mem[this.key(id, k)] || 0
      );
    } catch {
      return this.mem[this.key(id, k)] || 0;
    }
  },
  set(id, k, n) {
    this.mem[this.key(id, k)] = n;
    try {
      localStorage.setItem(this.key(id, k), String(n));
    } catch {}
  },
};

// fresh holds the conversations that had something new when the page opened, so "New" stays until you leave it.
let fresh = new Set();

const markHeard = (e) => {
  for (const k of ["toRecipient", "fromGiver"])
    if (e[k] && (k === "fromGiver" || seen.has(e.id)))
      heard.set(e.id, k, e[k].length);
};

function refreshTalk() {
  const h = talkHTML(state.current),
    t = $("#talk");
  if (t) t.outerHTML = h;
  else $("#their-wishes")?.insertAdjacentHTML("afterend", h);
  markHeard(state.current);
}

const unreadTalk = (e) =>
  ["toRecipient", "fromGiver"].some((k) => {
    const t = e[k] || [];
    return t.length && !t[t.length - 1].mine && t.length > heard.n(e.id, k);
  });

const stamp = (s) =>
  new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "short",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(s));

function threadHTML(e, k) {
  const list = e[k],
    to = k === "toRecipient",
    unread = fresh.has(k),
    them = to ? "Them" : "Your secret giver",
    id = to ? "to" : "from";
  return `<div class="thread" id="thread-${id}"><h3>${to ? "Ask the person you’re buying for" : "Your secret giver"}${unread ? ' <span class="new">New</span>' : ""}</h3><p class="hint">${to ? "They’ll see your question but not who asked." : "Whoever is buying for you can ask you things here. You won’t see who they are."}</p>${list.length ? `<ol class="chat" aria-label="${to ? "Your conversation with them" : "Your conversation with your secret giver"}">${list.map((m) => `<li class="msg${m.mine ? " mine" : ""}"><span class="label">${m.mine ? "You" : them}</span><p>${esc(m.text)}</p><time datetime="${esc(m.at)}">${stamp(m.at)}</time></li>`).join("")}</ol>` : ""}${e.archived ? "" : `<form data-form="message" data-to="${to ? "recipient" : "giver"}" data-key="${k}">${errorBox()}<div class="field"><label for="msg-${id}">${to ? "Your question" : "Your reply"}</label><textarea id="msg-${id}" name="text" required maxlength="500"></textarea></div><button>${to ? "Send question" : "Send reply"}</button></form>`}</div>`;
}

function talkHTML(e) {
  if (!e.drawn) return "";
  const parts = [];
  if (e.toRecipient && seen.has(e.id)) parts.push(threadHTML(e, "toRecipient"));
  if (e.fromGiver) parts.push(threadHTML(e, "fromGiver"));
  return parts.length
    ? `<section id="talk"><h2>Messages</h2>${parts.join("")}</section>`
    : "";
}

// The people list is a strip of tickets. Once entries are closed a ticket whose person has their gift loses its stub. It never says who anyone drew.
const drewHTML = (e, m) => {
  const p = e.pairs?.find((p) => p.giver === m.id);
  return p
    ? `<span class="sub drew">Bought for ${esc(e.members.find((x) => x.id === p.recipient)?.name || "")}</span>`
    : "";
};

function peopleHTML(e) {
  const owner = e.owner === state.user.id,
    open = !e.drawn && !e.archived,
    organising = owner && open,
    needed = e.kind === "group" ? 0 : Math.max(0, 3 - e.members.length),
    ordered = e.kind === "elephant" && e.drawn && seen.has(e.id),
    list = e.members
      .map((m, i) => ({ m, i }))
      .sort((a, b) =>
        ordered ? e.order.indexOf(a.m.id) - e.order.indexOf(b.m.id) : 0,
      ),
    role = (m) =>
      [m.id === state.user.id && "You", m.id === e.owner && "Organiser"]
        .filter(Boolean)
        .join(", "),
    got = e.members.filter((m) => m.ready).length;
  return `<section id="people"><div class="grid-head"><h2>${ordered ? "Pick order" : "People"}</h2><span class="muted num">${e.drawn ? (got === e.members.length ? "Everyone is ready" : `${got} of ${e.members.length} are ready`) : plural(e.members.length, "person", "people")}</span></div><ol class="tickets">${list
    .map(({ m, i }, pos) => {
      const ready = e.drawn && m.ready;
      const self = m.id === state.user.id;
      return `<li class="tk ${hue(i)}${ready ? " torn" : ""}"${self ? " data-self" : ""}><div class="stub">${ready ? `<span>${pad(ordered ? pos + 1 : i + 1)}</span>` : pad(ordered ? pos + 1 : i + 1)}</div><div class="main"><div class="who"><span class="name">${esc(m.name)}</span>${role(m) ? `<span class="sub">${role(m)}</span>` : ""}${drewHTML(e, m)}</div>${organising && m.id !== state.user.id ? `<button class="quiet" data-action="remove-member" data-id="${m.id}" aria-label="Remove ${esc(m.name)}">Remove</button>` : e.drawn ? `<span class="gift">${ready ? "Gift ready" : "Not yet"}</span>` : ""}</div>${self ? tapePieces(e.id, ready) : ""}</li>`;
    })
    .join(
      "",
    )}${open ? Array.from({ length: needed }, (_, i) => `<li class="tk open"><div class="stub">${pad(e.members.length + i + 1)}</div><div class="main"><span class="name">Waiting for someone to join</span></div></li>`).join("") : ""}</ol>${ordered ? '<p class="hint">The order of picking. 1 goes first.</p>' : ""}</section>`;
}

async function invite(code, version) {
  sessionStorage.setItem("giftyInvite", code);
  const e = await api("invite/" + encodeURIComponent(code));
  if (version !== state.routeVersion) return;
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1>${esc(e.name)}</h1><p>${esc(e.organiser)} has invited you to join this ${e.kind === "group" ? "group gift" : e.kind === "elephant" ? "white elephant" : "gift exchange"}. ${e.kind === "group" ? "It’s a gift for one of your friends, so you need to be friends with them to join." : e.kind === "elephant" ? "Once the organiser locks it in, you’ll draw a number for the order of picking. Bring one wrapped gift." : "Once the organiser locks it in, you’ll draw one person to buy for."}</p><dl class="facts"><div><dt class="label">Date</dt><dd>${date(e.date)}</dd></div><div><dt class="label">Spending limit</dt><dd>${money(e)}</dd></div><div><dt class="label">Joined so far</dt><dd>${plural(e.people, "person", "people")}</dd></div></dl>${state.user ? `<form data-form="join">${errorBox()}<input type="hidden" name="code" value="${esc(code)}"><button class="full">Join as ${esc(state.user.name)}</button></form>` : '<div class="buttons"><a class="button" href="#signup">Create an account to join</a><a class="button secondary" href="#login">Sign in</a></div>'}</div>`;
}

const done = {
  "delete-wish": "Idea removed.",
  delete: "Exchange deleted.",
  draw: "Entries closed. Everyone can draw a name now.",
  archive: "Exchange archived.",
  leave: "You’ve left the exchange.",
  remove: "Person removed.",
  rotate: "Invitation link replaced. The old link no longer works.",
  reveal: "Names revealed. Everyone can see who bought for whom.",
};

function initExchanges() {
  onSubmit(
    ["create", "join", "apart", "message", "edit", "confirm"],
    async (form, data) => {
      switch (form.dataset.form) {
        case "create": {
          const e = await api("exchanges", data);
          sessionStorage.setItem("giftyJoined", e.id);
          location.hash = "exchange/" + e.id;
          notify("Exchange created. Share the invitation link to add people.");
          break;
        }
        case "join": {
          const e = await api("join", data);
          sessionStorage.removeItem("giftyInvite");
          sessionStorage.setItem("giftyJoined", e.id);
          location.hash = "exchange/" + e.id;
          notify("You’ve joined the exchange.");
          break;
        }
        case "apart": {
          if (data.a === data.b) throw Error("Choose two different people.");
          await api("exchanges/" + state.current.id + "/apart", {
            pairs: [...state.current.apart, [data.a, data.b]],
          });
          $("#dialog").close();
          await route();
          $("#apart h2")?.focus();
          notify("Saved. Those two won’t draw each other.");
          break;
        }
        case "message": {
          state.current = await api(`exchanges/${state.current.id}/message`, {
            to: form.dataset.to,
            text: data.text,
          });
          const t = $(
            "#thread-" + (form.dataset.to === "recipient" ? "to" : "from"),
          );
          t.outerHTML = threadHTML(state.current, form.dataset.key);
          markHeard(state.current);
          $(
            "#msg-" + (form.dataset.to === "recipient" ? "to" : "from"),
          ).focus();
          notify(
            form.dataset.to === "recipient"
              ? "Sent. They won’t know it was you."
              : "Sent. Your giver won’t learn who you are.",
          );
          return;
        }
        case "edit":
          await api("exchanges/" + state.current.id + "/edit", data);
          $("#dialog").close();
          await route();
          notify("Details saved.");
          break;
        case "confirm": {
          const op = form.dataset.operation,
            id = form.dataset.id;
          if (op === "delete-wish") state.user = await api("wishes/" + id, {});
          else
            await api(
              "exchanges/" +
                (op === "delete" ? id : state.current.id) +
                "/" +
                op,
              op === "remove" ? { id } : {},
            );
          $("#dialog").close();
          if (op === "leave" || op === "delete") {
            if (location.hash === "#exchanges") await route();
            else location.hash = "exchanges";
          } else await route();
          notify(
            op === "draw" && state.current?.kind === "elephant"
              ? "Entries closed. Everyone can draw a number now."
              : done[op] || "Saved.",
          );
          break;
        }
      }
    },
  );

  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "claim": {
          const on = b.dataset.on === "true",
            id = b.dataset.id;
          if (saving.has("claim" + id)) break;
          saving.add("claim" + id);
          try {
            state.current = await api(`exchanges/${state.current.id}/claim`, {
              wish: id,
              claim: on,
            });
            $("#their-wishes").outerHTML = wishSection(state.current);
            (
              $(`#their-wishes [data-action="claim"][data-id="${id}"]`) ||
              $("#their-wishes h2")
            ).focus();
            notify(
              on
                ? "Marked. Anyone else buying for them will see that someone has it."
                : "Released.",
            );
          } catch (err) {
            notify(err.message);
            if (err.status === 409 || err.status === 404) await route();
          } finally {
            saving.delete("claim" + id);
          }
          break;
        }
        case "apart-add": {
          const ms = freeToPair(state.current);
          modal(
            "Keep two people apart",
            `<p>They won’t draw each other. Only you can see who you chose. Each person can be in one pair.</p><form data-form="apart">${errorBox()}<div class="field"><label for="apart-a">First person</label><select id="apart-a" name="a">${ms.map((m) => `<option value="${esc(m.id)}">${esc(m.name)}</option>`).join("")}</select></div><div class="field"><label for="apart-b">Second person</label><select id="apart-b" name="b">${ms.map((m, i) => `<option value="${esc(m.id)}"${i === 1 ? " selected" : ""}>${esc(m.name)}</option>`).join("")}</select></div><button class="full">Keep apart</button></form>`,
          );
          break;
        }
        case "apart-remove": {
          if (saving.has("apart")) break;
          saving.add("apart");
          try {
            await api(`exchanges/${state.current.id}/apart`, {
              pairs: state.current.apart.filter(
                (_, i) => i !== Number(b.dataset.i),
              ),
            });
            await route();
            $("#apart h2")?.focus();
            notify("They can draw each other again.");
          } finally {
            saving.delete("apart");
          }
          break;
        }
        case "reveal":
          confirmAction(
            "Reveal who had whom?",
            "Everyone in the exchange will see who bought for whom. This can’t be undone.",
            "reveal",
          );
          break;
        case "draw-name": {
          const box = b.closest(".drumbox"),
            e = state.current;
          b.disabled = true;
          box.classList.add("spinning");
          if (still()) {
            box.insertAdjacentHTML("afterend", revealAfter(e));
            box.remove();
            clearToast();
            seen.add(e.id);
            if (e.kind === "elephant") $("#people").outerHTML = peopleHTML(e);
            refreshTalk();
            sizeFold($(".fold"));
            $("#to-heading").focus();
            revealTimer = setTimeout(recover, 20000);
            break;
          }
          const ms = e.members,
            ele = e.kind === "elephant",
            ri = ele
              ? e.order.indexOf(state.user.id)
              : Math.max(
                  0,
                  ms.findIndex((m) => m.id === e.recipient.id),
                ),
            others = (
              ele
                ? e.order.map((_, i) => [{ name: ord(i + 1) }, i])
                : ms.map((m, i) => [m, i])
            ).filter(([, i]) => i !== ri),
            rows = [];
          for (let k = 0; k < 6; k++)
            rows.push(...others.slice().sort(() => Math.random() - 0.5));
          rows.push(ele ? [{ name: ord(ri + 1) }, ri] : [e.recipient, ri]);
          const strip = $(".strip", box),
            win = $(".win", box);
          strip.innerHTML = rows
            .map(
              ([m, i]) =>
                `<div class="rr ${hue(i)}"><b>${pad(i + 1)}</b><span>${esc(m.name)}</span></div>`,
            )
            .join("");
          const end = -(rows.length - 1) * 64;
          await wait(700);
          if (!box.isConnected) return;
          await strip.animate(
            [
              { transform: "translateY(0)" },
              { transform: `translateY(${end - 10}px)`, offset: 0.93 },
              { transform: `translateY(${end}px)` },
            ],
            {
              duration: 3800,
              easing: "cubic-bezier(.22,.6,.12,1)",
              fill: "both",
            },
          ).finished;
          if (!box.isConnected) return;
          box.classList.replace("spinning", "won");
          win.animate(
            [
              { transform: "scale(1)" },
              { transform: "scale(1.05)" },
              { transform: "scale(1)" },
            ],
            { duration: 300 },
          );
          await wait(900);
          if (!box.isConnected) return;
          clearToast();
          seen.add(e.id);
          box.classList.add("ejecting");
          box.removeAttribute("aria-labelledby");
          $("#to-heading", box).removeAttribute("id");
          box.insertAdjacentHTML("afterend", revealAfter(e, { unfold: true }));
          if (e.kind === "elephant") $("#people").outerHTML = peopleHTML(e);
          refreshTalk();
          const f = $(".my-ticket .fold");
          sizeFold(f);
          f.style.setProperty("--d", "1.6s");
          $("#to-heading").focus({ preventScroll: true });
          await wait(3300);
          if (!box.isConnected) return;
          const h = box.offsetHeight;
          await box.animate(
            [
              { height: h + "px", opacity: 1, marginBottom: "0px" },
              {
                height: "0px",
                opacity: 0,
                marginBottom: "-32px",
                paddingTop: "0px",
                paddingBottom: "0px",
                borderWidth: "0px",
              },
            ],
            { duration: 450, easing: "ease-in-out" },
          ).finished;
          box.remove();
          revealTimer = setTimeout(recover, 20000);
          break;
        }
        case "ready": {
          if (saving.has("ready")) break;
          saving.add("ready");
          try {
            const on = !b.classList.contains("on"),
              peel =
                !on &&
                taped.n(state.current.id) > 0 &&
                taped.n(state.current.id) % tapeSpots === 0;
            state.current = await api(`exchanges/${state.current.id}/ready`, {
              ready: on,
            });
            if (!on) taped.bump(state.current.id);
            b.classList.toggle("on", on);
            b.textContent = on ? "Gift ready · Undo" : "Mark gift as ready";
            refreshReminders();
            $("#people").outerHTML = peopleHTML(state.current);
            tabTrick(on, peel);
            notify(
              on
                ? "Marked as ready. No more reminders for this exchange."
                : "Reminders for this exchange are back on.",
            );
          } finally {
            saving.delete("ready");
          }
          break;
        }
        case "hold":
          if (ptr) {
            ptr = false;
            break;
          }
          showTicket(!holdOn, "key");
          break;
        case "share-invite":
          try {
            await navigator.share({
              title: state.current.name,
              text: `Join ${state.current.name} on Gifty.`,
              url: $("#invite-link").value,
            });
          } catch (err) {
            if (err.name !== "AbortError")
              notify("Couldn’t open sharing. Use Copy link instead.");
          }
          break;
        case "copy-invite":
          try {
            await navigator.clipboard.writeText($("#invite-link").value);
            notify("Link copied.");
          } catch {
            $("#invite-link").select();
            notify(
              "Couldn’t copy automatically. The link is selected, so copy it from there.",
            );
          }
          break;
        case "draw": {
          const ms = state.current.members,
            n = ms.length,
            ele = state.current.kind === "elephant",
            what = ele ? "number" : "name";
          modal(
            "Close entries?",
            `<p>${ele ? "Pick numbers are given out" : "Names are assigned"} as soon as you confirm. After this, nobody can be removed, the details can’t be edited and it can’t be redone. ${state.mailOn ? `Everyone with a confirmed email address gets a message to draw a ${what}.` : `Tell everyone to open the exchange and draw a ${what}.`}</p>${state.current.apart?.length ? `<p>${plural(state.current.apart.length, "pair", "pairs")} kept apart will not draw each other.</p>` : ""}<h3>${plural(n, "person", "people")} taking part</h3><ol class="draw-list">${ms
              .slice(0, 8)
              .map((m) => `<li>${esc(m.name)}</li>`)
              .join(
                "",
              )}</ol>${n > 8 ? `<p class="muted">and ${n - 8} more</p>` : ""}<form data-form="confirm" data-operation="draw" data-id="">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Close entries for ${n} people</button></div></form>`,
          );
          break;
        }
        case "archive":
          confirmAction(
            "Archive this exchange?",
            "Archiving closes invitations. Everyone can still see the details and their recipient. This can’t be undone.",
            "archive",
          );
          break;
        case "delete-exchange":
          confirmAction(
            `Delete ${b.dataset.name || state.current.name}?`,
            "It will be removed for everyone in it, with the assignments and any ideas just for it. This can’t be undone.",
            "delete",
            b.dataset.id,
          );
          break;
        case "leave":
          confirmAction(
            "Leave this exchange?",
            "You’ll be removed from the list of people. You can rejoin with the invitation link until entries close.",
            "leave",
          );
          break;
        case "remove-member":
          confirmAction(
            `Remove ${state.current.members.find((m) => m.id === b.dataset.id)?.name || "this person"}?`,
            "They’ll be removed from the list of people. They can rejoin with the invitation link until entries close.",
            "remove",
            b.dataset.id,
          );
          break;
        case "rotate":
          confirmAction(
            "Replace the invitation link?",
            "The current link will stop working. People who have already joined stay in the exchange.",
            "rotate",
          );
          break;
        case "edit-exchange":
          modal(
            "Edit details",
            `<form data-form="edit">${errorBox()}${exchangeFields(state.current)}<button class="full">Save changes</button></form>`,
          );
          break;
      }
    } catch (err) {
      notify(err.message);
    }
  });

  document.addEventListener("change", (event) => {
    const box = event.target.closest(
      'input[type="checkbox"],input[type="radio"]',
    );
    if (!box) return;
    if (box.name === "kind") {
      syncKind(box.form);
      return;
    }
  });

  document.addEventListener("pointerdown", (e) => {
    if (e.button === 0 && e.target.closest('[data-action="hold"]')) {
      ptr = true;
      showTicket(true, "pointer");
    }
  });

  ["pointerup", "pointercancel"].forEach((t) =>
    document.addEventListener(t, () => {
      if (holdOn && holdVia === "pointer") showTicket(false);
    }),
  );

  document.addEventListener("contextmenu", (e) => {
    if (e.target.closest('[data-action="hold"]')) e.preventDefault();
  });

  window.addEventListener("blur", () => {
    if (holdOn) showTicket(false);
    recover();
  });

  window.addEventListener("pageshow", (e) => {
    if (e.persisted) recover();
  });

  // A page left open in the background can belong to a session that has since changed (signed out or into another account in another tab), so check when the tab comes back.
  document.addEventListener("visibilitychange", async () => {
    if (document.hidden) {
      if (holdOn) showTicket(false);
      recover();
      return;
    }
    if (!state.user) return;
    try {
      const me = await api("me");
      if (me.id !== state.user.id || /^#exchange\//.test(location.hash)) {
        state.user = me;
        await route();
      }
    } catch (err) {
      if (err.status === 401) {
        state.user = null;
        state.current = null;
        location.hash = "login";
        await route();
      }
    }
  });
}

export { invite, newExchange, detail, exchanges, initExchanges };
