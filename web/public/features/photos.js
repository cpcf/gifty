import { $, esc } from "../ui/dom.js";
import { plural } from "../ui/format.js";
import { state } from "../core/session.js";
import { getShownWishes } from "./friends.js";
import { modal } from "../ui/dialog.js";
import { notify } from "../ui/toast.js";

const readDataURL = (file) =>
  new Promise((res, rej) => {
    const fr = new FileReader();
    fr.onload = () => res(fr.result);
    fr.onerror = () => rej(fr.error);
    fr.readAsDataURL(file);
  });

// A chosen photo is resized to fit a 900px square and re-encoded as JPEG, so the data file stays small.
const MAX_PHOTOS = 5;

// A chosen photo is resized to fit a 900px square and re-encoded as JPEG, so the data file stays small.
const MAX_PHOTO_BYTES = 512 * 1024;

// A chosen photo is resized to fit a 900px square and re-encoded as JPEG, so the data file stays small.
const PHOTO_TYPES = ["image/jpeg", "image/png", "image/gif", "image/webp"];

async function photoData(file) {
  try {
    const bmp = await createImageBitmap(file, {
        imageOrientation: "from-image",
      }),
      scale = Math.min(1, 900 / Math.max(bmp.width, bmp.height)),
      c = document.createElement("canvas");
    c.width = Math.max(1, Math.round(bmp.width * scale));
    c.height = Math.max(1, Math.round(bmp.height * scale));
    c.getContext("2d").drawImage(bmp, 0, 0, c.width, c.height);
    bmp.close();
    const blob = await new Promise((r) => c.toBlob(r, "image/jpeg", 0.82));
    if (!blob) throw Error("unreadable");
    return { value: await readDataURL(blob), src: URL.createObjectURL(blob) };
  } catch {
    // No resizing pipeline: send a file the server accepts as it is, and say so if it is some other kind.
    if (!PHOTO_TYPES.includes(file.type))
      throw Error(
        `${file.name || "That file"} isn’t a photo this browser can read. Try a JPEG or PNG.`,
      );
    return { value: await readDataURL(file), src: URL.createObjectURL(file) };
  }
}

// The photo picker. Each form keeps its own list of photos in photoLists: ones already on the idea (kept
// by their /image/ URL) and ones just chosen (as resized data URIs), in the order they will be saved.
const photoLists = new WeakMap();

let photoSeq = 0;

function photoField(w) {
  const list = (w.images || []).map((url) => ({
    id: ++photoSeq,
    src: url,
    value: url,
  }));
  const html = `<div class="field photos" data-photos><span class="photos-label" id="photos-label">Photos <span class="optional">(optional)</span></span><ul class="photo-list" aria-labelledby="photos-label"></ul><button type="button" class="secondary photo-add" data-action="add-photo"></button><input type="file" id="photo" accept="${PHOTO_TYPES.join(",")}" multiple hidden><p class="hint photo-status" role="status"></p></div>`;
  queueMicrotask(() => {
    const el = $("[data-photos]", $("#dialog"));
    if (el) {
      photoLists.set(el, list);
      paintPhotos(el);
    }
  });
  return html;
}

function paintPhotos(el, announce, problem) {
  const list = photoLists.get(el),
    n = list.length,
    full = n >= MAX_PHOTOS;
  $(".photo-list", el).innerHTML = list
    .map((p, i) =>
      p.busy
        ? `<li class="photo-item"><div class="photo-frame busy"><span class="muted">Preparing…</span></div></li>`
        : `<li class="photo-item"><div class="photo-frame"><img src="${esc(p.src)}" alt="Photo ${i + 1}"><button type="button" class="photo-x" data-action="remove-photo" data-id="${p.id}" aria-label="Remove photo ${i + 1}"><svg viewBox="0 0 12 12" width="12" height="12" aria-hidden="true"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></button></div>${n > 1 ? (i === 0 ? '<span class="photo-cover">Cover photo</span>' : `<button type="button" class="quiet photo-make" data-action="cover-photo" data-id="${p.id}" aria-label="Make photo ${i + 1} the cover">Make cover</button>`) : ""}</li>`,
    )
    .join("");
  const add = $(".photo-add", el);
  add.disabled = full;
  add.textContent = n ? "Add more photos" : "Add photos";
  const hint = full
    ? `That’s the most, ${MAX_PHOTOS} photos.`
    : n > 1
      ? `${n} of ${MAX_PHOTOS}. The first is the one shown on your list.`
      : `Up to ${MAX_PHOTOS}. A photo helps the person buying for you.`;
  const status = $(".photo-status", el);
  status.textContent = announce || hint;
  status.classList.toggle("problem", !!problem);
}

async function addPhotos(el, files) {
  const list = photoLists.get(el),
    room = MAX_PHOTOS - list.length,
    take = files.slice(0, room),
    problems = [];
  if (files.length > room)
    problems.push(
      room
        ? `Only ${room} more fit, so the rest were left out.`
        : `That’s the most, ${MAX_PHOTOS} photos.`,
    );
  const added = take.map((file) => ({ id: ++photoSeq, busy: true, file }));
  list.push(...added);
  paintPhotos(el);
  let ok = 0;
  await Promise.all(
    added.map(async (p) => {
      try {
        const data = await photoData(p.file);
        if (data.value.length * 0.75 > MAX_PHOTO_BYTES) {
          URL.revokeObjectURL(data.src);
          throw Error(
            `${p.file.name || "That photo"} is too big even after shrinking.`,
          );
        }
        p.src = data.src;
        p.value = data.value;
        p.busy = false;
        ok++;
      } catch (err) {
        list.splice(list.indexOf(p), 1);
        problems.push(err.message || "A photo could not be read.");
      }
    }),
  );
  if (!el.isConnected) return;
  paintPhotos(
    el,
    problems.length
      ? problems.join(" ")
      : ok
        ? `Added ${plural(ok, "photo", "photos")}.`
        : "",
    problems.length > 0,
  );
}

