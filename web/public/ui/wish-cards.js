import { esc } from "./dom.js";

function wishesHTML(
  wishes,
  editable = false,
  claimable = false,
  audience = () => "",
) {
  return `<ul class="wishes">${(wishes || []).map((w) => `<li>${(w.images || []).length ? `<button type="button" class="wish-photo" data-action="view-photo" data-id="${w.id}" aria-label="See ${w.images.length > 1 ? `all ${w.images.length} photos` : "a larger photo"} of ${esc(w.title)}"><img src="${esc(w.images[0])}" alt="" loading="lazy">${w.images.length > 1 ? `<span class="wish-count" aria-hidden="true">${w.images.length}</span>` : ""}</button>` : ""}<div class="wish-body"><div class="wish-head"><h3>${esc(w.title)}</h3>${editable ? `<div class="actions">${w.status ? `<button class="quiet" data-action="unsort-wish" data-id="${w.id}" aria-label="Put back: ${esc(w.title)}">Put back</button>` : `<button class="quiet" data-action="edit-wish" data-id="${w.id}" aria-label="Edit ${esc(w.title)}">Edit</button><button class="quiet" data-action="sort-wish" data-id="${w.id}" aria-label="Done with: ${esc(w.title)}">Done with it</button>`}<button class="quiet" data-action="delete-wish" data-id="${w.id}" aria-label="Remove ${esc(w.title)}">Remove</button></div>` : ""}</div>${editable && w.status ? `<p class="tag">${w.status === "got" ? "You’ve got this" : "You don’t want this any more"}</p>` : ""}${w.price ? `<p class="price">${esc(w.price)}</p>` : ""}${w.note ? `<p>${esc(w.note)}</p>` : ""}${w.url ? `<a href="${esc(w.url)}" target="_blank" rel="noopener noreferrer">Open link<span class="muted"> (new tab)</span></a>` : ""}${editable && !w.status ? `<p class="aud">${esc(audience(w))}</p>` : ""}${claimable ? (typeof claimable === "function" ? claimable : claimHTML)(w) : ""}</div></li>`).join("")}</ul>`;
}

// What the person buying for someone can do with one of their ideas: say they are getting it, so a giver in another exchange doesn't buy it too. The owner is never told.
function claimHTML(w) {
  const t = esc(w.title);
  if (w.status)
    return `<div class="claim"><p class="tag">${w.status === "got" ? "They’ve got this now." : "They don’t want this any more."} Check before you buy it.</p><button type="button" class="quiet" data-action="claim" data-id="${w.id}" data-on="false" aria-label="Stop tracking ${t}">Stop tracking</button></div>`;
  if (w.claim === "mine")
    return `<div class="claim"><p class="tag">You’re getting this.</p><button type="button" class="quiet" data-action="claim" data-id="${w.id}" data-on="false" aria-label="Undo: you’re getting ${t}">Undo</button></div>`;
  if (w.claim === "other")
    return `<div class="claim"><p class="tag">Someone else is getting this one.</p></div>`;
  return `<div class="claim"><button type="button" class="secondary small" data-action="claim" data-id="${w.id}" data-on="true" aria-label="I’m getting this: ${t}">I’m getting this</button></div>`;
}

export { wishesHTML };
