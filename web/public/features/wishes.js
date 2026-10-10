import { api } from "../core/api.js";
import { state } from "../core/session.js";
import { header } from "../ui/navigation.js";
import { plural } from "../ui/format.js";
import { wishesHTML } from "../ui/wish-cards.js";
import { birthdayOn, utcToday, fmt } from "./birthday-dates.js";
import { $, esc } from "../ui/dom.js";
import { modal, confirmAction } from "../ui/dialog.js";
import { errorBox, field, onSubmit } from "../ui/forms.js";
import { photoField, photosForForm } from "./photos.js";
import { route } from "../core/router.js";
import { notify } from "../ui/toast.js";

// myExchanges and wishScope feed the idea dialog: which exchanges an idea can be limited to, and which one new ideas start in.
let myExchanges = [];

// myExchanges and wishScope feed the idea dialog: which exchanges an idea can be limited to, and which one new ideas start in.
let wishScope = "";

// Who an idea is shown to, in words, for the owner's own list.
function audienceText(w) {
  const givers = !w.noExchanges,
    names = (w.exchanges || [])
      .map((id) => myExchanges.find((e) => e.id === id)?.name)
      .filter(Boolean),
    g = !givers
      ? ""
      : (w.exchanges || []).length
        ? `whoever buys for you in ${names.join(", ") || "some exchanges"}`
        : "whoever buys for you";
  return w.friends && g
    ? `Seen by friends and by ${g}`
    : w.friends
      ? "Seen by friends only"
      : g
        ? `Seen by ${g}`
        : "Only you can see this";
}

async function wishes(version, scope) {
  const [me, items, fr] = await Promise.all([
    api("me"),
    api("exchanges"),
    api("friends").catch(() => ({ friends: [] })),
  ]);
  if (version !== state.routeVersion) return;
  state.user = me;
  myExchanges = items
    .filter((e) => !e.archived)
    .sort((a, b) => a.date.localeCompare(b.date));
  wishScope = myExchanges.some((e) => e.id === scope) ? scope : "";
  header("wishes");
  const list = state.user.wishes || [],
    active = list.filter((w) => !w.status),
    sorted = list.filter((w) => w.status),
    both = active.filter((w) => w.friends && !w.noExchanges),
    fonly = active.filter((w) => w.friends && w.noExchanges),
    rest = active.filter((w) => !w.friends),
    everyEx = rest.filter((w) => !w.noExchanges && !(w.exchanges || []).length),
    priv = rest.filter((w) => w.noExchanges),
    group = (id, title, items, none) =>
      `<section class="wish-group" id="${id}" aria-labelledby="${id}-h"><div class="grid-head"><h2 id="${id}-h" class="wrap">${title}</h2><span class="muted num">${plural(items.length, "idea", "ideas")}</span></div>${items.length ? wishesHTML(items, true, false, audienceText) : `<p class="muted">${none}</p>`}</section>`,
    sortedHTML = sorted.length
      ? `<section class="wish-group" aria-labelledby="wg-sorted"><div class="grid-head"><h2 id="wg-sorted">Done with</h2><span class="muted num">${plural(sorted.length, "idea", "ideas")}</span></div><p class="muted">Nobody sees these. Put one back if you change your mind.</p>${wishesHTML(sorted, true, false, audienceText)}</section>`
      : "",
    b = state.user.birthday,
    ty = new Date().getFullYear();
  let last = b && birthdayOn(b, ty);
  if (b && last > utcToday()) last = birthdayOn(b, ty - 1);
  $("#main").innerHTML =
    `<div class="wish-page"><div class="page-head"><div><h1>Wish list</h1><p class="muted">Choose who sees each idea: friends, whoever is buying for you, or both. Friends can mark ideas they’re getting. You never see which.</p></div><button data-action="add-wish">Add idea</button></div>${state.user.birthdayRecent && b ? `<p class="notice">Your birthday was on ${fmt({ day: "numeric", month: "long" }, last)}. Mark what you’ve got, or no longer want, so the list stays useful for next time.</p>` : ""}${
      list.length
        ? `${both.length ? group("wg-both", "Friends and whoever buys for you", both, "") : ""}${fonly.length || (fr.friends.length && !both.length) ? group("wg-friends", "Friends only", fonly, "None. Tick Friends when you add an idea and the friends you’ve added can see it.") : ""}${group("wg-all", "Whoever buys for you", everyEx, "None. Ideas you add for whoever is buying for you are seen in every exchange.")}${myExchanges
            .map((e) =>
              group(
                "wishes-" + e.id,
                "Whoever buys for you in " + esc(e.name),
                rest.filter(
                  (w) => !w.noExchanges && (w.exchanges || []).includes(e.id),
                ),
                `None. Whoever draws you in ${esc(e.name)} sees your ideas for every exchange.`,
              ),
            )
            .join(
              "",
            )}${priv.length ? group("wg-private", "Only you", priv, "") : ""}${sortedHTML}`
        : '<section class="empty sheet"><h2>No ideas yet</h2><p>Add things you’d be glad to get. A photo, link, size or colour helps the people buying for you.</p></section>'
    }</div>`;
  if (wishScope) $("#wishes-" + wishScope)?.scrollIntoView({ block: "start" });
}

