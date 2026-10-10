import { clearToast, notify } from "./toast.js";
import { $, esc } from "./dom.js";
import { errorBox } from "./forms.js";

function modal(title, content) {
  clearToast();
  $("#dialog").innerHTML =
    `<h2 id="dialog-title">${title}</h2>${content}<button class="quiet close" data-action="close">Close</button>`;
  $("#dialog").showModal();
}

function confirmAction(title, body, action, id = "") {
  modal(
    esc(title),
    `<p>${esc(body)}</p><form data-form="confirm" data-operation="${action}" data-id="${id}">${errorBox()}<div class="dialog-actions"><button type="button" class="secondary" data-action="close" autofocus>Cancel</button><button>${esc(title.replace(/\?$/, ""))}</button></div></form>`,
  );
}

function initDialog() {
  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "close":
          $("#dialog").close();
          break;
      }
    } catch (err) {
      notify(err.message);
    }
  });
}

export { modal, confirmAction, initDialog };
