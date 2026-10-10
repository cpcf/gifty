import { $ } from "./dom.js";
import { state } from "../core/session.js";

// A toast survives the navigation it announces (to the page it was raised on), then clears on any other.
function notify(text) {
  $("#toast").textContent = text;
  toastRoute = state.routeVersion;
  toastHash = location.hash;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(clearToast, 4500);
}

function clearToast() {
  clearTimeout(toastTimer);
  $("#toast").textContent = "";
}

let toastTimer,
  toastRoute = 0,
  toastHash = "";

export { notify, clearToast, toastRoute, toastHash };
