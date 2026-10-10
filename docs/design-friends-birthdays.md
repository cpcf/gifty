# Design: friends, birthdays, shared lists, exchange kinds

Status: implemented (see the end of this page for what changed in the build). Mockups are in `docs/mockups/` and use the real `web/public/styles/style.css` plus `proposed.css` (the new components). Open any of them in a browser; resize to see the phone layout.

| Mockup | Shows |
| --- | --- |
| `calendar.html` | Friends, Calendar view |
| `friends.html` | Friends, People view, a request, the friend link |
| `friend-list.html` | A friend's list with the three claim states |
| `my-list.html` | Your own list and the new "Who can see this" choice |
| `account.html` | Birthday and birthday reminders |
| `exchange-types.html` | Kind picker, and a white elephant exchange after the draw |

## What changes

1. **Friends.** People become friends by link, with both sides agreeing. Friends see each other's birthday and the ideas you show to friends.
2. **Birthdays and a calendar.** A birthday on your account, and a month calendar of your friends' birthdays.
3. **One wish list, three audiences.** Each idea is shown to friends, to whoever buys for you in an exchange, to both, or to neither. Friends can mark "I'm getting this". The owner never sees which ideas are marked.
4. **Exchange kinds.** Secret Santa stays the default. White elephant is added. Group gift is designed but built last.

## Decisions I made (say if any is wrong)

- **Adding friends is by link, never by search.** There is no directory of accounts and no lookup by email. This matches "invitations are shared by hand" and the invite-only mode. The link opener sends a request and you accept, because a forwarded link must not hand over your list.
- **The friend link does not get anyone past the access code.** An exchange invite does; a friend link is permanent until replaced, so it should not.
- **Friends is its own nav item** with two views, Calendar and People. Nav becomes Exchanges, Friends, Wish list, Account. It still fits at 340px with the existing wrapping.
- **Year of birth is optional.** With it, friends see "Turns 34". Without it, they never see an age. Feb 29 shows on 28 Feb in common years.
- **Exchange dates are not on the calendar.** You asked for birthdays; adding events would blur it.
- **Weeks start on Monday.**

## Wish list and claims

The existing list gains a second audience instead of a second list. In the idea form, "Who can see this" becomes two independent choices: a Friends checkbox, and the existing exchange choice (every exchange, or only some). Ticking neither makes the idea private. Existing ideas stay as they are, visible to givers only, so nothing is exposed by upgrading.

**Marking off.** A friend opening Anna's list sees each idea in one of three states, using the claim markup that exists today:

- Open: a secondary "I'm getting this" button.
- Yours: "You're getting this." with Undo.
- Taken: "Someone has this." with no button.

A claim is shared across audiences. If a friend marks an idea, whoever buys for Anna in an exchange sees "Someone has this", and the reverse. That is the point: no double gifts however the giver came to the list.

**Hiding it from the owner.** The owner's API response never contains a claim, as now. The page says so in words above the list: Anna is never told and can't see which ideas are marked. The owner's own list says "Friends can mark ideas they're getting. You never see which."

**After the birthday.** Claims on friend-only ideas clear three days after the birthday, so next year's list starts fresh. For fourteen days after their birthday the owner sees the same note on every list, claimed or not: "Your birthday was on 25 October. Mark what you've got, or no longer want, so the list stays useful." The note is uniform, so it tells them nothing about what was marked. Marking "Got it" hides the idea, as today.

**Limits.** Friends can't see who marked an idea. Coordinating a shared gift happens in person; the Group gift kind is the in-app answer to that, later.

## Calendar

Month grid, Monday first, with the month in the page and the previous and next months as buttons named for the months ("September", "November"). There are no arrow characters, in keeping with the system. "Back to this month" appears when you are elsewhere.

- **Desktop:** the grid in the main column. In the 340px side column, a panel for the chosen day (names, age if known, a link to their list) and a "Coming up" list of the next 30 days. Each row of Coming up is a 48px link.
- **Phone:** the grid has seven 46px columns, so names don't fit. Each birthday is a 6px stripe in the cell, up to three. The chosen day's panel sits below the grid and names the people in words, so colour alone never carries the meaning.
- Every day is one button, at least 56px tall on a phone and 112px on desktop. Chips inside are visual; the links are in the day panel, which keeps every target above 44px.
- Today is the day number in an ink square. The chosen day has a 2px inset ink border. Days outside the month are on the ground colour with no chips.
- Your own birthday is an outlined chip reading "You".
- Colour: a friend's chip takes the next paper colour in the order the friendship was made, and keeps it. The same chip colour never means anything else.

**Calendar file.** A first version downloads a `.ics` of friends' birthdays as yearly repeating all-day events. A subscribable feed is possible later, but it needs a secret URL, so it is not in the first build.

## Friends and People

