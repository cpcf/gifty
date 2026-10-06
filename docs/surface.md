# Gifty app — Operate

Every screen in `web/` is one Operate surface: people come to get a task done (invite, draw, choose a gift), and the visual world supports that rather than leading. This page summarises the design direction. The working brief Impeccable reads is `.impeccable/surfaces/web-index-html.md`; the implemented system, with tokens, is in [DESIGN.md](../DESIGN.md).

## Direction: fete raffle

Chosen on 6 October 2026, replacing an earlier plum, lilac and lime look and then an internal-mail look.

An exchange is a raffle at a village fete. People are raffle tickets and your recipient comes out of a drum. Gifty is for the owner's friends and family, so it is built to be fun and useful, not to convert anyone. It rejects the festive red-and-green Secret Santa look and the pastel-plus-acid startup look.

- **Palette:** a warm grey ground, white sheets for working surfaces and one near-black ink for text, rules and the only filled button. Five flat paper colours (yellow, pink, blue, mint, orange) are handed to tickets in turn and mean nothing else. No shadows, gradients or texture.
- **Type:** Public Sans, self-hosted, one family, with tabular figures for numbers. Small uppercase labels name a value beside them and never sit above headings as slogans.
- **Tickets:** each person is a ticket with a numbered stub and a dashed perforation. An empty slot is a dashed open ticket. Someone who has their gift has a torn stub and the words "Got it". Tickets appear only on the people list, on your reveal ticket and in the home page example.
- **The drum:** an illustration with a cosmetic "Draw your name" step. The draw is already made at lock-in; the drum spins, a folded ticket slides out and unfolds. Joining an exchange drops a folded name chip into the drum.
- **The reveal ticket:** covered by default (paper colour, a solid ink bar and "??"), with no name anywhere in the page while covered. Press and hold to show it; by keyboard, activate to show and activate again to hide.
- **Copy:** plain and functional. Say what things are and what happens, especially for actions that cannot be undone. Playfulness comes from the objects, not jokes.

## Screens

- **Signed-out home:** a short explanation, the rules of a draw, and a worked example reveal ticket. When an access code is configured, visitors without access see a short invite-only page instead; invitation links bypass it.
- **Exchanges:** a table with fixed date, limit, people and status columns, which becomes stacked rows on phones.
- **Exchange:** facts strip, the drum box before you draw (then your reveal ticket), your recipient's wish list, the note, the ticket list of people (which shows who has their gift, never who it is for) and a short summary of email reminders with editors behind "Change reminders". Invitation and draw actions, or your wish-list prompt, sit in a side column that comes first on phones while the exchange is open.
- **Invitation:** who sent it, the date, the limit and how many have joined.
- **Wish list:** ideas grouped into "Every exchange" and one section per exchange; each idea chooses who can see it.
- **Account:** email on or off, and reminders that apply to every exchange unless set for one.

## Constraints

Standard web controls throughout, 44px minimum touch targets, visible focus, motion only when the visitor has not asked for reduced motion, and WCAG 2.1 AA as checked by axe in the browser tests. Phones are the main device.
