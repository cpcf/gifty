import { header } from "../ui/navigation.js";
import { $, esc } from "../ui/dom.js";
import { state } from "../core/session.js";
import { birthdayHTML } from "./friends.js";
import { accountRemindersHTML } from "./reminders.js";
import { api } from "../core/api.js";
import { plural, statusTag } from "../ui/format.js";
import { onSubmit, errorBox, field } from "../ui/forms.js";
import { clearToast, notify } from "../ui/toast.js";
import { route } from "../core/router.js";
import { modal } from "../ui/dialog.js";

function account() {
  header("account");
  $("#main").innerHTML =
    `<div class="narrow form-page"><h1>Account</h1><dl class="facts stacked"><div><dt class="label">Name</dt><dd class="wrap">${esc(state.user.name)}</dd></div><div><dt class="label">Email</dt><dd class="wrap">${esc(state.user.email)}</dd></div></dl>${birthdayHTML()}${state.mailOn ? `<section class="sheet"><h2>Emails</h2>` + `${state.user.verified ? "" : '<p>Your email address isn’t confirmed yet. Gifty only sends exchange emails to confirmed addresses, so open the link in the confirmation email.</p><p><button class="secondary" data-action="resend-verify">Send the link again</button></p>'}<label class="check"><input type="checkbox" name="notify" ${state.user.notify ? "checked" : ""}><span>Email me about my exchanges<span class="hint">When entries close, and reminders before the exchange date. Emails never say who you’re buying for.</span></span></label><label class="check"><input type="checkbox" name="birthday-mail" ${state.user.birthdayMail ? "checked" : ""}><span>Email me about friends’ birthdays<span class="hint">A week before, and on the day. The email never says what anyone is getting.</span></span></label>${accountRemindersHTML()}</section>` : ""}<div class="after"><button class="secondary" data-action="logout">Sign out</button> <button class="quiet" data-action="delete-account">Delete account</button></div></div>`;
}

async function adminPage(version) {
  const o = await api("admin/overview");
  if (version !== state.routeVersion) return;
  header("admin");
  const item = (lines, btn) =>
    `<li class="admin-row"><div class="wrap">${lines.map((l) => `<div>${l}</div>`).join("")}</div>${btn}</li>`;
  $("#main").innerHTML =
    `<div class="page-head"><div><h1>Admin</h1></div></div><p class="muted">You can remove accounts and exchanges here. Who is buying for whom is never shown.</p><section><h2>Accounts (${o.users.length})</h2><ul class="admin-list">${o.users.map((u) => item([`<b>${esc(u.name)}</b>${u.admin ? " (admin)" : ""}`, esc(u.email) + (u.verified ? "" : " (unconfirmed)"), `<span class="muted">${plural(u.exchanges, "exchange", "exchanges")}</span>`], u.self ? "" : `<button class="quiet" data-action="admin-delete-user" data-id="${esc(u.id)}" aria-label="Delete ${esc(u.name)}">Delete</button>`)).join("")}</ul></section><section><h2>Exchanges (${o.exchanges.length})</h2>${o.exchanges.length ? `<ul class="admin-list">${o.exchanges.map((e) => item([`<b>${esc(e.name)}</b>`, `Organised by ${esc(e.organiser)} · ${plural(e.people, "person", "people")}`, statusTag(e)], `<button class="quiet" data-action="admin-delete-exchange" data-id="${esc(e.id)}" data-name="${esc(e.name)}" aria-label="Delete ${esc(e.name)}">Delete</button>`)).join("")}</ul>` : '<p class="muted">None yet.</p>'}</section>`;
  adminData = o;
}

let adminData = null;

function initAccounts() {
  onSubmit(["delete-account", "admin-confirm"], async (form, data) => {
    switch (form.dataset.form) {
      case "delete-account":
        await api("account/delete", { password: data.password });
        state.user = null;
        state.current = null;
        clearToast();
        sessionStorage.removeItem("giftyInvite");
        $("#dialog").close();
        location.hash = "home";
        await route();
        notify("Your account has been deleted.");
        break;
      case "admin-confirm":
        await api(`admin/${form.dataset.kind}/delete`, { id: form.dataset.id });
        $("#dialog").close();
        await route();
        notify("Deleted.");
        break;
    }
  });

  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "delete-account":
          modal(
            "Delete your account?",
            `<p>This removes your name, email address and wish list for good. Exchanges you’re in that haven’t closed entries lose you; exchanges where names are already drawn keep an empty “Deleted account” so everyone else’s draw still works. If you organise an exchange with other people in it, archive it first.</p><form data-form="delete-account">${errorBox()}${field("password", "Your password", "password", "", 'required maxlength="256" autocomplete="current-password"')}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Delete my account</button></div></form>`,
          );
          break;
        case "admin-delete-user": {
          const u = adminData.users.find((u) => u.id === b.dataset.id);
          modal(
            esc(`Delete ${u.name}?`),
            `<p>${esc(u.email)} will be removed with their wish list. If they are in a drawn exchange they stay on it as an empty “Deleted account”.</p><form data-form="admin-confirm" data-kind="users" data-id="${esc(u.id)}">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Delete account</button></div></form>`,
          );
          break;
        }
        case "admin-delete-exchange":
          modal(
            esc(`Delete ${b.dataset.name}?`),
            `<p>The exchange, its people list and any draw are removed for everyone. This can’t be undone.</p><form data-form="admin-confirm" data-kind="exchanges" data-id="${esc(b.dataset.id)}">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>Delete exchange</button></div></form>`,
          );
          break;
        case "logout":
          await api("logout", {});
          state.user = null;
          state.current = null;
          clearToast();
          sessionStorage.removeItem("giftyInvite");
          location.hash = "home";
          await route();
          break;
        case "resend-verify":
          state.user = await api("verify/resend", {});
          notify(`Confirmation link sent to ${state.user.email}.`);
          break;
      }
    } catch (err) {
      notify(err.message);
    }
  });
}

export { account, adminPage, initAccounts };
