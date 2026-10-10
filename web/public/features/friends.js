import { api } from "../core/api.js";
import { state } from "../core/session.js";
import {
  utcToday,
  birthdayOn,
  monthName,
  fmt,
  dayName,
  birthdayNext,
  daysBetween,
  turns,
  inDays,
  birthdayText,
} from "./birthday-dates.js";
import { header } from "../ui/navigation.js";
import { $, esc } from "../ui/dom.js";
import { hue, plural } from "../ui/format.js";
import { errorBox } from "../ui/forms.js";
import { wishesHTML } from "../ui/wish-cards.js";
import { notify } from "../ui/toast.js";
import { route } from "../core/router.js";
import { saving } from "../core/operations.js";
import { modal } from "../ui/dialog.js";

// Friends, birthdays and calendar data stay local to this feature.
let friendsData = null;

let cal = null;

let shownWishes = [];

let friendPageData = null;

async function loadFriends() {
  friendsData = await api("friends");
  if (!friendsData.code) friendsData = await api("friends/rotate", {});
  return friendsData;
}

// Everyone with a birthday to show: friends (each keeps the paper colour for their place in the list) and you.
function birthdayEntries() {
  const out = friendsData.friends
    .map((f, i) => f.birthday && { id: f.id, name: f.name, b: f.birthday, i })
    .filter(Boolean);
  if (state.user.birthday)
    out.push({ me: true, name: "You", b: state.user.birthday });
  return out;
}

const friendsViews = (cur) =>
  `<nav class="views" aria-label="Friends views"><a href="#friends"${cur === "calendar" ? ' aria-current="page"' : ""}>Calendar</a><a href="#friends/people"${cur === "people" ? ' aria-current="page"' : ""}>People</a></nav>`;

const friendsHead = (cur) =>
  `<div class="page-head"><div><h1>Friends</h1></div>${cur === "calendar" ? '<a class="button secondary" href="#friends/people">Share friend link</a>' : ""}</div>${friendsViews(cur)}`;

async function friendsCalendar(version) {
  await loadFriends();
  if (version !== state.routeVersion) return;
  const t = utcToday(),
    d = new Date(t);
  cal = { y: d.getUTCFullYear(), m: d.getUTCMonth(), sel: t };
  header("friends");
  $("#main").innerHTML =
    friendsHead("calendar") +
    '<div class="detail"><div class="detail-main" id="cal-main"></div><aside id="cal-side"></aside></div>';
  paintCal();
}

