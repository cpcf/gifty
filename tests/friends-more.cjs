// The friends flows tests/friends.cjs doesn't reach: many birthdays on one day, a leap-day birthday across years, year
// labels and arrow keys across month ends, declining, cancelling, removing and replacing, hiding and removing a
// birthday, changing who sees an idea, putting an idea down, the birthday notice, the kind picker with no friends, a
// group gift left and previewed, and signed-out visits. Needs a fresh data file and no mail; see docs/testing.md.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const AxeBuilder=require('@axe-core/playwright').default;
const base=process.env.GIFTY_TEST_URL||'http://127.0.0.1:8094';
const audit=async p=>{await p.waitForTimeout(250);const r=await new AxeBuilder({page:p}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(r.violations.map(v=>({id:v.id,targets:v.nodes.map(n=>n.target)})),[])};
const stamp=Date.now();
const future=days=>{const d=new Date(Date.now()+days*864e5);return new Date(d.getTime()-d.getTimezoneOffset()*6e4).toISOString().slice(0,10)};
(async()=>{
const browser=await chromium.launch({...(process.env.GIFTY_CHROME?{executablePath:process.env.GIFTY_CHROME}:{}),headless:true});
const errors=[];
const open=async(width=1280)=>{const ctx=await browser.newContext({locale:process.env.GIFTY_LOCALE||'en-GB',viewport:{width,height:1000},reducedMotion:'reduce'});const p=await ctx.newPage();p.on('pageerror',e=>errors.push(e.message));return p};
const email=name=>`${name.toLowerCase()}${stamp}@example.com`,PASSWORD='a sufficiently long password';
const signup=async name=>{const p=await open();await p.goto(base);await p.locator('header').getByRole('link',{name:'Create an account'}).click();await p.getByLabel('Your name').fill(name);await p.getByLabel('Email address').fill(email(name));await p.getByLabel('Password',{exact:true}).fill(PASSWORD);await p.getByRole('button',{name:'Create an account',exact:true}).click();await p.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();return p};
const call=(p,path,body)=>p.evaluate(async([path,body])=>{const r=await fetch('/api/'+path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});return {status:r.status,json:await r.json().catch(()=>null)}},[path,body]);
const idOf=async p=>(await call(p,'me')).json.id;
const befriend=async(x,y)=>{const code=(await call(y,'friends')).json.code;await call(x,'friends/request',{code});await call(y,'friends/accept',{id:await idOf(x)})};
const nav=async(p,name)=>{await p.locator('header nav').getByRole('link',{name,exact:true}).click()};
const now=new Date(),cy=now.getFullYear(),cm=now.getMonth();
const ts=(y,m,d)=>Date.UTC(y,m,d);
const dayCell=(p,y,m,d)=>p.locator(`.day[data-ts="${ts(y,m,d)}"]`);
const calendar=async p=>{await nav(p,'Friends');await p.locator('.day.today').waitFor()};
const monthsAhead=async(p,n)=>{const back=p.getByRole('button',{name:'Back to this month'});if(await back.count())await back.click();for(let i=0;i<n;i++)await p.locator('.cal-bar .next').click()};
const setBirthday=(p,month,day,year,show=true)=>call(p,'birthday',{month,day,year:year||0,show});

const ana=await signup('Ana'),people={};
for(const n of ['Ben','Cy','Dee','Eve','Fay'])people[n]=await signup(n);
for(const n of Object.keys(people))await befriend(people[n],ana);
const [ben,cyP,dee,eve,fay]=['Ben','Cy','Dee','Eve','Fay'].map(n=>people[n]);
for(const [p,d] of [[ben,1],[cyP,1],[dee,1],[eve,1]])await setBirthday(p,cm+1,now.getDate(),0);
await setBirthday(fay,2,29,1996);

// 1. Four birthdays on one day: three chips and "+1 more" on desktop, three stripes on a phone, everyone named in words.
await calendar(ana);
const today=dayCell(ana,cy,cm,now.getDate());
assert.equal(await today.locator('.bd').count(),3);
await today.locator('.more').getByText('+1 more').waitFor();
const label=await today.getAttribute('aria-label');for(const n of ['Ben','Cy','Dee','Eve'])assert.match(label,new RegExp(n),`the day's label leaves out ${n}`);
for(const n of ['Ben','Cy','Dee','Eve'])await ana.locator('#day-panel').getByText(n,{exact:true}).waitFor();
assert.equal(await ana.locator('.day-line').evaluate(e=>getComputedStyle(e).display),'none','the day line shows on desktop');
await audit(ana);
await ana.locator('.soon a',{hasText:'Fay'}).count().then(n=>assert.ok(n<=1));
// ...and the list of what's coming says Today for all four.
assert.equal(await ana.locator('.soon a span.now').count(),4);

// 2. Arrow keys cross the end of a month, and the month buttons name the year when it changes.
const last=new Date(Date.UTC(cy,cm+1,0)).getUTCDate();
await dayCell(ana,cy,cm,last).focus();await ana.keyboard.press('ArrowRight');
assert.equal(await ana.evaluate(()=>document.activeElement.dataset.ts),String(ts(cy,cm,last+1)),'ArrowRight at a month end');
assert.equal(await ana.locator('.day[aria-pressed="true"]').getAttribute('data-ts'),String(ts(cy,cm,last+1)));
await ana.keyboard.press('ArrowLeft');
assert.equal(await ana.locator('.day[aria-pressed="true"]').getAttribute('data-ts'),String(ts(cy,cm,last)),'ArrowLeft back over the month end');
assert.equal(await ana.getByRole('button',{name:'Back to this month'}).count(),0,'the calendar is back on this month');
await monthsAhead(ana,11-cm); // to December
const nextLabel=await ana.locator('.cal-bar .next').innerText();assert.match(nextLabel,new RegExp(String(cy+1)),`December's next button says "${nextLabel}"`);
await ana.locator('.cal-bar .next').click();
const prevLabel=await ana.locator('.cal-bar .prev').innerText();assert.match(prevLabel,new RegExp(String(cy)),`January's previous button says "${prevLabel}"`);
await ana.getByRole('button',{name:'Back to this month'}).click();

// 3. A leap-day birthday sits on 28 February in a year without one and on 29 February in a leap year.
const nextYear=(pred)=>{let y=cy+1;while(!pred(y))y++;return y};
const plain=nextYear(y=>y%4!==0||(y%100===0&&y%400!==0)),leap=nextYear(y=>y%4===0&&(y%100!==0||y%400===0));
await monthsAhead(ana,(plain-cy)*12+(1-cm));
await dayCell(ana,plain,1,28).locator('.bd',{hasText:'Fay'}).waitFor();
assert.equal(await ana.locator('.day:not(.out) .bd',{hasText:'Fay'}).count(),1);
await dayCell(ana,plain,1,28).click();await ana.locator('#day-panel').getByText('Turns '+(plain-1996)).waitFor();
await monthsAhead(ana,(leap-cy)*12+(1-cm));
await dayCell(ana,leap,1,29).locator('.bd',{hasText:'Fay'}).waitFor();
assert.equal(await dayCell(ana,leap,1,28).locator('.bd').count(),0,'the leap-day birthday is on both 28 and 29 February');
await audit(ana);
await ana.getByRole('button',{name:'Back to this month'}).click();

// 4. Phone: the same day is three stripes, and tapping a day answers in the line under the month heading.
const phone=await open(390);await phone.goto(base);await phone.locator('header').getByRole('link',{name:'Sign in'}).click();await phone.getByLabel('Email address').fill(email('Ana'));await phone.getByLabel('Password',{exact:true}).fill(PASSWORD);await phone.getByRole('button',{name:'Sign in',exact:true}).click();
await calendar(phone);
assert.equal(await dayCell(phone,cy,cm,now.getDate()).locator('.bd').count(),3);
assert.equal(await dayCell(phone,cy,cm,now.getDate()).locator('.bd').first().evaluate(e=>Math.round(e.getBoundingClientRect().height)),6);
await phone.locator('.day-line').waitFor();
await dayCell(phone,cy,cm,now.getDate()===1?2:1).click();
await phone.locator('.day-line').getByText(/No birthdays/).waitFor();
await dayCell(phone,cy,cm,now.getDate()).click();
for(const n of ['Ben','Cy','Dee','Eve'])await phone.locator('.day-line').getByText(new RegExp(n)).waitFor();
assert.equal(await phone.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
await phone.close();

// 5. Declining and cancelling requests leave nothing behind.
const gus=await signup('Gus');
await nav(ana,'Friends');await ana.getByRole('link',{name:'People'}).click();
const anaLink=await ana.locator('#friend-link').inputValue();
await gus.goto(anaLink);await gus.getByRole('button',{name:'Ask to be friends'}).click();await gus.getByText('You asked').waitFor();
await ana.reload();await ana.getByText('asked to be friends').waitFor();
await ana.getByRole('button',{name:'Not now'}).click();await ana.getByText('Request declined. They aren’t told.').waitFor();
assert.equal(await ana.getByText('asked to be friends').count(),0);
await gus.reload();assert.equal((await call(gus,'friends')).json.sent.length,0);
await gus.goto(anaLink);await gus.getByRole('button',{name:'Ask to be friends'}).click();await gus.getByText('You asked').waitFor();
await gus.getByRole('button',{name:/Cancel your request/}).click();await gus.getByText('Request cancelled.').waitFor();
await ana.reload();assert.equal(await ana.getByText('asked to be friends').count(),0,'a cancelled request is still waiting');
await gus.goto(anaLink);await gus.getByText(/You’ve asked|Ask Ana/).first().waitFor();
await gus.getByRole('button',{name:'Ask to be friends'}).click();await gus.getByText('You asked').waitFor();
await ana.reload();await ana.getByRole('button',{name:'Accept'}).click();await ana.getByText('You’re friends now.').waitFor();
await ana.getByRole('link',{name:'Gus',exact:true}).waitFor();
await gus.goto(anaLink);await gus.getByRole('heading',{name:'You and Ana are friends'}).waitFor();

// 6. Putting an idea down, changing who sees it, and removing a friend all release what friends had marked.
const wish=async(p,title)=>{await nav(p,'Wish list');await p.getByRole('button',{name:'Add idea'}).click();await p.getByLabel('What is it?').fill(title);await p.getByRole('button',{name:'Save',exact:true}).click();await p.getByRole('heading',{name:title}).waitFor()};
await wish(ana,'Cast-iron pan');await wish(ana,'Wool hat');
const anaId=await idOf(ana);
const friendPage=async(p,name)=>{await nav(p,'Friends');await p.getByRole('link',{name:'People'}).click();await p.getByRole('link',{name,exact:true}).click();await p.getByRole('heading',{name,exact:true}).waitFor()};
await friendPage(ben,'Ana');await ben.getByRole('button',{name:'I’m getting this: Cast-iron pan'}).click();await ben.getByText('You’re getting this.').waitFor();
await friendPage(cyP,'Ana');await cyP.locator('.wishes li',{hasText:'Cast-iron pan'}).getByText('Someone else is getting this one.').waitFor();
// 6a. Ana takes Friends off the pan: it vanishes for friends, and Ben's mark goes with it.
await nav(ana,'Wish list');await ana.getByRole('button',{name:'Edit Cast-iron pan'}).click();
await ana.getByRole('checkbox',{name:/^Friends/}).uncheck();await ana.getByRole('button',{name:'Save',exact:true}).click();await ana.getByRole('heading',{name:'Cast-iron pan'}).waitFor();
await ana.getByText('Seen by whoever buys for you').first().waitFor();
assert.equal((await call(ben,`friends/${anaId}`)).json.wishes.some(w=>w.title==='Cast-iron pan'),false,'a friend still sees an idea shown to nobody they know');
await ana.getByRole('button',{name:'Edit Cast-iron pan'}).click();await ana.getByRole('checkbox',{name:/^Friends/}).check();await ana.getByRole('button',{name:'Save',exact:true}).click();await ana.getByRole('heading',{name:'Cast-iron pan'}).waitFor();
await friendPage(cyP,'Ana');assert.equal(await cyP.locator('.wishes li',{hasText:'Cast-iron pan'}).getByRole('button',{name:/I’m getting this/}).count(),1,'the mark survived the idea leaving and returning');
// 6b. Done with it: the friend who had marked it is told to check; others stop seeing it; putting it back restores it.
await friendPage(ben,'Ana');await ben.getByRole('button',{name:'I’m getting this: Cast-iron pan'}).click();await ben.getByText('You’re getting this.').waitFor();
await nav(ana,'Wish list');await ana.getByRole('button',{name:'Done with: Cast-iron pan'}).click();
await ana.getByRole('dialog').getByRole('button',{name:'I’ve got it'}).click();await ana.getByText('Taken off your list. Nobody sees it any more.').waitFor();
await ana.getByRole('heading',{name:'Done with',exact:true}).waitFor();
await friendPage(ben,'Ana');await ben.getByText('They’ve got this now.').waitFor();
await friendPage(cyP,'Ana');assert.equal(await cyP.getByRole('heading',{name:'Cast-iron pan'}).count(),0,'a sorted idea is still shown to other friends');
await ben.getByRole('button',{name:'Stop tracking Cast-iron pan'}).click();
await nav(ana,'Wish list');await ana.getByRole('button',{name:'Put back: Cast-iron pan'}).click();await ana.getByText('Back on your list.').waitFor();
await friendPage(cyP,'Ana');await cyP.getByRole('heading',{name:'Cast-iron pan'}).waitFor();
// 6c. Removing a friend: cancel keeps them; confirming ends it and releases their mark; the page is gone for them.
await friendPage(ben,'Ana');await ben.getByRole('button',{name:'I’m getting this: Cast-iron pan'}).click();await ben.getByText('You’re getting this.').waitFor();
await friendPage(ana,'Ben');
await ana.getByText('Manage friend').click();await audit(ana);
await ana.getByRole('button',{name:'Remove friend'}).click();
assert.equal(await ana.evaluate(()=>document.activeElement.textContent.trim()),'Cancel','a confirmation should open on Cancel');
await audit(ana);
await ana.getByRole('dialog').getByRole('button',{name:'Cancel'}).click();
await ana.getByRole('heading',{name:'Ben',exact:true}).waitFor();assert.equal((await call(ana,'friends')).json.friends.some(f=>f.name==='Ben'),true);
await ana.getByRole('button',{name:'Remove friend'}).click();
await ana.getByRole('dialog').getByRole('button',{name:'Remove friend'}).click();await ana.getByText(/Removed\. Anything either of you had marked/).waitFor();
await ana.getByRole('heading',{name:'Your friends'}).waitFor();assert.equal(await ana.getByRole('link',{name:'Ben',exact:true}).count(),0);
assert.equal((await call(ben,`friends/${anaId}`)).status,404);
await ben.goto(base+'#friends/'+anaId);await ben.reload();await ben.getByRole('heading',{name:'Not found'}).waitFor();
await befriend(ben,ana); // friends again: the mark is not back
await friendPage(cyP,'Ana');assert.equal(await cyP.locator('.wishes li',{hasText:'Cast-iron pan'}).getByRole('button',{name:/I’m getting this/}).count(),1,'a removed friend still holds a mark');

// 7. Replacing the friend link stops the old one, keeps the friends, and tells a visitor plainly.
await nav(ana,'Friends');await ana.getByRole('link',{name:'People'}).click();
const before=await ana.locator('#friend-link').inputValue();
await ana.getByRole('button',{name:'Replace link'}).click();await audit(ana);
await ana.getByRole('dialog').getByRole('button',{name:'Cancel'}).click();assert.equal(await ana.locator('#friend-link').inputValue(),before);
await ana.getByRole('button',{name:'Replace link'}).click();await ana.getByRole('dialog').getByRole('button',{name:'Replace link'}).click();await ana.getByText(/Friend link replaced/).waitFor();
const after=await ana.locator('#friend-link').inputValue();assert.notEqual(after,before);
const visitor=await open();await visitor.goto(before);await visitor.getByText('This friend link no longer works.').waitFor();await visitor.getByRole('heading',{name:'Not found'}).waitFor();
await visitor.goto(after);await visitor.getByRole('heading',{name:'Ana sent you a friend link'}).waitFor();await visitor.close();
assert.equal((await call(ana,'friends')).json.friends.length>=5,true,'replacing the link lost friends');

// 8. The birthday form: hide it, remove it, and mistakes.
await nav(ana,'Account');await ana.getByRole('heading',{name:'Birthday',exact:true}).waitFor();
await ana.getByLabel('Month').selectOption('3');await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Choose both a month and a day.').waitFor();
await ana.getByLabel('Day',{exact:true}).fill('14');await ana.getByLabel('Year').fill('1990');await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Saved. Your friends can see it.').waitFor();
await calendar(ben);const chip=ben.locator('.cal-grid .bd',{hasText:'Ana'});
await monthsAhead(ben,(2-cm+12)%12); // March, wherever we are
assert.equal(await chip.count(),1,"Ana's birthday isn't on her friends' calendar");
await nav(ana,'Account');await ana.getByRole('checkbox',{name:/Show my birthday to friends/}).uncheck();await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Saved. It’s hidden from your friends.').waitFor();
assert.equal((await call(ana,'me')).json.birthdayHidden,true);
await ben.reload();await monthsAhead(ben,(2-cm+12)%12);assert.equal(await chip.count(),0,'a hidden birthday is on a friend’s calendar');
await nav(ana,'Friends');await monthsAhead(ana,(2-cm+12)%12);await ana.locator('.cal-grid .bd.me',{hasText:'You'}).waitFor(); // you still see your own
await nav(ana,'Account');await ana.getByLabel('Month').selectOption('0');await ana.getByLabel('Day',{exact:true}).fill('');await ana.getByLabel('Year').fill('');await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Birthday removed.').waitFor();
assert.equal((await call(ana,'me')).json.birthday,null);assert.equal(await ana.getByLabel('Day',{exact:true}).inputValue(),'');
await ana.getByRole('button',{name:'Save birthday'}).click();await ana.getByText('Choose a month and day first.').waitFor();

// 9. The birthday notice on the exchanges page: three names, then a count; nothing for someone with no friends.
await nav(ana,'Exchanges');
const notice=ana.locator('.notice',{hasText:'Birthdays soon'});await notice.waitFor();
assert.equal(await notice.getByRole('link').count(),4,'three names and a link for the rest'); // Ben, Cy and Dee, then "1 more"
await notice.getByRole('link',{name:'1 more'}).waitFor();await notice.getByText('today').first().waitFor();
await notice.getByRole('link',{name:'1 more'}).click();await ana.locator('.day.today').waitFor(); // it opens the calendar
const hal=await signup('Hal');
assert.equal(await hal.locator('.notice',{hasText:'Birthdays soon'}).count(),0,'someone with no friends sees a birthday notice');
await nav(eve,'Exchanges');await eve.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();
assert.equal(await eve.locator('.notice',{hasText:'Birthdays soon'}).count(),0,'a notice for a friend with no birthday');

// 10. The kind picker with no friends: group gift is disabled with the reason; a white elephant's steals can be edited.
await hal.getByRole('link',{name:'New exchange'}).first().click();
const group=hal.getByRole('radio',{name:/Group gift/});assert.equal(await group.isDisabled(),true);await hal.getByText('Add a friend first.').first().waitFor();
await hal.getByRole('radio',{name:/Secret Santa/}).waitFor();assert.equal(await hal.getByRole('radio',{name:/Secret Santa/}).isChecked(),true);
await hal.getByRole('radio',{name:/White elephant/}).check();
assert.equal(await hal.getByLabel('Steals per gift').isVisible(),true);assert.equal(await hal.getByLabel('Who is it for?').isVisible(),false);
await hal.getByRole('radio',{name:/Secret Santa/}).check();assert.equal(await hal.getByLabel('Steals per gift').isVisible(),false);
await hal.getByRole('radio',{name:/White elephant/}).check();
await hal.getByLabel('Exchange name').fill('Swap');await hal.getByLabel('Exchange date').fill(future(40));await hal.getByRole('button',{name:'Create exchange',exact:true}).click();
await hal.getByRole('heading',{name:'Swap'}).waitFor();
await hal.getByRole('button',{name:'Edit details'}).click();assert.equal(await hal.getByRole('dialog').getByRole('radio').count(),0,'the kind can be changed after creation');
await hal.getByRole('dialog').getByLabel('Steals per gift').fill('5');await hal.getByRole('dialog').getByRole('button',{name:'Save changes'}).click();
await hal.locator('.facts div',{hasText:'Steals per gift'}).locator('dd',{hasText:/^5$/}).waitFor();

// 11. A group gift: its invitation says what it is, a member can leave and come back, and the person it is for sees none of it.
await befriend(ana,ben);await befriend(cyP,ben);
await nav(ana,'Exchanges');await ana.getByRole('link',{name:'New exchange'}).first().click();
await ana.getByLabel('Exchange name').fill('Gift for Ben');await ana.getByLabel('Exchange date').fill(future(20));
await ana.getByRole('radio',{name:/Group gift/}).check();await ana.getByLabel('Who is it for?').selectOption({label:'Ben'});await ana.getByRole('button',{name:'Create exchange',exact:true}).click();
await ana.getByRole('heading',{name:'Gift for Ben'}).waitFor();
const invite=await ana.getByLabel('Invitation link').inputValue();
const guest=await open();await guest.goto(invite);await guest.getByText(/group gift/).first().waitFor();await guest.getByText(/friends with them to join/).waitFor();await audit(guest);await guest.close();
await cyP.goto(invite);await cyP.getByRole('button',{name:/Join as/}).click();await cyP.getByRole('heading',{name:'Gift for Ben'}).waitFor();
await cyP.getByRole('button',{name:'Leave exchange'}).click();await cyP.getByRole('dialog').getByRole('button',{name:'Leave this exchange'}).click();await cyP.getByText('You’ve left the exchange.').waitFor();
assert.equal((await call(cyP,'exchanges')).json.some(e=>e.name==='Gift for Ben'),false);
await cyP.goto(invite);await cyP.getByRole('button',{name:/Join as/}).click();await cyP.getByRole('heading',{name:'Gift for Ben'}).waitFor(); // back in
await nav(ben,'Exchanges');assert.equal(await ben.getByText('Gift for Ben').count(),0);
assert.equal((await call(ben,'exchanges')).json.some(e=>e.name==='Gift for Ben'),false);

// 12. Signed out, none of the new pages is reachable and none gives anything away.
const out=await open();
for(const hash of ['friends','friends/people','friends/'+anaId,'account']){await out.goto(base+'/#'+hash);await out.reload();await out.getByRole('heading',{name:'Friends',exact:true}).count().then(n=>assert.equal(n,0,`#${hash} shows the friends page signed out`))}
await out.goto(base+'/#friends/'+anaId);await out.reload();assert.equal(await out.getByText('Ana',{exact:true}).count(),0,'a friend page names someone to a stranger');
assert.equal((await call(out,'friends')).status,401);assert.equal((await call(out,`friends/${anaId}`)).status,401);
assert.equal((await out.evaluate(async()=>(await fetch('/calendar/birthdays')).status)),401);
await out.close();

assert.deepEqual(errors,[],'uncaught script errors');
await browser.close();
console.log('PASS: crowded and leap-day calendars, year labels, arrow keys over month ends, request decline/cancel, putting ideas down, audience edits, removing and replacing, birthday form, notices, kind picker, group gift leave and preview, signed-out access');
})().catch(e=>{console.error(e);process.exit(1)});