- **People view:** a plain ruled table (name, birthday, next, list), the same stacked-row table the exchanges list uses on phones. Not tickets: tickets stay on exchange people lists.
- **Request:** a bordered notice at the top, "Jo asked to be friends. You would see each other's birthdays and lists." Accept is the one ink button; Not now is secondary.
- **Friend link:** a side panel in the style of the invitation panel: read-only field, Copy link, Share on phones, and a quiet Replace link. Replacing stops the old link and leaves existing friends alone.
- **Exchanges page:** a plain notice for birthdays in the next 14 days (three at most, then "and 2 more"), linking to their list.
- **Empty state:** "No friends yet. Send your friend link. When someone opens it and you accept, you can see each other's birthdays and lists."

## Exchange kinds

The create form leads with "Kind of exchange", three plain radio rows with a sentence each.

**Secret Santa:** unchanged.

**White elephant:** everyone brings a wrapped gift; nobody is assigned a person. Locking in assigns a random pick order instead of recipients.
- Same flow and same machine: "Draw a number" runs the reel through numbers and lands on yours. Your ticket reads "You pick 4th".
- Nothing is secret, so the ticket is not covered. The order is public to members, and the people list is sorted by pick number, with the stub showing that number in place of join order. Colours stay with each person.
- "Mark gift as ready" and its torn stub work as today. Reminders, calendar file and archiving are unchanged.
- Wish lists, messages, keep apart and reveal day don't apply and are hidden for this kind.
- A "Steals per gift" fact (default 3) is shown to everyone. It is a rule for the room and is not enforced.

**Group gift (built last):** friends give one gift to a friend who is not a member and never sees the exchange. Members see that friend's friend-audience list and the claim states above. Choosing it adds a "Who is it for?" select of your friends. Members must all be friends with that person. It needs friends first and is the only kind that needs a new hidden-from-someone rule, so it deserves its own review.

## Components to add (all flat, no shadows)

`.views`, `.cal-bar`, `.cal`, `.day`, `.bd` (birthday chip), `.who-list`, `.soon`, `.request`, `.kinds`, `.aud` (see `docs/mockups/proposed.css`).

`DESIGN.md` needs three amendments, which I haven't made without your say:
1. Paper colours may also mark a friend on the calendar (chips and stripes).
2. A "Views switch" component: nav-style links with an ink underline for the current one.
3. The exchange people list may be ordered by pick number for white elephant.

## Data and privacy

- `User`: `Birthday` (month, day, optional year), `BirthdayHidden`, `Friends []string`, pending requests in and out, a friend link token. Requests are capped per person so the link can't be used for spam.
- `Wish`: `Friends bool`. Friend claims use the existing `ClaimedBy`, with `ClaimedIn` set to a fixed marker so they are released when friendship ends or an idea stops being shown to friends.
- `Exchange`: `Kind` (empty means Secret Santa), `Steals`, and for group gift a `For` user.
- A friend is sent: name, the birthday if shown, ideas with the Friends audience, and claims as mine or other. Never an email address, never idea scopes, and never claims to the owner. Photos follow the same rule as ideas: served to friends only for friend-audience ideas.
- Ending a friendship, or deleting an account, releases claims in both directions, as exchange claims are released today.
- The friend link is a 256-bit token like the invitation link, and its endpoints get the same rate limits. Add these to `docs/security.md` and cover each in `main_test.go`, including "owner never receives a claim" for friend ideas.
- Birthday emails name the friend and the date and never mention claims, in line with "emails never name a recipient".

## Build order

1. Friends: link, request, accept, remove, People view. 
2. Birthday on the account, then the calendar and Coming up.
3. Friend audience on ideas, the friend's list view and claims, then the post-birthday note and clearing.
4. Birthday reminder emails and the `.ics` file.
5. White elephant.
6. Group gift.

Each step ships on its own.

## Not included

Searching for people, invitations by email, seeing who marked an idea, money tracking for shared gifts, a subscribable calendar feed, and non-birthday events on the calendar.

## As built

Where the build differs from the proposal above:

- **Birthday emails** use a fixed schedule (a week before, and on the day) with one on/off switch on the account page, not an editable schedule. The account mockup's "Change reminders" row became that switch.
- **The calendar's "Share friend link"** is a link to the People view, where the link lives.
- **Marks** in the interface read "Someone else is getting this one.", as for exchanges, rather than "Someone has this."
- **Group gift invitations**: the public invitation preview shows the exchange's name to anyone holding the link, including the person the gift is for. The create form says so. Joining is refused for them with the same message as a closed invitation.
- **The friend link** is made when an account is created (and, for older accounts, the first time the Friends page is opened).
- **Ideas only for friends** and **ideas for nobody** are expressed with two flags on the idea (`friends`, `noExchanges`), so every idea saved before this change keeps its old audience.

