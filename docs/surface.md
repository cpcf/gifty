# Gifty app — Operate

Every screen in `web/` is one Operate surface: people come to get a task done (invite, draw, choose a gift), and the visual world supports that rather than leading. This page summarises the design direction. The working brief Impeccable reads is `.impeccable/surfaces/web-index-html.md`; the implemented system, with tokens, is in [DESIGN.md](../DESIGN.md).

## Direction: internal mail

Chosen on 6 October 2026, replacing an earlier plum, lilac and lime look whose colours and slogan copy read as generic.

An exchange is treated like a reusable office internal-mail envelope: a ruled grid of names that fills as people join, passed along once. The direction rejects both the festive red-and-green Secret Santa look and the pastel-plus-acid-accent startup look.

- **Palette:** flat manila buff for the header and page ground, white sheets for working surfaces, one green-black ink for text and rules. A single biro red is kept for one job only: the name of the person you are buying for. Errors use ink on a pale buff box, never a second red. No texture or decorative glyphs.
- **Type:** Public Sans, self-hosted, with tabular figures for dates and amounts. Small uppercase labels name values (DATE, SPENDING LIMIT, TO); they never sit above headings as slogans.
- **Signature features:** the participant list is a numbered, ruled table with empty rows showing how many more people are needed to draw. The recipient appears in red on a ruled TO line at the top of the exchange page.
- **Copy:** plain and functional. Say what things are and what happens, especially for actions that cannot be undone.

## Screens

- **Signed-out home:** a short explanation, the rules of a draw, and a worked example: the TO box one person sees after the draw. When an access code is configured, visitors without access see a short invite-only page instead; invitation links bypass it.
- **Exchanges:** a table with fixed date, limit, people and status columns, which becomes stacked rows on phones.
- **Exchange:** facts strip, TO line, the recipient's wish list for this exchange, the note, the participant grid (which ticks off who has their gift after the draw, never who it is for) and a short summary of email reminders with editors behind “Change reminders”, with invitation and draw actions, or your wish-list prompt, in a side column that comes first on phones while the exchange is open. The first view after the draw is the one animated moment: the TO slip arrives and the name is written in.
- **Invitation:** who sent it, the date, the limit and how many have joined.
- **Wish list:** ideas grouped into "Every exchange" and one section per exchange; each idea chooses who can see it.
- **Account:** email on or off, and reminders that apply to every exchange unless set for one.

## Constraints

Standard web controls throughout, 44px minimum touch targets, visible focus, and WCAG 2.1 AA as checked by axe in the browser tests. Phones are the main device.