function scopeField(w) {
  const chosen = w.id ? w.exchanges || [] : wishScope ? [wishScope] : [],
    givers = w.id ? !w.noExchanges : true,
    friends = w.id ? !!w.friends : true,
    all = !chosen.length;
  return `<fieldset class="choices"><legend>Who can see this idea</legend><label class="check"><input type="checkbox" name="friends" ${friends ? "checked" : ""}><span>Friends<span class="hint">Everyone you’ve added as a friend, and no one else.</span></span></label><label class="check"><input type="checkbox" name="givers" ${givers ? "checked" : ""}><span>Whoever buys for me in an exchange<span class="hint">${myExchanges.length ? "Choose which exchanges below." : "Whoever draws your name sees it."}</span></span></label>${myExchanges.length ? `<div class="nested indent"><label class="check"><input type="radio" name="scope" value="all" ${all ? "checked" : ""}><span>Every exchange</span></label><label class="check"><input type="radio" name="scope" value="some" ${all ? "" : "checked"}><span>Only some exchanges</span></label><div class="nested indent">${myExchanges.map((e) => `<label class="check"><input type="checkbox" name="exchanges" value="${e.id}" ${chosen.includes(e.id) ? "checked" : ""}><span class="wrap">${esc(e.name)}</span></label>`).join("")}</div></div>` : ""}<p class="hint">Tick neither and the idea is only for you.</p></fieldset>`;
}

function wishModal(w = {}) {
  modal(
    w.id ? "Edit idea" : "Add idea",
    `<form data-form="wish">${errorBox()}<input type="hidden" name="id" value="${esc(w.id || "")}">${scopeField(w)}${field("title", "What is it?", "text", w.title || "", 'required maxlength="120" autofocus placeholder="e.g. Wool socks, size 6–8"')}${field("price", 'Rough price <span class="optional">(optional)</span>', "text", w.price || "", 'maxlength="60" placeholder="e.g. about £25"')}${field("url", 'Link <span class="optional">(optional)</span>', "url", w.url || "", 'maxlength="2000" placeholder="https://"')}<div class="field"><label for="note">Details <span class="optional">(optional)</span></label><textarea name="note" id="note" maxlength="1000" placeholder="Size, colour, where to buy it.">${esc(w.note || "")}</textarea></div>${photoField(w)}<button class="full">Save</button></form>`,
  );
}

function initWishes() {
  onSubmit(["wish"], async (form, data) => {
    switch (form.dataset.form) {
      case "wish":
        {
          const fd = new FormData(form),
            friends = fd.has("friends"),
            givers = fd.has("givers"),
            some = givers && fd.get("scope") === "some",
            exchanges = some ? fd.getAll("exchanges") : [];
          if (some && !exchanges.length)
            throw Error(
              "Choose at least one exchange, or show the idea in every exchange.",
            );
          const photos = photosForForm(form);
          if (photos.some((p) => p.busy))
            throw Error(
              "A photo is still being prepared. Try again in a moment.",
            );
          state.user = await api("wishes", {
            id: data.id,
            title: data.title,
            url: data.url,
            note: data.note,
            price: data.price,
            exchanges,
            friends,
            noExchanges: !givers,
            photos: photos.map((p) => p.value),
          });
        }
        $("#dialog").close();
        await route();
        notify("Idea saved.");
        break;
    }
  });

  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "add-wish":
          wishModal();
          break;
        case "sort-wish": {
          const w = state.user.wishes.find((w) => w.id === b.dataset.id);
          modal(
            esc(`Take ${w?.title || "this idea"} off your list?`),
            `<p>Whoever is buying for you won’t see it any more. You can put it back later.</p>${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button type="button" data-action="wish-status" data-id="${esc(b.dataset.id)}" data-status="dropped">I don’t want it any more</button><button type="button" data-action="wish-status" data-id="${esc(b.dataset.id)}" data-status="got">I’ve got it</button></div>`,
          );
          break;
        }
        case "wish-status":
          try {
            state.user = await api(`wishes/${b.dataset.id}/status`, {
              status: b.dataset.status,
            });
          } catch (err) {
            const box = $("#dialog .form-error");
            box.textContent = err.message;
            break;
          }
          $("#dialog").close();
          await route();
          notify("Taken off your list. Nobody sees it any more.");
          break;
        case "unsort-wish":
          state.user = await api(`wishes/${b.dataset.id}/status`, {
            status: "",
          });
          await route();
          notify("Back on your list.");
          break;
        case "edit-wish":
          wishModal(state.user.wishes.find((w) => w.id === b.dataset.id));
          break;
        case "delete-wish": {
          const w = state.user.wishes.find((w) => w.id === b.dataset.id);
          confirmAction(
            `Remove ${w?.title || "this idea"}?`,
            "It will be removed from your wish list.",
            "delete-wish",
            b.dataset.id,
          );
          break;
        }
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
    if (box.closest('form[data-form="wish"]')) {
      const f = box.form;
      if (box.name === "exchanges" && box.checked)
        f.querySelector('input[value="some"]').checked = true;
      if ((box.name === "exchanges" && box.checked) || box.name === "scope")
        f.elements.givers.checked = true;
      return;
    }
  });
}

export { wishes, wishModal, initWishes };