function paintCal(focus) {
  const today = utcToday(),
    { y, m } = cal,
    first = Date.UTC(y, m, 1),
    lead = (new Date(first).getUTCDay() + 6) % 7,
    days = new Date(Date.UTC(y, m + 1, 0)).getUTCDate(),
    rows = Math.ceil((lead + days) / 7),
    entries = birthdayEntries();
  const on = (ts) =>
      entries.filter(
        (e) => birthdayOn(e.b, new Date(ts).getUTCFullYear()) === ts,
      ),
    sd = cal.sel && new Date(cal.sel),
    stop =
      sd && sd.getUTCFullYear() === y && sd.getUTCMonth() === m
        ? cal.sel
        : first,
    step = (n) => {
      const d = new Date(Date.UTC(y, m + n, 1));
      return (
        monthName(d.getUTCMonth()) +
        (d.getUTCFullYear() !== y ? " " + d.getUTCFullYear() : "")
      );
    };
  const sel = cal.sel,
    here = sel ? on(sel) : [];
  let cells = "";
  for (let i = 0; i < rows * 7; i++) {
    const ts = first + (i - lead) * 864e5,
      dt = new Date(ts),
      out = dt.getUTCMonth() !== m,
      list = out ? [] : on(ts),
      names = list.map((e) => e.name).join(", ");
    cells += `<button type="button" class="day${out ? " out" : ""}${ts === today ? " today" : ""}" data-action="cal-day" data-ts="${ts}" aria-pressed="${ts === cal.sel}" aria-label="${fmt({ day: "numeric", month: "long" }, ts)}${ts === today ? ", today" : ""}${names ? ": " + esc(names) : ""}" tabindex="${ts === stop ? 0 : -1}"><span class="n" aria-hidden="true">${dt.getUTCDate()}</span>${list
      .slice(0, 3)
      .map(
        (e) =>
          `<span class="bd ${e.me ? "me" : hue(e.i)}" aria-hidden="true">${esc(e.name)}</span>`,
      )
      .join(
        "",
      )}${list.length > 3 ? `<span class="more" aria-hidden="true">+${list.length - 3} more</span>` : ""}</button>`;
  }
  $("#cal-main").innerHTML =
    `<div class="cal-bar"><button type="button" class="secondary prev" data-action="cal-step" data-by="-1">${step(-1)}</button><h2 id="cal-title">${monthName(m)} ${y}</h2><button type="button" class="secondary next" data-action="cal-step" data-by="1">${step(1)}</button></div>${y === new Date(today).getUTCFullYear() && m === new Date(today).getUTCMonth() ? "" : '<div class="cal-today"><button type="button" class="quiet" data-action="cal-today">Back to this month</button></div>'}<p class="day-line" aria-live="polite">${sel ? fmt({ weekday: "long", day: "numeric", month: "long" }, sel) + (here.length ? ": " + esc(here.map((e) => e.name).join(", ")) : ". No birthdays.") : "Choose a day."}</p><div class="month" role="group" aria-labelledby="cal-title"><div class="cal-dow" aria-hidden="true">${[0, 1, 2, 3, 4, 5, 6].map((i) => `<span class="label">${dayName(i)}</span>`).join("")}</div><div class="cal-grid">${cells}</div></div><div class="cal-foot"><p class="hint">${friendsData.friends.length ? "Your friends’ birthdays, and yours. Choose a day to see who it is, or move between days with the arrow keys." : "Your birthday, and your friends’ once they’ve accepted."}</p><a class="button quiet" href="/calendar/birthdays" download>Add friends’ birthdays to your calendar</a><p class="hint">It’s a file that repeats every year.</p></div>`;
  const soon = entries
    .filter((e) => !e.me)
    .map((e) => ({ ...e, at: birthdayNext(e.b, today) }))
    .filter((e) => daysBetween(today, e.at) <= 30)
    .sort((a, b) => a.at - b.at || a.name.localeCompare(b.name));
  $("#cal-side").innerHTML =
    `<section class="sheet panel" id="day-panel"><h2>${sel ? fmt({ weekday: "long", day: "numeric", month: "long" }, sel) : "Birthdays"}</h2>${sel ? (here.length ? `<ul class="who-list">${here.map((e) => `<li><div><b class="wrap">${esc(e.name)}</b><small>${e.me ? "Your birthday" : turns(e, sel) ? "Turns " + turns(e, sel) : "Birthday"}</small></div>${e.me ? "" : `<a class="button quiet" href="#friends/${esc(e.id)}" aria-label="${esc(e.name)}’s list">Their list</a>`}</li>`).join("")}</ul>` : '<p class="muted">No birthdays.</p>') : '<p class="muted">Choose a day.</p>'}</section><section class="sheet panel"><h2>Coming up</h2>${
      soon.length
        ? `<ul class="soon">${soon
            .map((e) => {
              const n = daysBetween(today, e.at);
              return `<li><a href="#friends/${esc(e.id)}"><time datetime="${new Date(e.at).toISOString().slice(0, 10)}">${fmt({ day: "numeric", month: "short" }, e.at)}</time><b>${esc(e.name)}</b><span${n === 0 ? ' class="now"' : ""}>${inDays(n)}</span></a></li>`;
            })
            .join("")}</ul><p class="hint">Showing the next 30 days.</p>`
        : `<p class="muted">${friendsData.friends.length ? "Nobody has a birthday in the next 30 days." : "No friends yet. Send your friend link from the People tab."}</p>`
    }</section>`;
  if (focus) $(focus)?.focus();
}

