// Access gate against a server started with GIFTY_ACCESS_CODE='test gate code'.
// GIFTY_TEST_URL=http://127.0.0.1:8090 node tests/gate.cjs
const {chromium}=require('playwright');const AxeBuilder=require('@axe-core/playwright').default;const assert=require('assert/strict');
const fs=require('node:fs');const base=process.env.GIFTY_TEST_URL||'http://127.0.0.1:8090',out=process.env.GIFTY_SCREENSHOTS||'/tmp/gifty-browser';fs.mkdirSync(out,{recursive:true});
const audit=async p=>{await p.waitForTimeout(200);const r=await new AxeBuilder({page:p}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(r.violations.map(v=>v.id),[])};
(async()=>{const b=await chromium.launch({...(process.env.GIFTY_CHROME?{executablePath:process.env.GIFTY_CHROME}:{}),headless:true});const errors=[];
const p=await (await b.newContext({viewport:{width:390,height:844}})).newPage();p.on('pageerror',e=>errors.push(e.message));
await p.goto(base);await p.getByRole('heading',{name:'Gifty is invite-only for now'}).waitFor();assert.equal(await p.getByRole('link',{name:'Create an account'}).count(),0);await audit(p);await p.screenshot({path:out+'/gate-mobile.png'});
await p.goto(base+'/#signup');await p.getByRole('heading',{name:'Gifty is invite-only for now'}).waitFor();
// The API refuses signup without access, whatever the page shows.
const direct=await p.evaluate(async()=>(await fetch('/api/signup',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:'x',email:'x@example.com',password:'a long enough password'})})).status);assert.equal(direct,403);
await p.getByLabel('Access code').fill('wrong code');await p.getByRole('button',{name:'Continue'}).click();await p.getByText('That code isn’t right.').waitFor();
await p.getByLabel('Access code').fill('test gate code');await p.getByRole('button',{name:'Continue'}).click();await p.getByRole('heading',{name:'Create an account'}).waitFor();
await p.getByLabel('Your name').fill('Ana');await p.getByLabel('Email address').fill('ana'+Date.now()+'@example.com');await p.getByLabel('Password',{exact:true}).fill('a long enough password');await p.getByRole('button',{name:'Create an account'}).click();await p.getByRole('heading',{name:'Exchanges',exact:true}).waitFor();
const e=await p.evaluate(async()=>(await fetch('/api/exchanges',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:'Gate test',date:'2099-12-20',budget:'20',currency:'GBP',note:''})})).json());
// An invited guest goes straight through without the code.
const g=await (await b.newContext({viewport:{width:390,height:844}})).newPage();g.on('pageerror',e=>errors.push(e.message));await g.goto(base+'/#invite/'+e.invite);await g.getByRole('link',{name:'Create an account to join'}).click();await g.getByRole('heading',{name:'Create an account'}).waitFor();
await g.getByLabel('Your name').fill('Ben');await g.getByLabel('Email address').fill('ben'+Date.now()+'@example.com');await g.getByLabel('Password',{exact:true}).fill('a long enough password');await g.getByRole('button',{name:'Create an account'}).click();await g.getByRole('button',{name:/^Join as /}).click();await g.getByRole('heading',{name:'Gate test'}).waitFor();
const h=await (await fetch(base+'/')).headers;assert.equal(h.get('cross-origin-opener-policy'),'same-origin');assert(h.get('permissions-policy').includes('camera=()'));
assert.deepEqual(errors,[]);console.log('PASS gate');await b.close()})().catch(e=>{console.error(e);process.exit(1)});
