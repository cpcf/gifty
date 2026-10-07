// Keep apart, anonymous messages, claiming ideas, sorted ideas, calendar file and reveal day, driven in Chrome
// with four people. Needs a server with a fresh data file; see docs/testing.md.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const AxeBuilder=require('@axe-core/playwright').default;
const base=process.env.GIFTY_TEST_URL||'http://127.0.0.1:8088';
const audit=async p=>{await p.waitForTimeout(250);const r=await new AxeBuilder({page:p}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(r.violations.map(v=>({id:v.id,targets:v.nodes.map(n=>n.target)})),[])};
const today=(()=>{const d=new Date();return new Date(d.getTime()-d.getTimezoneOffset()*6e4).toISOString().slice(0,10)})();
const stamp=Date.now();
const shots=process.env.GIFTY_SCREENSHOTS,shot=async(p,n)=>{if(shots)await p.screenshot({path:`${shots}/${n}.png`,fullPage:true})};
(async()=>{
const browser=await chromium.launch({...(process.env.GIFTY_CHROME?{executablePath:process.env.GIFTY_CHROME}:{}),headless:true});
const errors=[];
const open=async()=>{const ctx=await browser.newContext({viewport:{width:1280,height:1000},reducedMotion:'reduce'});const p=await ctx.newPage();p.on('pageerror',e=>errors.push(e.message));if(process.env.DEBUG_API)p.on('response',r=>{if(r.request().method()==='POST'&&r.url().includes('/api/'))r.text().then(x=>console.log(r.status(),r.url().replace(/.*\/api\//,''),x.slice(0,160)))});return p};
const names=['Alex','Bea','Cara','Dev'],pages={};
const wish=async(p,title)=>{await p.getByRole('link',{name:'Wish list',exact:true}).click();await p.getByRole('button',{name:'Add idea'}).click();await p.getByLabel('What is it?').fill(title);await p.getByRole('button',{name:'Save',exact:true}).click();await p.getByRole('heading',{name:title}).waitFor()};
// Alex organises an exchange dated today, so the reveal is available straight away.
const alex=pages.Alex=await open();
await alex.goto(base);await alex.locator('header').getByRole('link',{name:'Create an account'}).click();
await alex.getByLabel('Your name').fill('Alex');await alex.getByLabel('Email address').fill(`alex${stamp}@example.com`);await alex.getByLabel('Password',{exact:true}).fill('a sufficiently long password');await alex.getByRole('button',{name:'Create an account',exact:true}).click();
await alex.getByRole('link',{name:'New exchange',exact:true}).first().click();await alex.getByLabel('Exchange name').fill('Features, party');await alex.getByLabel('Exchange date').fill(today);await alex.getByRole('button',{name:'Create exchange',exact:true}).click();
await alex.getByRole('heading',{name:'Features, party'}).waitFor();
const exchangeURL=alex.url(),invitation=await alex.getByLabel('Invitation link').inputValue();
for(const name of names.slice(1)){const p=pages[name]=await open();await p.goto(invitation);await p.getByRole('link',{name:'Create an account to join'}).click();await p.getByLabel('Your name').fill(name);await p.getByLabel('Email address').fill(`${name.toLowerCase()}${stamp}@example.com`);await p.getByLabel('Password',{exact:true}).fill('another long password here');await p.getByRole('button',{name:'Create an account',exact:true}).click();await p.getByRole('heading',{name:'Features, party'}).waitFor()}
for(const name of names){await wish(pages[name],name+' teapot');await wish(pages[name],name+' scarf')}

// 1. Keep apart: only the organiser has the panel.
await alex.goto(exchangeURL);await alex.reload();
assert.equal(await pages.Bea.getByRole('heading',{name:'Keep apart'}).count(),0,'a member can see the keep-apart panel');
await alex.getByRole('button',{name:'Keep two people apart'}).click();
await alex.getByLabel('First person').selectOption({label:'Bea'});await alex.getByLabel('Second person').selectOption({label:'Bea'});
await alex.getByRole('dialog').getByRole('button',{name:'Keep apart',exact:true}).click();await alex.getByText('Choose two different people.').waitFor();
await alex.getByLabel('Second person').selectOption({label:'Cara'});await audit(alex);
await alex.getByRole('dialog').getByRole('button',{name:'Keep apart',exact:true}).click();
await alex.locator('.apart-list li',{hasText:'Bea and Cara'}).waitFor();await audit(alex);
await alex.getByRole('button',{name:'Remove: Bea and Cara'}).click();await alex.locator('.apart-list li',{hasText:'Bea and Cara'}).waitFor({state:'detached'});
await alex.getByRole('button',{name:'Keep two people apart'}).click();await alex.getByLabel('First person').selectOption({label:'Bea'});await alex.getByLabel('Second person').selectOption({label:'Cara'});await alex.getByRole('dialog').getByRole('button',{name:'Keep apart',exact:true}).click();await alex.locator('.apart-list li',{hasText:'Bea and Cara'}).waitFor();

// 7. The calendar file.
const cal=await alex.evaluate(async()=>{const a=document.querySelector('a[href^="/calendar/"]'),r=await fetch(a.getAttribute('href'));return {status:r.status,type:r.headers.get('content-type'),text:await r.text()}});
assert.equal(cal.status,200);assert(cal.type.startsWith('text/calendar'));assert(cal.text.includes('BEGIN:VCALENDAR')&&cal.text.includes('SUMMARY:Features\\, party'));

// Close entries (the dialog mentions the pair) and draw.
await alex.getByRole('button',{name:'Close entries',exact:true}).click();await alex.getByText('1 pair kept apart will not draw each other.').waitFor();await alex.getByRole('dialog').getByRole('button',{name:'Close entries for 4 people',exact:true}).click();
const toName={};
for(const name of names){const p=pages[name];await p.goto(exchangeURL);await p.reload();await p.getByRole('button',{name:'Draw a name'}).click();await p.locator('.my-ticket').waitFor();await p.reload();await p.getByRole('button',{name:/^Hold to see/}).focus();await p.keyboard.press('Enter');toName[name]=await p.locator('#to-name').textContent();assert(names.includes(toName[name])&&toName[name]!==name)}
assert.notEqual(toName.Bea,'Cara');assert.notEqual(toName.Cara,'Bea');
const giverOf=n=>names.find(g=>toName[g]===n),R=toName.Alex,rp=pages[R],G=giverOf('Alex');

// 2. Anonymous messages: Alex asks, the person Alex buys for answers without learning who asked.
await alex.reload();await alex.getByRole('heading',{name:'Ask the person you’re buying for'}).waitFor();
await alex.getByRole('button',{name:'Send question'}).click();await alex.getByText('Write your question first.').waitFor();
await alex.getByLabel('Your question').fill('What colour do you like?');await alex.getByRole('button',{name:'Send question'}).click();await alex.locator('.msg.mine',{hasText:'What colour do you like?'}).waitFor();
assert.equal(await alex.evaluate(()=>document.activeElement.id),'msg-to','focus not returned to the message box');await audit(alex);
await rp.goto(base+'/#exchanges');await rp.getByText('New message.').waitFor();
await rp.goto(exchangeURL);await rp.reload();await rp.getByRole('heading',{name:/^Your secret giver/}).waitFor();await rp.locator('.msg',{hasText:'What colour do you like?'}).waitFor();
assert(!(await rp.locator('#talk').textContent()).includes('Alex'),'the giver is named in the conversation');
await shot(rp,'secret-giver');await rp.getByLabel('Your reply').fill('Green, please');await rp.getByRole('button',{name:'Send reply'}).click();await rp.locator('.msg.mine',{hasText:'Green, please'}).waitFor();await audit(rp);
await alex.reload();await alex.locator('.msg:not(.mine)',{hasText:'Green, please'}).waitFor();
for(const other of names.filter(n=>n!==R&&n!=='Alex'&&n!==G)){const p=pages[other];await p.goto(exchangeURL);await p.reload();await p.getByRole('heading',{name:'People'}).waitFor();assert(!(await p.locator('body').textContent()).includes('What colour do you like?'),other+' can read someone else’s conversation')}

// 3. Claiming an idea. The owner is told nothing.
await shot(alex,'giver-before-claim');await alex.getByRole('button',{name:`I’m getting this: ${R} teapot`}).click();await alex.getByText('You’re getting this.').waitFor();
assert.equal(await alex.evaluate(()=>document.activeElement.textContent),'Undo','focus lost after claiming');await audit(alex);
await rp.goto(base+'/#wishes');await rp.getByRole('heading',{name:R+' teapot'}).waitFor();
assert(!/getting this|Someone else is getting|I’m getting/.test(await rp.locator('main').textContent()),'the owner can see the claim');

// 5. Sorting an idea hides it from the giver, who is warned about one they had claimed.
await rp.getByRole('button',{name:`Sorted: ${R} scarf`}).click();await audit(rp);await rp.getByRole('dialog').getByRole('button',{name:'I’ve got it'}).click();
await rp.getByRole('heading',{name:'Sorted',exact:true}).waitFor();await rp.getByText('You’ve got this',{exact:true}).waitFor();
await rp.getByRole('button',{name:`Sorted: ${R} teapot`}).click();await rp.getByRole('dialog').getByRole('button',{name:'I don’t want it any more'}).click();await rp.getByText('You don’t want this any more').waitFor();await audit(rp);await shot(rp,'sorted');
await alex.reload();await alex.locator('#their-wishes').waitFor();
assert.equal(await alex.getByRole('heading',{name:R+' scarf'}).count(),0,'a sorted idea is still shown to the giver');
await alex.getByText('They don’t want this any more. Check before you buy it.').waitFor();
await rp.getByRole('button',{name:`Put back: ${R} teapot`}).click();await rp.getByRole('heading',{name:'Sorted',exact:true}).waitFor();
await alex.reload();await alex.getByRole('button',{name:`Undo: you’re getting ${R} teapot`}).waitFor();

// 4. Reveal day: only the organiser is offered it, and it shows who bought for whom.
await pages.Bea.goto(exchangeURL);await pages.Bea.reload();await pages.Bea.getByRole('heading',{name:'Your wish list'}).waitFor();assert.equal(await pages.Bea.getByRole('button',{name:'Reveal names'}).count(),0);
await shot(alex,'before-reveal');await alex.setViewportSize({width:390,height:844});await audit(alex);assert(await alex.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile overflow');
await alex.getByRole('button',{name:'Reveal names'}).click();await alex.getByRole('dialog').getByRole('button',{name:'Reveal who had whom',exact:true}).click();
await alex.getByText('The organiser has revealed the names.').waitFor();await shot(alex,'revealed-mobile');
assert.equal(await alex.getByRole('button',{name:'Reveal names'}).count(),0,'reveal offered twice');
const rows=await alex.locator('#people .tk:not(.open)').evaluateAll(l=>l.map(li=>({name:li.querySelector('.name').textContent,drew:li.querySelector('.drew')?.textContent.replace('Bought for ','')})));
for(const r of rows)assert.equal(r.drew,toName[r.name],`${r.name} was shown as buying for the wrong person`);
await audit(alex);
await pages.Bea.goto(exchangeURL);await pages.Bea.reload();await pages.Bea.getByText(`${giverOf('Bea')} bought for you.`).waitFor();
assert.deepEqual(errors,[]);
console.log('PASS: keep apart, anonymous messages, claims, sorted ideas, calendar file, reveal day');
await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
