# Gifty app — Operate

Every screen in `web/` is one Operate surface: people come to get a task done (invite, draw, choose a gift), and the visual world supports that rather than leading. This page summarises the design direction and the screens.

## Direction: fete raffle

An exchange is a raffle at a village fete. People are raffle tickets and your recipient comes out of a raffle machine. Gifty is for the owner's friends and family, so it is built to be fun and useful, not to convert anyone. It avoids the festive red-and-green Secret Santa look and the pastel-plus-acid startup look.

- **Palette:** a warm grey ground, white sheets for working surfaces and one near-black ink for text, rules and the only filled button. Five flat paper colours (yellow, pink, blue, mint, orange) are handed to tickets in turn and mean nothing else. No shadows, gradients or texture.
- **Type:** Public Sans, self-hosted, one family, with tabular figures for numbers. Small uppercase labels name a value beside them and never sit above headings as slogans.
- **Tickets:** each person is a ticket with a numbered stub and a dashed perforation. An empty slot is a dashed open ticket. Someone whose gift is ready to give has a torn stub and the words "Gift ready". Tickets appear only on the people list, on your reveal ticket and in the home page example.
- **The raffle machine:** a lamp-and-window machine with a cosmetic "Draw a name" step. The draw is already made when entries close; the window's reel runs through everyone and lands, then a folded ticket ratchets out of the slot and unfolds. Joining an exchange drops a folded name chip into the machine.
- **The reveal ticket:** covered by default (paper colour, a solid ink bar and "??"), with no name anywhere in the page while covered. Press and hold to show it; by keyboard, activate to show and activate again to hide.
- **Copy:** plain and functional. Say what things are and what happens, especially for actions that cannot be undone. Playfulness comes from the objects, not jokes.

## Screens

- **Signed-out home:** a short explanation, the rules of a draw, and a worked example reveal ticket. When an access code is configured, visitors without access see a short invite-only page instead, including in place of the sign-in page; invitation links let a guest sign up without the code.
- **Exchanges:** a table with fixed date, limit, people and status columns, which becomes stacked rows on phones. An archived exchange offers its organiser a Delete control on the row.
- **Exchange:** facts strip, the machine box before you draw (then your reveal ticket), your recipient's wish list, the note, the ticket list of people (which shows whose gift is ready, never who it is for) and a short summary of email reminders with editors behind "Change reminders". Invitation and draw actions, or your wish-list prompt, sit in a side column that comes first on phones while the exchange is open. An archived exchange ends with a Delete control for its organiser.
- **Invitation:** who sent it, the date, the limit and how many have joined.
- **Wish list:** ideas grouped into "Every exchange" and one section per exchange; each idea chooses who can see it. An idea can carry up to five photos (added by choosing, dropping or pasting; the first is the cover, shown as a square thumbnail with a count that opens a viewer to step through them all), a rough price, a link and a note; the person buying for you sees all of it.
- **Account:** email on or off, reminders that apply to every exchange unless set for one, and account deletion (password required). Deleting removes the person from open exchanges; in a drawn exchange they stay as an empty “Deleted account” so the draw still adds up. Organisers must archive exchanges that have other people first.
- **Admin** (only for addresses in `GIFTY_ADMINS` that are confirmed): lists accounts and exchanges and can delete either. It never shows assignments, wish lists or invitation links.

## Constraints

Standard web controls throughout, 44px minimum touch targets, visible focus, motion only when the visitor has not asked for reduced motion, and WCAG 2.1 AA as checked by axe in the browser tests. Phones are the main device.