// Photos can also be dropped onto the photo field or pasted into the dialog.
const photoFiles = (list) =>
  [...(list || [])].filter((f) => f.type.startsWith("image/") || !f.type);

function photosForForm(form) {
  return photoLists.get($("[data-photos]", form)) || [];
}

function initPhotos() {
  document.addEventListener("click", async (event) => {
    const b = event.target.closest("[data-action]");
    if (!b) return;
    const action = b.dataset.action;
    try {
      switch (action) {
        case "add-photo":
          $("#photo", b.closest("[data-photos]")).click();
          break;
        case "cover-photo": {
          const el = b.closest("[data-photos]"),
            list = photoLists.get(el),
            i = list.findIndex((p) => String(p.id) === b.dataset.id);
          if (i < 1) break;
          list.unshift(...list.splice(i, 1));
          paintPhotos(el, "Moved to the front. It’s now the cover.");
          $(".photo-x", el).focus();
          break;
        }
        case "show-photo": {
          const box = b.closest(".gallery"),
            img = $(".photo-full", box);
          img.src = b.dataset.src;
          img.alt = b.dataset.alt;
          box
            .querySelectorAll(".photo-thumb")
            .forEach((t) => t.setAttribute("aria-pressed", String(t === b)));
          break;
        }
        case "remove-photo": {
          const el = b.closest("[data-photos]"),
            list = photoLists.get(el),
            i = list.findIndex((p) => String(p.id) === b.dataset.id);
          if (i < 0) break;
          if (list[i].src.startsWith("blob:")) URL.revokeObjectURL(list[i].src);
          list.splice(i, 1);
          paintPhotos(
            el,
            list.length
              ? `Photo removed. ${plural(list.length, "photo", "photos")} left.`
              : "Photo removed.",
          );
          (
            [...el.querySelectorAll(".photo-x")][
              Math.min(i, list.length - 1)
            ] || $(".photo-add", el)
          ).focus();
          break;
        }
        case "view-photo": {
          const w =
            (state.user.wishes || []).find((w) => w.id === b.dataset.id) ||
            (state.current?.recipient?.wishes || []).find(
              (w) => w.id === b.dataset.id,
            ) ||
            getShownWishes().find((w) => w.id === b.dataset.id);
          if (w?.images?.length) {
            const n = w.images.length,
              alt = (i) =>
                `${w.title}${n > 1 ? `, photo ${i + 1} of ${n}` : ""}`;
            modal(
              esc(w.title),
              `<div class="gallery"><img class="photo-full" src="${esc(w.images[0])}" alt="${esc(alt(0))}">${n > 1 ? `<div class="photo-thumbs" role="group" aria-label="Photos">${w.images.map((u, i) => `<button type="button" class="photo-thumb" data-action="show-photo" data-src="${esc(u)}" data-alt="${esc(alt(i))}" aria-pressed="${i === 0}" aria-label="Show photo ${i + 1} of ${n}"><img src="${esc(u)}" alt=""></button>`).join("")}</div>` : ""}</div>`,
            );
          }
          break;
        }
      }
    } catch (err) {
      notify(err.message);
    }
  });

  // Choosing photos adds them to the form's list straight away, with a preview of each.
  document.addEventListener("change", (event) => {
    const input = event.target;
    if (
      !(input instanceof HTMLInputElement) ||
      input.type !== "file" ||
      input.id !== "photo"
    )
      return;
    const el = input.closest("[data-photos]");
    if (!el) return;
    const files = [...input.files];
    input.value = "";
    if (files.length) addPhotos(el, files);
  });

  document.addEventListener("dragover", (event) => {
    const el = event.target.closest?.("[data-photos]");
    if (!el || ![...(event.dataTransfer?.types || [])].includes("Files"))
      return;
    event.preventDefault();
    el.classList.add("dropping");
  });

  document.addEventListener("dragleave", (event) => {
    const el = event.target.closest?.("[data-photos]");
    if (el && !el.contains(event.relatedTarget))
      el.classList.remove("dropping");
  });

  document.addEventListener("drop", (event) => {
    const el = event.target.closest?.("[data-photos]");
    if (!el) return;
    event.preventDefault();
    el.classList.remove("dropping");
    const files = photoFiles(event.dataTransfer.files);
    if (files.length) addPhotos(el, files);
  });

  document.addEventListener("paste", (event) => {
    const el = $("[data-photos]", $("#dialog"));
    if (!el || !$("#dialog").open) return;
    const files = photoFiles(event.clipboardData?.files);
    if (files.length) {
      event.preventDefault();
      addPhotos(el, files);
    }
  });

  // Left and right arrows step through the photos in the viewer.
  document.addEventListener("keydown", (event) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    const box =
      event.target.closest?.(".gallery") ||
      (event.target === $("#dialog") ? $("#dialog .gallery") : null);
    if (!box) return;
    const thumbs = [...box.querySelectorAll(".photo-thumb")];
    if (thumbs.length < 2) return;
    const at = thumbs.findIndex(
        (t) => t.getAttribute("aria-pressed") === "true",
      ),
      next =
        thumbs[
          (at + (event.key === "ArrowRight" ? 1 : -1) + thumbs.length) %
            thumbs.length
        ];
    next.click();
    next.focus();
    event.preventDefault();
  });
}

export { photoField, photosForForm, initPhotos };