async function friendsPeople(version) {
  await loadFriends();
  if (version !== state.routeVersion) return;
  const d = friendsData,
    today = utcToday(),
    link = location.origin + "/#friend/" + d.code,
    rows = d.friends
      .map((f) => ({
        ...f,
        at: f.birthday ? birthdayNext(f.birthday, today) : Infinity,
      }))
      .sort((a, b) => a.at - b.at || a.name.localeCompare(b.name));
  header("friends");
  $("#main").innerHTML =
    `${friendsHead("people")}<div class="detail"><div class="detail-main">${d.requests.map((r) => `<div class="notice request" role="status"><p><strong class="wrap">${esc(r.name)}</strong> asked to be friends. You would see each other’s birthdays and lists.</p><div class="buttons"><button type="button" data-action="friend-accept" data-id="${esc(r.id)}">Accept</button><button type="button" class="secondary" data-action="friend-decline" data-id="${esc(r.id)}">Not now</button></div></div>`).join("")}${d.sent.length ? `<p class="muted">${d.sent.map((s) => `You asked <strong class="wrap">${esc(s.name)}</strong>. <button type="button" class="quiet" data-action="friend-cancel" data-id="${esc(s.id)}" aria-label="Cancel your request to ${esc(s.name)}">Cancel</button>`).join("<br>")}</p>` : ""}${rows.length ? `<div class="grid-head"><h2>Your friends</h2><span class="muted num">${rows.length}</span></div><table class="exchanges"><thead><tr><th scope="col">Name</th><th scope="col">Birthday</th><th scope="col">Next</th><th scope="col">List</th></tr></thead><tbody>${rows.map((f) => `<tr><td><a href="#friends/${esc(f.id)}" class="wrap">${esc(f.name)}</a></td><td data-label="Birthday">${f.birthday ? birthdayText(f.birthday) : '<span class="muted">Not shared</span>'}</td><td data-label="Next">${f.birthday ? inDays(daysBetween(today, f.at)) : "–"}</td><td data-label="List">${f.ideas ? plural(f.ideas, "idea", "ideas") : "No ideas yet"}</td></tr>`).join("")}</tbody></table>` : '<section class="empty sheet"><h2>No friends yet</h2><p>Send your friend link. When someone opens it and you accept, you can see each other’s birthdays and lists.</p></section>'}</div><aside><section class="sheet invite"><h2>Your friend link</h2><p>Send this to someone. When they open it they can ask to be friends, and you choose whether to accept.</p><label for="friend-link" class="visually-hidden">Friend link</label><input id="friend-link" readonly value="${esc(link)}"><div class="buttons"><button type="button" class="secondary" data-action="copy-friend">Copy link</button>${navigator.share ? '<button type="button" class="secondary" data-action="share-friend">Share</button>' : ""}</div><button type="button" class="quiet" data-action="rotate-friend" aria-describedby="friend-rotate-hint">Replace link</button><p class="hint" id="friend-rotate-hint">Replacing it stops the old link working. Friends you already have stay.</p></section></aside></div>`;
}

// On the exchanges page: friends whose birthday is within two weeks.
function birthdaysSoonHTML(fr) {
  if (!fr || !fr.friends?.length) return "";
  const today = utcToday(),
    soon = fr.friends
      .filter((f) => f.birthday)
      .map((f) => ({
        f,
        n: daysBetween(today, birthdayNext(f.birthday, today)),
      }))
      .filter((x) => x.n <= 14)
      .sort((a, b) => a.n - b.n);
  if (!soon.length) return "";
  return `<p class="notice">Birthdays soon: ${soon
    .slice(0, 3)
    .map(
      ({ f, n }) =>
        `<a href="#friends/${esc(f.id)}" class="wrap">${esc(f.name)}</a> ${inDays(n).toLowerCase()}`,
    )
    .join(
      ", ",
    )}${soon.length > 3 ? `, and <a href="#friends">${soon.length - 3} more</a>` : ""}.</p>`;
}

// Opening someone's friend link asks to be their friend; they choose whether to accept.
async function friendLink(code, version) {
  const r = await api("friend/" + encodeURIComponent(code));
  if (version !== state.routeVersion) return;
  header("friends");
  const n = esc(r.name);
  let body;
  if (r.own)
    body = `<h1>Your friend link</h1><p>This is your own link. Send it to someone you’d like to see on your calendar.</p><a class="button" href="#friends/people">Back to friends</a>`;
  else if (r.friends)
    body = `<h1>You and ${n} are friends</h1><p>You can already see each other’s birthdays and lists.</p><a class="button" href="#friends/people">Back to friends</a>`;
  else if (r.asked)
    body = `<h1>You’ve asked ${n}</h1><p>They’ll see your request next time they open Gifty.</p><a class="button" href="#friends/people">Back to friends</a>`;
  else
    body = `<h1>${r.asking ? `${n} asked you first` : `Ask ${n} to be friends?`}</h1><p>${r.asking ? "Opening their link settles it: you’ll be friends straight away." : "If they accept, you’ll see each other’s birthdays and the ideas each of you shows to friends. They choose whether to accept."}</p><form data-ff="friend-request" data-code="${esc(code)}">${errorBox()}<button>${r.asking ? "Become friends" : "Ask to be friends"}</button></form>`;
  $("#main").innerHTML = `<div class="narrow">${body}</div>`;
}

