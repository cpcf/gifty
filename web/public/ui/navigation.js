import { $ } from "./dom.js";
import { state } from "../core/session.js";

function header(active = "") {
  $("#header").classList.toggle("long-nav", !!state.user);
  const cur = (p) => (active === p ? 'aria-current="page"' : "");
  $("#header").innerHTML =
    `<a class="wordmark" href="#${state.user ? "exchanges" : "home"}">Gifty</a><nav aria-label="Main">${state.user ? `<a href="#exchanges" ${cur("exchanges")}>Exchanges</a><a href="#friends" ${cur("friends")}>Friends</a><a href="#wishes" ${cur("wishes")}>Wish list</a><a href="#account" ${cur("account")}>Account</a>${state.user.admin ? `<a href="#admin" ${cur("admin")}>Admin</a>` : ""}` : `<a href="#login" ${cur("login")}>Sign in</a>${state.gated && !state.hasAccess ? "" : `<a href="#signup" ${cur("signup")}>Create an account</a>`}`}</nav>`;
}

export { header };
