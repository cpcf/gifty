import { esc, $ } from "./dom.js";

function field(name, label, type = "text", value = "", extra = "") {
  return `<div class="field"><label for="${name}">${label}</label><input id="${name}" name="${name}" type="${type}" value="${esc(value)}" ${extra}>${type === "password" ? `<label class="check show-pw"><input type="checkbox" data-show="${name}"><span>Show ${label.toLowerCase()}</span></label>` : ""}</div>`;
}

let errorId = 0;

function errorBox() {
  return `<div class="form-error" role="alert" id="err${++errorId}"></div>`;
}

const missing = {
  "Your name": "Enter your name.",
  "Email address": "Enter your email address.",
  Password: "Enter your password.",
  "New password": "Enter a new password.",
  "Exchange name": "Enter a name for the exchange.",
  "Exchange date": "Choose a date for the exchange.",
  "Spending limit per person": "Enter a spending limit.",
  "What is it?": "Say what the idea is.",
  "Access code": "Enter the access code.",
  "Your question": "Write your question first.",
  "Your reply": "Write your reply first.",
};

function problemText(f) {
  const v = f.validity,
    label = (f.labels?.[0]?.textContent || f.getAttribute("aria-label") || "")
      .replace(/\(optional\)/, "")
      .trim();
  if (v.valueMissing)
    return missing[label] || `Fill in ${label.toLowerCase() || "this field"}.`;
  if (v.typeMismatch)
    return f.type === "email"
      ? "Enter an email address like name@example.com."
      : "Enter the whole link, starting with https://.";
  if (v.tooShort) return `Use at least ${f.minLength} characters.`;
  if (v.badInput) return "Enter a number.";
  if (v.rangeUnderflow)
    return f.type === "date"
      ? "Choose today or a later date."
      : `Enter ${f.min} or more.`;
  if (v.rangeOverflow) return `Enter ${f.max} or less.`;
  if (v.stepMismatch)
    return f.step === "0.01"
      ? "Use no more than two decimal places."
      : "Enter a whole number.";
  return f.validationMessage;
}

// The browser's own bubbles are replaced by the form's error box, listing every problem; the first problem field takes focus.
let invalid = null;

function onSubmit(kinds, handle) {
  document.addEventListener("submit", async (event) => {
    const form = event.target.closest("form[data-form]");
    if (!form || !kinds.includes(form.dataset.form)) return;
    event.preventDefault();
    const button = form.querySelector('button:not([type="button"])');
    if (button.disabled) return;
    button.disabled = true;
    form.setAttribute("aria-busy", "true");
    const error = $(".form-error", form);
    error.textContent = "";
    const data = Object.fromEntries(new FormData(form));
    try {
      await handle(form, data);
    } catch (err) {
      error.textContent = err.message;
      error.scrollIntoView({ block: "nearest" });
    } finally {
      button.disabled = false;
      form.removeAttribute("aria-busy");
    }
  });
}

function initForms() {
  document.addEventListener("change", async (event) => {
    const box = event.target.closest(
      'input[type="checkbox"],input[type="radio"]',
    );
    if (!box) return;
    if (box.dataset.show) {
      document.getElementById(box.dataset.show).type = box.checked
        ? "text"
        : "password";
      return;
    }
  });

  document.addEventListener(
    "invalid",
    (event) => {
      event.preventDefault();
      const f = event.target,
        form = f.form;
      f.setAttribute("aria-invalid", "true");
      if (!form) return;
      if (!invalid || invalid.form !== form) {
        invalid = { form, fields: [] };
        setTimeout(() => {
          const { form, fields } = invalid;
          invalid = null;
          const box = $(".form-error", form);
          if (box) {
            const msgs = fields.map(problemText);
            box.innerHTML =
              msgs.length === 1
                ? esc(msgs[0])
                : `<p>Fix these and try again:</p><ul>${msgs.map((m) => `<li>${esc(m)}</li>`).join("")}</ul>`;
            fields.forEach((f) => {
              const ids = (f.getAttribute("aria-describedby") || "")
                .split(" ")
                .filter(Boolean);
              if (!ids.includes(box.id))
                f.setAttribute("aria-describedby", [...ids, box.id].join(" "));
            });
          }
          fields[0].focus();
        });
      }
      invalid.fields.push(f);
    },
    true,
  );

  document.addEventListener("input", (event) => {
    const f = event.target;
    if (f.getAttribute?.("aria-invalid")) {
      f.removeAttribute("aria-invalid");
      if (f.form && !f.form.querySelector("[aria-invalid]")) {
        const box = $(".form-error", f.form);
        if (box) box.textContent = "";
      }
    }
  });
}

export { field, errorBox, onSubmit, initForms };
