import {
  invite,
  newExchange,
  detail,
  exchanges,
  initExchanges,
} from "./features/exchanges.js";
import {
  verifyPage,
  forgot,
  resetPage,
  unsubscribePage,
  gatePage,
  auth,
  home,
  initAuthentication,
} from "./features/authentication.js";
import {
  friendLinkSignedOut,
  friendsPeople,
  friendPage,
  friendsCalendar,
  friendLink,
  getFriendPageData,
  initFriends,
} from "./features/friends.js";
import { wishes, wishModal, initWishes } from "./features/wishes.js";
import { account, adminPage, initAccounts } from "./features/accounts.js";
import { configureRouter, initRouter } from "./core/router.js";
import { initReminders } from "./features/reminders.js";
import { initDialog } from "./ui/dialog.js";
import { initPhotos } from "./features/photos.js";
import { initForms } from "./ui/forms.js";
import { api } from "./core/api.js";
import { state } from "./core/session.js";
import { header } from "./ui/navigation.js";
import { $, esc } from "./ui/dom.js";
import { route } from "./core/router.js";
configureRouter({
  invite,
  verifyPage,
  forgot,
  resetPage,
  unsubscribePage,
  friendLinkSignedOut,
  gatePage,
  auth,
  home,
  wishes,
  wishModal,
  friendsPeople,
  friendPage,
  friendsCalendar,
  friendLink,
  account,
  adminPage,
  newExchange,
  detail,
  exchanges,
  friendPageData: getFriendPageData,
});
initFriends();
initAuthentication();
initReminders();
initExchanges();
initWishes();
initAccounts();
initDialog();
initRouter();
initPhotos();
initForms();

(async () => {
  try {
    const c = await api("config");
    state.mailOn = c.mail;
    state.gated = c.gated;
    state.hasAccess = c.access;
  } catch {}
  try {
    state.user = await api("me");
  } catch (err) {
    if (err.status !== 401) {
      header();
      $("#main").innerHTML =
        `<p role="alert">${esc(err.message)}</p><button data-action="retry">Try again</button>`;
      return;
    }
  }
  await route();
})();
