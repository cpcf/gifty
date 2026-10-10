import { errorBox, onSubmit } from "../ui/forms.js";
import { $ } from "../ui/dom.js";
import { state } from "../core/session.js";
import { api } from "../core/api.js";
import { notify } from "../ui/toast.js";
import { saving } from "../core/operations.js";

const units = {
  day: ["day", "days"],
  week: ["week", "weeks"],
  month: ["month", "months"],
};

const unitMax = { day: 365, week: 52, month: 12 };

const describe = (r) =>
  r.n === 0
    ? "on the day"
    : r.n === 1 && r.unit === "day"
      ? "the day before"
      : `${r.n} ${units[r.unit][r.n === 1 ? 0 : 1]} before`;

const summary = (rs) =>
  rs.length
    ? rs
        .map(describe)
        .join(", ")
        .replace(/^./, (c) => c.toUpperCase()) + "."
    : "No reminders.";

let rowId = 0;

function reminderRow(r = { n: 1, unit: "week" }) {
  const id = "rem" + ++rowId;
  return `<li class="rem-row"><input id="${id}" type="number" inputmode="numeric" required min="${r.unit === "day" ? 0 : 1}" max="${unitMax[r.unit]}" value="${r.n}" aria-label="How many"><select aria-label="Days, weeks or months">${Object.keys(
    units,
  )
    .map(
      (u) =>
        `<option value="${u}" ${u === r.unit ? "selected" : ""}>${units[u][r.n === 1 ? 0 : 1]}</option>`,
    )
    .join(
      "",
    )}</select><span>before</span><button type="button" class="quiet" data-action="rem-remove">Remove<span class="visually-hidden"> this reminder</span></button></li>`;
}

function reminderEditor(target, list, label, save) {
  return `<form class="rem-editor" data-form="reminders" data-target="${target}" aria-label="${label}">${errorBox()}<ul class="rem-rows">${list.map(reminderRow).join("")}</ul><p class="hint rem-empty" ${list.length ? "hidden" : ""}>No reminders. Add one, or save to have none.</p><div class="buttons"><button type="button" class="secondary" data-action="rem-add">Add a reminder<span class="visually-hidden"> to ${label.toLowerCase()}</span></button><button>${save}</button><button type="button" class="quiet" data-action="rem-cancel">Cancel</button></div></form>`;
}

function readReminders(form) {
  return [...form.querySelectorAll(".rem-row")].map((li) => ({
    n: Number($("input", li).value),
    unit: $("select", li).value,
  }));
}

function accountRemindersHTML() {
  const own = state.user.ownReminders;
  return `<fieldset class="choices" id="account-reminders"><legend>Reminders</legend><label class="check"><input type="radio" name="reminder-source" value="organiser" ${own ? "" : "checked"}><span>Use each organiser’s choice</span></label><label class="check"><input type="radio" name="reminder-source" value="own" ${own ? "checked" : ""}><span>Use my own in every exchange</span></label>${own ? `<div class="nested">${reminderEditor("account", state.user.reminders, "Your reminders for every exchange", "Save reminders")}</div>` : ""}<p class="hint">You can also set reminders for one exchange on its page.</p></fieldset>`;
}

function refreshAccountReminders(focus) {
  const s = $("#account-reminders");
  if (!s) return;
  s.outerHTML = accountRemindersHTML();
  if (focus) $(`input[name="${focus.name}"][value="${focus.value}"]`)?.focus();
}

// Reminders read as a summary with one Change control; the editors only appear when asked for.
function remindersHTML(e) {
  const owner = e.owner === state.user.id,
    custom = e.mine.source === "exchange",
    from =
      e.mine.source === "account"
        ? "your usual reminders"
        : owner
          ? "your default"
          : "the organiser’s choice",
    back =
      e.mine.source === "account" || (custom && state.user.ownReminders)
        ? "my usual reminders"
        : owner
          ? "my default"
          : "the organiser’s choice";
  return `<section class="sheet" id="reminders"><h2>Email reminders</h2><p class="hint">Sent before the exchange date, once entries are closed.${state.user.verified ? "" : ' Confirm your email address to get them: see <a href="#account">Account</a>.'}</p><dl class="rem-summary">${owner ? `<div><dt class="label">Everyone</dt><dd>${summary(e.reminders)}</dd></div>` : ""}${owner && !custom && !e.ready && e.mine.source !== "account" ? "" : `<div><dt class="label">You</dt><dd>${e.ready ? "None. Your gift is ready." : custom ? summary(e.mine.reminders) : `${summary(e.mine.reminders)} <span class="muted">Same as ${from}.</span>`}</dd></div>`}</dl>${owner || !e.ready ? `<details class="rem-change"><summary>Change reminders</summary><div class="rem-body">${owner ? `<h3>Everyone</h3>${reminderEditor("default", e.reminders, "Default reminders for everyone", "Save for everyone")}` : ""}${e.ready ? "" : custom ? `<h3>Just you</h3>${reminderEditor("mine", e.mine.reminders, "Your reminders for this exchange", "Save my reminders")}<button class="quiet" data-action="reminders-default">Go back to ${back}</button>` : `<details class="rem-change rem-own"><summary>Use different reminders for me</summary>${reminderEditor("mine", e.mine.reminders, "Your reminders for this exchange", "Save my reminders")}</details>`}</div></details>` : ""}</section>`;
}

