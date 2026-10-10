import { state } from "./session.js";
import { clearToast, notify, toastRoute, toastHash } from "../ui/toast.js";
import { $, esc } from "../ui/dom.js";
import { header } from "../ui/navigation.js";

async function route() {
  const version = ++state.routeVersion;
  if (version > toastRoute + 1 || location.hash !== toastHash) clearToast();
  $("#header [aria-current]")?.removeAttribute("aria-current");
  const hash = location.hash.slice(1) || "home";
  const [page, id, extra] = hash.split("/");
  $("#dialog").close();
  $("#main").innerHTML = '<p class="loading">Loading…</p>';
  try {
    if (page === "invite" && id) await views.invite(id, version);
    else if (page === "verify" && id) await views.verifyPage(id, version);
    else if (page === "forgot") views.forgot();
    else if (page === "reset" && id) views.resetPage(id);
    else if (page === "unsubscribe" && id) views.unsubscribePage(id);
    else if (page === "friend" && id && !state.user)
      await views.friendLinkSignedOut(id, version);
    else if (!state.user) {
      if (
        state.gated &&
        !state.hasAccess &&
        !(page === "signup" && sessionStorage.getItem("giftyInvite"))
      )
        views.gatePage();
      else if (page === "login") views.auth(false);
      else if (page === "signup") views.auth(true);
      else views.home();
    } else if (page === "wishes") {
      await views.wishes(version, id);
      if (extra === "add" && version === state.routeVersion) views.wishModal();
    } else if (page === "friends") {
      if (id === "people") await views.friendsPeople(version);
      else if (id) await views.friendPage(id, version);
      else await views.friendsCalendar(version);
    } else if (page === "friend" && id) await views.friendLink(id, version);
    else if (page === "account") views.account();
    else if (page === "admin" && state.user.admin)
      await views.adminPage(version);
    else if (page === "new") await views.newExchange(version);
    else if (page === "exchange" && id) await views.detail(id, version);
    else await views.exchanges(version);
    if (version === state.routeVersion) {
      const title =
        page === "exchange" && state.current
          ? state.current.name
          : page === "friends"
            ? id && id !== "people" && views.friendPageData()
              ? views.friendPageData().name
              : "Friends"
            : page === "friend"
              ? "Friend link"
              : page === "wishes"
                ? "Wish list"
                : page === "new"
                  ? "New exchange"
                  : page === "account"
                    ? "Account"
                    : page === "admin"
                      ? "Admin"
                      : page === "forgot"
                        ? "Reset your password"
                        : page === "reset"
                          ? "Choose a new password"
                          : page === "verify"
                            ? "Email confirmed"
                            : page === "unsubscribe"
                              ? "Stop emails"
                              : state.user
                                ? "Exchanges"
                                : page === "signup"
                                  ? "Create an account"
                                  : page === "login"
                                    ? "Sign in"
                                    : "";
      document.title = title ? title + " · Gifty" : "Gifty";
      window.scrollTo(0, 0);
      $("#main").focus({ preventScroll: true });
    }
  } catch (err) {
    if (version !== state.routeVersion) return;
    if (err.status === 401) {
      state.user = null;
      location.hash = "login";
      return;
    }
    header();
    const gone = err.status === 404,
      home = state.user ? "#exchanges" : "#home",
      homeLabel = state.user ? "Go to exchanges" : "Go to the home page";
    $("#main").innerHTML =
      `<div class="narrow"><h1>${gone ? "Not found" : "This page couldn’t be loaded"}</h1><p role="alert">${esc(err.message)}</p>${gone ? '<p class="muted">If you were sent an invitation link, open that link instead.</p>' : ""}<div class="buttons">${gone ? `<a class="button" href="${home}">${homeLabel}</a>` : `<button data-action="retry">Try again</button><a class="button secondary" href="${home}">${homeLabel}</a>`}</div></div>`;
  }
}

let views;
function configureRouter(entries) {
  views = entries;
}

function initRouter() {
  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "retry":
          await route();
          break;
      }
    } catch (err) {
      notify(err.message);
    }
  });

  window.addEventListener("hashchange", route);
}

export { route, configureRouter, initRouter };
