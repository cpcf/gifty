import { header } from "../ui/navigation.js";
import { $, esc } from "../ui/dom.js";
import { errorBox, field, onSubmit } from "../ui/forms.js";
import { state } from "../core/session.js";
import { api } from "../core/api.js";
import { notify } from "../ui/toast.js";
import { route } from "../core/router.js";

function home() {
  header();
  const tk = (n, h, name, sub = "") =>
    `<li class="tk ${h}"><div class="stub">${n}</div><div class="main"><div class="who"><span class="name">${name}</span>${sub ? `<span class="sub">${sub}</span>` : ""}</div></div></li>`;
  $("#main").innerHTML =
    `<div class="home"><div><h1>Organise a gift exchange</h1><p>Pick a date and a spending limit, then send everyone a link. Once they’ve all joined, you close entries and each person draws someone to buy for.</p><div class="buttons"><a class="button" href="#signup">Create an account</a><a class="button secondary" href="#login">Sign in</a></div><p class="hint invited">Been invited? Open the link you were sent.</p></div><div class="hero-art"><ol class="tickets" aria-label="Example: four people in an exchange">${tk("01", "y", "Priya", "Organiser")}${tk("02", "p", "Tom")}${tk("03", "b", "Ruth")}${tk("04", "m", "Sam")}</ol><p class="muted">Everyone who joins gets a numbered ticket. These four are made up.</p></div></div>`;
}

function gatePage() {
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1>Gifty is invite-only for now</h1><p>If someone has invited you to a gift exchange, open the invitation link they sent you. Otherwise, enter the access code you were given. You need it to sign in, too.</p><form class="sheet" data-form="access">${errorBox()}${field("code", "Access code", "password", "", 'required maxlength="200" autocomplete="off"')}<button class="full">Continue</button></form></div>`;
}

function auth(signup) {
  header(signup ? "signup" : "login");
  const next = sessionStorage.getItem("giftyInvite"),
    fnext = sessionStorage.getItem("giftyFriend");
  $("#main").innerHTML =
    `<div class="narrow"><h1>${signup ? "Create an account" : "Sign in"}</h1>${next ? '<p class="muted">You need an account to join the exchange you were invited to. You’ll go back to the invitation afterwards.</p>' : fnext ? '<p class="muted">You need an account to ask to be friends. You’ll come back to the friend link afterwards.</p>' : ""}<form class="sheet" data-form="${signup ? "signup" : "login"}">${errorBox()}${signup ? field("name", "Your name", "text", "", 'required maxlength="80" autocomplete="name"') : ""}${field("email", "Email address", "email", "", 'required maxlength="254" autocomplete="email"')}${field("password", "Password", "password", "", `required ${signup ? 'minlength="12"' : ""} maxlength="256" autocomplete="${signup ? "new-password" : "current-password"}"${signup ? ' aria-describedby="password-hint"' : ""}`)}${signup ? '<p class="hint field-hint" id="password-hint">At least 12 characters.</p>' : ""}<button class="full">${signup ? "Create an account" : "Sign in"}</button><p class="hint switch">${signup ? 'Already have an account? <a href="#login">Sign in</a>' : 'No account yet? <a href="#signup">Create one</a>'}</p>${!signup && state.mailOn ? '<p class="hint"><a href="#forgot">Forgotten your password?</a></p>' : ""}</form></div>`;
}

function forgot() {
  header("login");
  $("#main").innerHTML =
    `<div class="narrow"><h1>Reset your password</h1>${state.mailOn ? `<p class="muted">Enter the email address you signed up with. If it has a Gifty account, you’ll get a link to choose a new password. The link works for one hour.</p><form class="sheet" data-form="forgot">${errorBox()}${field("email", "Email address", "email", "", 'required maxlength="254" autocomplete="email"')}<button class="full">Send reset link</button></form>` : "<p>This Gifty server doesn’t send emails, so passwords can’t be reset here. Ask the person who runs it.</p>"}</div>`;
}

function resetPage(token) {
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1>Choose a new password</h1><form class="sheet" data-form="reset">${errorBox()}<input type="hidden" name="token" value="${esc(token)}">${field("password", "New password", "password", "", 'required minlength="12" maxlength="256" autocomplete="new-password" aria-describedby="password-hint"')}<p class="hint field-hint" id="password-hint">At least 12 characters. Saving signs you out on your other devices.</p><button class="full">Save password</button></form></div>`;
}

async function verifyPage(token, version) {
  await api("verify", { token });
  if (version !== state.routeVersion) return;
  if (state.user) state.user.verified = true;
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1>Email address confirmed</h1><p>Gifty will email you when it’s time to draw a name and before each exchange. You can turn this off on your account page.</p><a class="button" href="#${state.user ? "exchanges" : "login"}">${state.user ? "Go to exchanges" : "Sign in"}</a></div>`;
}

function unsubscribePage(token) {
  header();
  $("#main").innerHTML =
    `<div class="narrow"><h1>Stop exchange emails</h1><p>You won’t get emails when it’s time to draw a name or before exchanges. You’ll still get emails you ask for, such as password resets.</p><form data-form="unsubscribe">${errorBox()}<input type="hidden" name="token" value="${esc(token)}"><button>Stop emails</button></form></div>`;
}

function initAuthentication() {
  onSubmit(
    ["signup", "login", "access", "forgot", "reset", "unsubscribe"],
    async (form, data) => {
      switch (form.dataset.form) {
        case "signup":
        case "login":
          state.user = await api(
            form.dataset.form,
            form.dataset.form === "signup"
              ? { ...data, invite: sessionStorage.getItem("giftyInvite") || "" }
              : data,
          );
          state.hasAccess = true;
          {
            const code = sessionStorage.getItem("giftyInvite");
            let joined = null;
            if (code) {
              try {
                joined = await api("join", { code });
                sessionStorage.removeItem("giftyInvite");
                sessionStorage.setItem("giftyJoined", joined.id);
              } catch {}
            }
            location.hash = joined
              ? "exchange/" + joined.id
              : code
                ? "invite/" + code
                : sessionStorage.getItem("giftyFriend")
                  ? "friend/" + sessionStorage.getItem("giftyFriend")
                  : "exchanges";
            if (joined) notify(`You’ve joined ${joined.name}.`);
            else if (form.dataset.form === "signup" && state.mailOn)
              notify(
                "Account created. Check your email for a confirmation link.",
              );
          }
          break;
        case "access":
          await api("access", { code: data.code });
          state.hasAccess = true;
          if (location.hash === "#signup" || location.hash === "#login")
            await route();
          else location.hash = "signup";
          break;
        case "forgot":
          await api("reset/request", data);
          form.outerHTML =
            '<div class="sheet" role="status"><h2>Check your email</h2><p>If that address has a Gifty account, a reset link is on its way. It can take a few minutes and may land in spam.</p></div>';
          break;
        case "reset":
          state.user = await api("reset", data);
          state.hasAccess = true;
          location.hash = "exchanges";
          notify("Password changed. You’re signed in.");
          break;
        case "unsubscribe":
          await api("unsubscribe/" + encodeURIComponent(data.token), {});
          form.outerHTML =
            '<p class="notice" role="status">Done. You can turn exchange emails back on from your account page.</p>';
          break;
      }
    },
  );
}

export {
  verifyPage,
  forgot,
  resetPage,
  unsubscribePage,
  gatePage,
  auth,
  home,
  initAuthentication,
};