async function friendLinkSignedOut(code, version) {
  const r = await api("friend-link/" + encodeURIComponent(code));
  if (version !== state.routeVersion) return;
  sessionStorage.setItem("giftyFriend", code);
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1 class="wrap">${esc(r.name)} sent you a friend link</h1><p>Sign in, or create an account, to ask to be friends with ${esc(r.name)}. You’ll come back here afterwards.</p><div class="buttons"><a class="button" href="#signup">Create an account</a><a class="button secondary" href="#login">Sign in</a></div></div>`;
}

const friendClaimFor = (fid) => (w) => {
  const t = esc(w.title);
  if (w.status)
    return `<div class="claim"><p class="tag">${w.status === "got" ? "They’ve got this now." : "They don’t want this any more."} Check before you buy it.</p><button type="button" class="quiet" data-action="friend-claim" data-friend="${fid}" data-id="${w.id}" data-on="false" aria-label="Stop tracking ${t}">Stop tracking</button></div>`;
  if (w.claim === "mine")
    return `<div class="claim"><p class="tag">You’re getting this.</p><button type="button" class="quiet" data-action="friend-claim" data-friend="${fid}" data-id="${w.id}" data-on="false" aria-label="Undo: you’re getting ${t}">Undo</button></div>`;
  if (w.claim === "other")
    return `<div class="claim"><p class="tag">Someone else is getting this one.</p></div>`;
  return `<div class="claim"><button type="button" class="secondary small" data-action="friend-claim" data-friend="${fid}" data-id="${w.id}" data-on="true" aria-label="I’m getting this: ${t}">I’m getting this</button></div>`;
};

const friendWishes = (fid, list, name) =>
  `<section id="friend-wishes"><h2 tabindex="-1">${esc(name)}’s ideas</h2>${list.length ? wishesHTML(list, false, friendClaimFor(fid)) : `<p class="muted">No ideas yet. Anything ${esc(name)} shows to friends will appear here.</p>`}</section>`;

const claimNotice = (name) =>
  `<p class="muted">Marking an idea tells your other friends, and anyone else buying for ${esc(name)}, that someone has it. ${esc(name)} is never told, and can’t see which ideas are marked.</p>`;

async function friendPage(id, version) {
  const f = await api("friends/" + encodeURIComponent(id));
  if (version !== state.routeVersion) return;
  friendPageData = f;
  shownWishes = f.wishes;
  header("friends");
  const today = utcToday(),
    b = f.birthday,
    n = b ? daysBetween(today, birthdayNext(b, today)) : null,
    n2 = esc(f.name),
    t =
      b && b.year
        ? new Date(birthdayNext(b, today)).getUTCFullYear() - b.year
        : 0;
  $("#main").innerHTML =
    `<a class="back" href="#friends/people">Back to friends</a><div class="page-head"><div><h1 class="wrap">${n2}</h1>${b ? `<span class="status">${n === 0 ? "Birthday today" : n <= 30 ? "Birthday " + inDays(n) : "Birthday " + fmt({ day: "numeric", month: "long" }, birthdayNext(b, today))}</span>` : ""}</div></div><dl class="facts"><div><dt class="label">Birthday</dt><dd>${b ? fmt({ day: "numeric", month: "long" }, Date.UTC(2000, b.month - 1, b.day)) : "Not shared"}</dd></div>${t ? `<div><dt class="label">Turns</dt><dd>${t}</dd></div>` : ""}<div><dt class="label">On their list</dt><dd>${f.wishes.length ? plural(f.wishes.length, "idea", "ideas") : "None yet"}</dd></div></dl><div class="detail-main measure">${claimNotice(f.name)}${friendWishes(f.id, f.wishes, f.name)}<div class="after"><details class="rem-change"><summary>Manage friend</summary><div class="rem-body"><p>Removing a friend stops you seeing each other’s birthdays and lists, and releases anything either of you had marked. Nobody is sent a message.</p><button type="button" class="secondary" data-action="friend-remove" data-id="${esc(f.id)}">Remove friend</button></div></details></div></div>`;
}

// Account page: your birthday.
function birthdayHTML() {
  const b = state.user.birthday,
    months = Array.from(
      { length: 12 },
      (_, i) =>
        `<option value="${i + 1}"${b && b.month === i + 1 ? " selected" : ""}>${monthName(i)}</option>`,
    ).join("");
  return `<section class="sheet" id="birthday"><h2>Birthday</h2><p>Your friends see this on their calendar. Nobody else does.</p><form data-ff="birthday">${errorBox()}<div class="row"><div class="field"><label for="bd-month">Month</label><select id="bd-month" name="month"><option value="0">Not set</option>${months}</select></div><div class="field"><label for="bd-day">Day</label><input id="bd-day" name="day" type="number" min="1" max="31" inputmode="numeric" value="${b ? b.day : ""}"></div><div class="field"><label for="bd-year">Year <span class="optional">(optional)</span></label><input id="bd-year" name="year" type="number" min="1900" max="${new Date().getFullYear()}" inputmode="numeric" value="${b && b.year ? b.year : ""}" placeholder="1990"></div></div><p class="hint field-hint">With a year, friends see how old you’re turning. Without one, they don’t.</p><label class="check"><input type="checkbox" name="show" ${b && state.user.birthdayHidden ? "" : "checked"}><span>Show my birthday to friends<span class="hint">Untick to keep it saved but hidden.</span></span></label><div class="buttons"><button>Save birthday</button></div></form></section>`;
}

// Group gift: the friend it is for, their ideas shown to friends, and what has been taken.
function groupSection(e) {
  const f = e.for;
  shownWishes = f.wishes || [];
  if (!f.friends)
    return `<section id="friend-wishes"><h2 class="wrap">${esc(f.name)}’s ideas</h2><p class="notice">You and ${esc(f.name)} aren’t friends any more, so their list isn’t shown. Add them as a friend again to see it.</p></section>`;
  return claimNotice(f.name) + friendWishes(f.id, shownWishes, f.name);
}

const busy = async (form, fn) => {
  const button = form.querySelector('button:not([type="button"])');
  if (button.disabled) return;
  button.disabled = true;
  form.setAttribute("aria-busy", "true");
  const error = $(".form-error", form);
  if (error) error.textContent = "";
  try {
    await fn();
  } catch (err) {
    if (error) {
      error.textContent = err.message;
      error.scrollIntoView({ block: "nearest" });
    } else notify(err.message);
  } finally {
    button.disabled = false;
    form.removeAttribute("aria-busy");
  }
};

function getShownWishes() {
  return shownWishes;
}
function getFriendPageData() {
  return friendPageData;
}

function initFriends() {
  document.addEventListener("submit", (event) => {
    const form = event.target.closest("form[data-ff]");
    if (!form) return;
    event.preventDefault();
    busy(form, async () => {
      const data = Object.fromEntries(new FormData(form));
      switch (form.dataset.ff) {
        case "friend-request": {
          await api("friends/request", { code: form.dataset.code });
          sessionStorage.removeItem("giftyFriend");
          location.hash = "friends/people";
          notify("Asked. You’ll be friends once they accept.");
          break;
        }
        case "friend-remove":
          await api("friends/remove", { id: form.dataset.id });
          $("#dialog").close();
          location.hash = "friends/people";
          notify(
            "Removed. Anything either of you had marked on the other’s list is released.",
          );
          break;
        case "friend-rotate": {
          friendsData = await api("friends/rotate", {});
          $("#dialog").close();
          await route();
          notify("Friend link replaced. The old link no longer works.");
          break;
        }
        case "birthday": {
          const month = Number(data.month),
            day = Number(data.day),
            year = Number(data.year) || 0;
          if (!month && !day && !year && !state.user.birthday) {
            notify("Choose a month and day first.");
            break;
          }
          if (month === 0 && !day) {
            state.user = await api("birthday", { month: 0, day: 0 });
            $("#birthday").outerHTML = birthdayHTML();
            notify("Birthday removed.");
            break;
          }
          if (!month || !day) throw Error("Choose both a month and a day.");
          state.user = await api("birthday", {
            month,
            day,
            year,
            show: data.show === "on",
          });
          $("#birthday").outerHTML = birthdayHTML();
          notify(
            data.show === "on"
              ? "Saved. Your friends can see it."
              : "Saved. It’s hidden from your friends.",
          );
          break;
        }
      }
    });
  });

  // Arrow keys move between days, so the grid is one tab stop instead of thirty-five.
  document.addEventListener("keydown", (event) => {
    const b = event.target.closest?.(".day");
    if (!b || event.altKey || event.ctrlKey || event.metaKey) return;
    const ts = Number(b.dataset.ts),
      wd = (new Date(ts).getUTCDay() + 6) % 7,
      by = {
        ArrowLeft: -1,
        ArrowRight: 1,
        ArrowUp: -7,
        ArrowDown: 7,
        Home: -wd,
        End: 6 - wd,
      }[event.key];
    if (by === undefined) return;
    event.preventDefault();
    const to = ts + by * 864e5,
      d = new Date(to);
    cal.y = d.getUTCFullYear();
    cal.m = d.getUTCMonth();
    cal.sel = to;
    paintCal(`.day[data-ts="${to}"]`);
  });

  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "cal-day":
          cal.sel = Number(b.dataset.ts);
          paintCal(`.day[data-ts="${cal.sel}"]`);
          break;
        case "cal-step": {
          const d = new Date(Date.UTC(cal.y, cal.m + Number(b.dataset.by), 1));
          cal.y = d.getUTCFullYear();
          cal.m = d.getUTCMonth();
          const t = new Date(utcToday());
          cal.sel =
            cal.y === t.getUTCFullYear() && cal.m === t.getUTCMonth()
              ? utcToday()
              : null;
          paintCal(b.dataset.by === "1" ? ".cal-bar .next" : ".cal-bar .prev");
          break;
        }
        case "cal-today": {
          const t = new Date(utcToday());
          cal.y = t.getUTCFullYear();
          cal.m = t.getUTCMonth();
          cal.sel = utcToday();
          paintCal(`.day[data-ts="${cal.sel}"]`);
          break;
        }
        case "friend-accept":
        case "friend-decline":
        case "friend-cancel": {
          if (saving.has(action)) break;
          saving.add(action);
          try {
            await api("friends/" + action.slice(7), { id: b.dataset.id });
            await route();
            notify(
              action === "friend-accept"
                ? "You’re friends now."
                : action === "friend-decline"
                  ? "Request declined. They aren’t told."
                  : "Request cancelled.",
            );
          } finally {
            saving.delete(action);
          }
          break;
        }
        case "friend-remove": {
          const name = friendPageData?.name || "this person";
          modal(
            esc(`Remove ${name} as a friend?`),
            `<p>You’ll stop seeing each other’s birthdays and lists, and anything either of you had marked on the other’s list is released. Nobody is sent a message. You can be friends again later.</p><form data-ff="friend-remove" data-id="${esc(b.dataset.id)}">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Remove friend</button></div></form>`,
          );
          break;
        }
        case "rotate-friend":
          modal(
            "Replace your friend link?",
            `<p>The current link will stop working. Friends you already have stay, and requests already sent stay too.</p><form data-ff="friend-rotate">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Replace link</button></div></form>`,
          );
          break;
        case "copy-friend":
          try {
            await navigator.clipboard.writeText($("#friend-link").value);
            notify("Link copied.");
          } catch {
            $("#friend-link").select();
            notify(
              "Couldn’t copy automatically. The link is selected, so copy it from there.",
            );
          }
          break;
        case "share-friend":
          try {
            await navigator.share({
              title: "Be my friend on Gifty",
              text: "Add me as a friend on Gifty.",
              url: $("#friend-link").value,
            });
          } catch (err) {
            if (err.name !== "AbortError")
              notify("Couldn’t open sharing. Use Copy link instead.");
          }
          break;
        case "friend-claim": {
          const on = b.dataset.on === "true",
            id = b.dataset.id,
            fid = b.dataset.friend;
          if (saving.has("claim" + id)) break;
          saving.add("claim" + id);
          try {
            const r = await api(`friends/${fid}/claim`, {
              wish: id,
              claim: on,
            });
            if (
              state.current?.kind === "group" &&
              location.hash.startsWith("#exchange/")
            ) {
              state.current = await api("exchanges/" + state.current.id);
              $("#friend-wishes").outerHTML = friendWishes(
                fid,
                state.current.for.wishes,
                state.current.for.name,
              );
              shownWishes = state.current.for.wishes;
            } else {
              friendPageData = r;
              shownWishes = r.wishes;
              $("#friend-wishes").outerHTML = friendWishes(
                fid,
                r.wishes,
                r.name,
              );
            }
            (
              $(
                `#friend-wishes [data-action="friend-claim"][data-id="${id}"]`,
              ) || $("#friend-wishes h2")
            ).focus();
            notify(
              on
                ? "Marked. Anyone else who can see their list will see that someone has it."
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
      }
    } catch (err) {
      notify(err.message);
    }
  });
}

export {
  birthdaysSoonHTML,
  loadFriends,
  groupSection,
  getShownWishes,
  birthdayHTML,
  friendLinkSignedOut,
  friendsPeople,
  friendPage,
  friendsCalendar,
  friendLink,
  getFriendPageData,
  initFriends,
};
