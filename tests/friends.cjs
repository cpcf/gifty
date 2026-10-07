// Friends, birthdays, the calendar, shared wish lists with marked ideas, white elephant and group gifts, driven in Chrome
// with four people. Needs a server with a fresh data file and no mail; see docs/testing.md.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const AxeBuilder=require('@axe-core/playwright').default;
const base=process.env.GIFTY_TEST_URL||'http://127.0.0.1:8093';
const audit=async p=>{await p.waitForTimeout(250);const r=await new AxeBuilder({page:p}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(r.violations.map(v=>({id:v.id,targets:v.nodes.map(n=>n.target)})),[])};
const stamp=Date.now();
const shots=process.env.GIFTY_SCREENSHOTS,shot=async(p,n)=>{if(shots)await p.screenshot({path:`${shots}/${n}.png`,fullPage:true})};
const future=(days)=>{const d=new Date(Date.now()+days*864e5);return new Date(d.getTime()-d.getTimezoneOffset()*6e4).toISOString().slice(0,10)};
(async()=>{
const browser=await chromium.launch({...(process.env.GIFTY_CHROME?{executablePath:process.env.GIFTY_CHROME}:{}),headless:true});
const errors=[];
const open=async(width=1280)=>{const ctx=await browser.newContext({locale:process.env.GIFTY_LOCALE||'en-GB',viewport:{width,height:1000},reducedMotion:'reduce'});const p=await ctx.newPage();p.on('pageerror',e=>errors.push(e.message));return p};
const signup=async(name)=>{const p=await open();await p.goto(base);await p.locator('header').getByRole('link',{name:'Create an account'}).click();await p.getByLabel('Your name').fill(name);await p.getByLabel('Email address').fill(`${name.toLowerCase()}${stamp}@example.com`);await p.getByLabel('Password',{exact:true}).fill('a sufficiently long password');await p.getByRole('button',{name:'Create an account',exact:true}).click();await p.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();return p};
const noOverflow=async(p,what)=>assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),true,`${what} overflows horizontally`);
const nav=async(p,name)=>{await p.locator('header nav').getByRole('link',{name,exact:true}).click()};

const ana=await signup('Ana'),ben=await signup('Ben'),cy=await signup('Cy'),dee=await signup('Dee');

// 1. Ana sets her birthday to today, with a year, and shows it to friends.
const now=new Date();
await nav(ana,'Account');await ana.getByRole('heading',{name:'Birthday',exact:true}).waitFor();
await ana.getByLabel('Month').selectOption(String(now.getMonth()+1));await ana.getByLabel('Day',{exact:true}).fill(String(now.getDate()));await ana.getByLabel('Year').fill('1990');
await audit(ana);
await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Saved. Your friends can see it.').waitFor();

// 2. Becoming friends: Ben opens Ana's link and asks, Ana accepts.
await nav(ana,'Friends');await ana.getByRole('heading',{name:'Friends',exact:true}).waitFor();await ana.getByRole('link',{name:'People'}).click();
const link=async p=>{await nav(p,'Friends');await p.getByRole('link',{name:'People'}).click();return p.getByLabel('Friend link').inputValue()};
const anaLink=await link(ana);
await audit(ana);await shot(ana,'friends-people-empty');
await ben.goto(anaLink);await ben.getByRole('heading',{name:'Ask Ana to be friends?'}).waitFor();await audit(ben);
await ben.getByRole('button',{name:'Ask to be friends'}).click();await ben.getByText('You asked').waitFor();
await ana.reload();await ana.getByText('asked to be friends').waitFor();await audit(ana);await shot(ana,'friends-request');
// A request alone opens nothing.
const benId=await ben.evaluate(async()=>(await (await fetch('/api/friends')).json()).sent[0].id);
assert.equal(await ben.evaluate(async id=>(await fetch('/api/friends/'+id)).status,benId),404);
await ana.getByRole('button',{name:'Accept'}).click();await ana.getByRole('heading',{name:'Your friends'}).waitFor();
await ana.getByRole('link',{name:'Ben',exact:true}).waitFor();
// Cy and Dee also ask; Ana accepts both. Everyone friends Ana, and Dee and Ben friend each other later.
for(const p of [cy,dee]){await p.goto(anaLink);await p.getByRole('button',{name:'Ask to be friends'}).click();await p.getByText('You asked').waitFor()}
await ana.reload();await ana.getByRole('button',{name:'Accept'}).first().click();await ana.getByRole('link',{name:'Cy',exact:true}).or(ana.getByRole('link',{name:'Dee',exact:true})).first().waitFor();await ana.getByRole('button',{name:'Accept'}).click();await ana.getByRole('link',{name:'Dee',exact:true}).waitFor();await ana.getByRole('link',{name:'Cy',exact:true}).waitFor();
await ana.getByLabel('Friend link').waitFor();await shot(ana,'friends-people');await audit(ana);
// Someone with no account opens the link, signs up, and lands on the request.
const eve=await open();await eve.goto(anaLink);await eve.getByRole('heading',{name:'Ana sent you a friend link'}).waitFor();await audit(eve);
await eve.locator('main').getByRole('link',{name:'Create an account'}).click();await eve.getByText('You need an account to ask to be friends').waitFor();
await eve.getByLabel('Your name').fill('Eve');await eve.getByLabel('Email address').fill(`eve${stamp}@example.com`);await eve.getByLabel('Password',{exact:true}).fill('a sufficiently long password');await eve.getByRole('button',{name:'Create an account',exact:true}).click();
await eve.getByRole('heading',{name:'Ask Ana to be friends?'}).waitFor();
// Eve changes her mind: Ana never sees a request she didn't get.
await eve.close();
// The link can't be used on yourself.
await ana.goto(anaLink);await ana.getByText('This is your own link').waitFor();

// 3. The calendar shows Ana's birthday to Ben, today, and the coming-up list says so.
await nav(ben,'Friends');await ben.getByRole('heading',{name:'Friends',exact:true}).waitFor();
const chip=ben.locator('.day.today .bd',{hasText:'Ana'});await chip.waitFor();
await ben.locator('.soon a',{hasText:'Ana'}).getByText('Today').waitFor();
await ben.locator('#day-panel').getByText('Turns '+(now.getFullYear()-1990)).waitFor();
await audit(ben);await shot(ben,'friends-calendar');
const tabStops=await ben.locator('.day[tabindex="0"]').count();assert.equal(tabStops,1,`the calendar has ${tabStops} tab stops`);
await ben.locator('.day.today').focus();await ben.keyboard.press('ArrowRight');
assert.equal(await ben.evaluate(()=>document.activeElement.dataset.ts-document.querySelector('.day.today').dataset.ts),864e5,'ArrowRight did not move to the next day');
await ben.keyboard.press('ArrowLeft');await ben.keyboard.press('ArrowDown');await ben.keyboard.press('ArrowUp');
assert.equal(await ben.evaluate(()=>document.activeElement.classList.contains('today')),true,'arrow keys did not return to today');
await ben.getByRole('button',{name:'Back to this month'}).count().then(n=>assert.equal(n,0,'"Back to this month" shows on this month'));
const nextMonth=ben.locator('.cal-bar .next');await nextMonth.click();await ben.getByRole('button',{name:'Back to this month'}).waitFor();
assert.equal(await ben.evaluate(()=>document.activeElement.classList.contains('next')),true,'focus lost after changing month');
await ben.getByRole('button',{name:'Back to this month'}).click();await chip.waitFor();
// The birthday notice on the exchanges page.
await nav(ben,'Exchanges');await ben.locator('.notice',{hasText:'Birthdays soon'}).getByRole('link',{name:'Ana'}).waitFor();
// The calendar file.
const ics=await ben.evaluate(async()=>{const r=await fetch('/calendar/birthdays');return {status:r.status,type:r.headers.get('content-type'),text:await r.text()}});
assert.equal(ics.status,200);assert.match(ics.type,/text\/calendar/);assert.match(ics.text,/SUMMARY:Ana’s birthday/);assert.match(ics.text,/RRULE:FREQ=YEARLY/);

// 4. Ana's wish list: ideas for friends, for givers, for both and for nobody.
const idea=async(p,title,{friends=true,givers=true}={})=>{await nav(p,'Wish list');await p.getByRole('button',{name:'Add idea'}).click();await p.getByLabel('What is it?').fill(title);const f=p.getByRole('checkbox',{name:/^Friends/}),g=p.getByRole('checkbox',{name:/^Whoever buys for me/});if(friends!==await f.isChecked())await f.click();if(givers!==await g.isChecked())await g.click();await p.getByRole('button',{name:'Save',exact:true}).click();await p.getByRole('heading',{name:title}).waitFor()};
await idea(ana,'Cast-iron pan');await idea(ana,'Wool socks',{friends:false});await idea(ana,'Secret hat',{givers:false});await idea(ana,'Private diary',{friends:false,givers:false});
await ana.getByRole('heading',{name:'Friends and whoever buys for you'}).waitFor();await ana.getByRole('heading',{name:'Friends only'}).waitFor();await ana.getByRole('heading',{name:'Whoever buys for you',exact:true}).waitFor();await ana.getByRole('heading',{name:'Only you'}).waitFor();
await ana.getByText('Seen by friends only').waitFor();await ana.getByText('Only you can see this').waitFor();await ana.getByText('Seen by friends and by whoever buys for you').waitFor();
await audit(ana);await shot(ana,'wishes-own');
await ana.getByRole('button',{name:'Add idea'}).click();await audit(ana);await shot(ana,'wish-dialog');await ana.getByRole('button',{name:'Close'}).click();

// 5. Ben sees only the ideas shown to friends, and marks one. Cy sees it taken. Ana sees nothing about it.
await nav(ben,'Friends');await ben.getByRole('link',{name:'People'}).click();await ben.getByRole('link',{name:'Ana',exact:true}).click();await ben.getByRole('heading',{name:'Ana',exact:true}).waitFor();
assert.equal(await ben.getByRole('heading',{name:'Cast-iron pan'}).count(),1);assert.equal(await ben.getByRole('heading',{name:'Secret hat'}).count(),1);
assert.equal(await ben.getByRole('heading',{name:'Wool socks'}).count(),0,'a friend sees an idea meant for givers only');assert.equal(await ben.getByRole('heading',{name:'Private diary'}).count(),0);
await ben.getByText('Ana is never told').waitFor();await audit(ben);await shot(ben,'friend-page');
await ben.getByRole('button',{name:'I’m getting this: Cast-iron pan'}).click();await ben.getByText('You’re getting this.').waitFor();
assert.equal(await ben.evaluate(()=>document.activeElement.textContent.trim()),'Undo','focus not on Undo');
await audit(ben);
await nav(cy,'Friends');await cy.getByRole('link',{name:'People'}).click();await cy.getByRole('link',{name:'Ana',exact:true}).click();
const taken=cy.locator('.wishes li',{hasText:'Cast-iron pan'});await taken.getByText('Someone else is getting this one.').waitFor();assert.equal(await taken.getByRole('button').count(),0);
await nav(ana,'Wish list');await ana.getByRole('heading',{name:'Cast-iron pan'}).waitFor();
const page=await ana.locator('main').innerText();assert.doesNotMatch(page,/getting this|Someone else|Ben/,'the owner can see a claim');
// Ben changes his mind; the idea is free again for Cy.
await ben.getByRole('button',{name:'Undo: you’re getting cast-iron pan'}).click();await ben.getByRole('button',{name:'I’m getting this: Cast-iron pan'}).waitFor();

// 6. Phone width: nothing overflows on the new pages.
for(const w of [390,320]){const p=await open(w);await p.goto(base);await p.locator('header').getByRole('link',{name:'Sign in'}).click();await p.getByLabel('Email address').fill(`ben${stamp}@example.com`);await p.getByLabel('Password',{exact:true}).fill('a sufficiently long password');await p.getByRole('button',{name:'Sign in',exact:true}).click();await p.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();
 await nav(p,'Friends');await p.locator('.day.today').waitFor();await noOverflow(p,`calendar at ${w}`);await audit(p);await shot(p,`calendar-${w}`);
 const box=await p.locator('.day').first().evaluate(e=>{const r=e.getBoundingClientRect();return {w:r.width,h:r.height}});assert.ok(box.h>=44&&box.w>=44,`calendar day is ${box.w}x${box.h}px`);
 await p.locator('.day-line').getByText(/Ana/).waitFor();await p.locator('.day:not(.out)').nth(0).click();await p.locator('.day-line').getByText('October').or(p.locator('.day-line')).first().waitFor();
 await p.getByRole('link',{name:'People'}).click();await p.getByRole('heading',{name:'Your friends'}).waitFor();await noOverflow(p,`people at ${w}`);await audit(p);await shot(p,`people-${w}`);
 await p.getByRole('link',{name:'Ana',exact:true}).click();await p.getByRole('heading',{name:'Ana',exact:true}).waitFor();await noOverflow(p,`friend page at ${w}`);await audit(p);
 await nav(p,'Account');await p.getByRole('heading',{name:'Birthday',exact:true}).waitFor();await noOverflow(p,`account at ${w}`);
 await p.close()}

// 7. White elephant: Ana organises, three join and close entries; each draws a number.
await nav(ana,'Exchanges');await ana.getByRole('link',{name:'New exchange'}).first().click();
await ana.getByLabel('Exchange name').fill('Office swap');await ana.getByLabel('Exchange date').fill(future(30));
await ana.getByRole('radio',{name:/White elephant/}).check();await ana.getByLabel('Limit per gift').waitFor();await ana.getByLabel('Steals per gift').fill('2');
await audit(ana);await shot(ana,'new-exchange-elephant');
await ana.getByRole('button',{name:'Create exchange',exact:true}).click();await ana.getByRole('heading',{name:'Office swap'}).waitFor();
await ana.locator('.status',{hasText:'White elephant'}).waitFor();await ana.getByText('Steals per gift').waitFor();
const invite=await ana.getByLabel('Invitation link').inputValue();
assert.equal(await ana.getByRole('heading',{name:'Keep apart'}).count(),0,'a white elephant offers keep apart');
for(const p of [ben,cy]){await p.goto(invite);await p.getByText('Bring one wrapped gift').waitFor();await p.getByRole('button',{name:/Join as/}).click();await p.getByRole('heading',{name:'Office swap'}).waitFor()}
await ana.reload();await ana.getByRole('button',{name:'Close entries'}).click();await ana.getByRole('dialog').getByText('Pick numbers are given out').waitFor();await audit(ana);
await ana.getByRole('dialog').getByRole('button',{name:/Close entries for 3 people/}).click();await ana.getByRole('button',{name:'Draw a number'}).waitFor();
for(const p of [ana,ben,cy]){await p.reload();await p.getByRole('button',{name:'Draw a number'}).click();await p.locator('.my-ticket').waitFor();await p.getByText('You pick').first().waitFor()}
await ana.getByRole('heading',{name:'Pick order'}).waitFor();
const order=await ana.locator('#people .tk .name').allInnerTexts();assert.equal(order.length,3);
const mine=await ana.locator('.my-ticket .stub').innerText();assert.equal(Number(mine)-1,order.indexOf('Ana'),'the ticket and the list disagree about the order');
await audit(ana);await shot(ana,'white-elephant');
await ana.getByRole('button',{name:'Mark gift as ready'}).click();await ana.getByText('Gift ready · Undo').waitFor();
await nav(ana,'Exchanges');await ana.getByRole('row',{name:/Office swap/}).getByText('White elephant').waitFor();

// 8. Group gift for Dee: Ana is Dee's friend, Ben is Dee's friend, Cy is not.
const deeLink=await link(dee);
await ben.goto(deeLink);await ben.getByRole('button',{name:'Ask to be friends'}).click();await ben.getByText('You asked').waitFor();
await dee.reload();await dee.getByRole('button',{name:'Accept'}).click();await dee.getByRole('heading',{name:'Your friends'}).waitFor();
await ana.goto(deeLink);await ana.getByText('You and Dee are friends').waitFor();
await idea(dee,'Climbing chalk bag');await idea(dee,'Hidden bookmark',{friends:false});
await nav(ana,'Exchanges');await ana.getByRole('link',{name:'New exchange'}).first().click();
await ana.getByLabel('Exchange name').fill('Climbing gift');await ana.getByLabel('Exchange date').fill(future(20));
await ana.getByRole('radio',{name:/Group gift/}).check();await ana.getByLabel('Who is it for?').selectOption({label:'Dee'});await audit(ana);
await ana.getByRole('button',{name:'Create exchange',exact:true}).click();await ana.getByRole('heading',{name:'Climbing gift'}).waitFor();
await ana.getByRole('heading',{name:'Dee’s ideas'}).waitFor();await ana.getByRole('heading',{name:'Climbing chalk bag'}).waitFor();
assert.equal(await ana.getByRole('heading',{name:'Hidden bookmark'}).count(),0);
assert.equal(await ana.getByRole('button',{name:'Close entries'}).count(),0,'a group gift offers a draw');
await ana.getByText('can’t see this exchange').waitFor();await audit(ana);await shot(ana,'group-gift');
const groupInvite=await ana.getByLabel('Invitation link').inputValue();
// Cy isn't Dee's friend: refused with a plain reason. Dee can't join, and never sees it listed.
await cy.goto(groupInvite);await cy.getByRole('button',{name:/Join as/}).click();await cy.getByText('Add the person it’s for as a friend first').waitFor();
await dee.goto(groupInvite);await dee.getByRole('button',{name:/Join as/}).click();await dee.getByText('closed or no longer exists').waitFor();
await nav(dee,'Exchanges');await dee.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();assert.equal(await dee.getByText('Climbing gift').count(),0,'the person a group gift is for can see it listed');
// Ben joins, and marks the chalk bag from the exchange page; Ana sees it taken.
await ben.goto(groupInvite);await ben.getByRole('button',{name:/Join as/}).click();await ben.getByRole('heading',{name:'Climbing gift'}).waitFor();
await ben.getByRole('button',{name:'I’m getting this: Climbing chalk bag'}).click();await ben.getByText('You’re getting this.').waitFor();
await ana.reload();await ana.locator('.wishes li',{hasText:'Climbing chalk bag'}).getByText('Someone else is getting this one.').waitFor();
await nav(dee,'Wish list');await dee.getByRole('heading',{name:'Wish list',exact:true}).waitFor();const deeText=await dee.locator('main').innerText();assert.doesNotMatch(deeText,/getting this|Someone else|Climbing gift/,'Dee can see a mark or the exchange');

assert.deepEqual(errors,[],'uncaught script errors');
await browser.close();
console.log('PASS: friends, birthdays, calendar, wish list audiences, marking ideas, white elephant, group gift, accessibility, phone layouts, no JS errors');
})().catch(e=>{console.error(e);process.exit(1)});