function refreshReminders(focusSelector) {
  const s = $("#reminders");
  if (!s) return;
  s.outerHTML = remindersHTML(state.current);
  if (focusSelector) $(focusSelector)?.focus();
}

function initReminders() {
  onSubmit(["reminders"], async (form, data) => {
    switch (form.dataset.form) {
      case "reminders": {
        const reminders = readReminders(form),
          t = form.dataset.target;
        if (t === "account") {
          state.user = await api("account/reminders", { reminders });
          refreshAccountReminders();
        } else {
          state.current = await api(
            `exchanges/${state.current.id}/${t === "mine" ? "my-reminders" : "reminders"}`,
            { reminders },
          );
          refreshReminders();
        }
        (
          $("#reminders .rem-change>summary") ||
          $(`form[data-target="${t}"] [data-action="rem-add"]`)
        )?.focus();
        notify("Reminders saved.");
        break;
      }
    }
  });

  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "rem-add": {
          const form = b.closest("form"),
            rows = $(".rem-rows", form);
          if (rows.children.length >= 8) {
            notify("You can add up to 8 reminders.");
            break;
          }
          rows.insertAdjacentHTML("beforeend", reminderRow());
          $(".rem-empty", form).hidden = true;
          rows.lastElementChild.querySelector("input").focus();
          break;
        }
        case "rem-cancel":
          if (b.closest("#reminders"))
            refreshReminders("#reminders .rem-change>summary");
          else
            refreshAccountReminders({ name: "reminder-source", value: "own" });
          break;
        case "rem-remove": {
          const li = b.closest("li"),
            form = li.closest("form"),
            next = li.nextElementSibling || li.previousElementSibling;
          li.remove();
          $(".rem-empty", form).hidden = !!$(".rem-row", form);
          (next
            ? $("input", next)
            : $('[data-action="rem-add"]', form)
          ).focus();
          break;
        }
        case "reminders-default":
          state.current = await api(
            `exchanges/${state.current.id}/my-reminders`,
            {
              default: true,
            },
          );
          refreshReminders("#reminders .rem-change>summary");
          notify(
            state.current.mine.source === "account"
              ? "Using your usual reminders."
              : "Using the organiser’s choice.",
          );
          break;
      }
    } catch (err) {
      notify(err.message);
    }
  });

  document.addEventListener("change", async (event) => {
    const box = event.target.closest(
      'input[type="checkbox"],input[type="radio"]',
    );
    if (
      !box ||
      !["birthday-mail", "notify", "reminder-source"].includes(box.name)
    )
      return;
    if (saving.has(box.name)) {
      box.checked = !box.checked;
      return;
    }
    saving.add(box.name);
    try {
      switch (box.name) {
        case "birthday-mail":
          state.user = await api("account/birthday-mail", {
            notify: box.checked,
          });
          notify(
            box.checked
              ? "Birthday emails turned on."
              : "Birthday emails turned off.",
          );
          break;
        case "notify":
          state.user = await api("account", { notify: box.checked });
          notify(
            box.checked
              ? "Exchange emails turned on."
              : "Exchange emails turned off.",
          );
          break;
        case "reminder-source":
          state.user = await api(
            "account/reminders",
            box.value === "own"
              ? {
                  reminders: state.user.reminders.length
                    ? state.user.reminders
                    : [
                        { n: 1, unit: "week" },
                        { n: 1, unit: "day" },
                      ],
                }
              : { default: true },
          );
          refreshAccountReminders(box);
          notify(
            box.value === "own"
              ? "Using your own reminders. Change them below."
              : "Using each organiser’s choice.",
          );
          break;
      }
    } catch (err) {
      if (box.type === "radio") refreshAccountReminders();
      else box.checked = !box.checked;
      notify(err.message);
    } finally {
      saving.delete(box.name);
    }
  });

  document.addEventListener("input", (event) => {
    const n = event.target.closest(".rem-row input");
    if (!n) return;
    const one = Number(n.value) === 1;
    [...$("select", n.closest("li")).options].forEach(
      (o) => (o.textContent = units[o.value][one ? 0 : 1]),
    );
  });

  document.addEventListener("change", (event) => {
    const sel = event.target.closest(".rem-row select");
    if (!sel) return;
    const n = $("input", sel.closest("li"));
    n.max = unitMax[sel.value];
    n.min = sel.value === "day" ? 0 : 1;
    if (Number(n.value) > unitMax[sel.value]) n.value = unitMax[sel.value];
    if (Number(n.value) < Number(n.min)) n.value = n.min;
  });
}

export { remindersHTML, refreshReminders, accountRemindersHTML, initReminders };
