// Admin page and account deletion against a server started with GIFTY_SMTP_HOST=log and GIFTY_ADMINS=boss@example.com.
// GIFTY_TEST_URL=http://127.0.0.1:8091 GIFTY_MAIL_LOG=server.log node tests/admin.cjs
const {chromium}=require('playwright');const AxeBuilder=require('@axe-core/playwright').default;const fs=require('fs');const assert=require('assert/strict');
const base=process.env.GIFTY_TEST_URL||'http://127.0.0.1:8091',out=process.env.GIFTY_SCREENSHOTS||'/tmp/gifty-browser',logf=process.env.GIFTY_MAIL_LOG;if(!logf)throw Error('Set GIFTY_MAIL_LOG to the server log file.');fs.mkdirSync(out,{recursive:true});
const mails=()=>fs.readFileSync(logf,'utf8').replace(/=\r?\n/g,'').replace(/=([0-9A-F]{2})/g,(_,h)=>String.fromCharCode(parseInt(h,16)));
const audit=async p=>{await p.waitForTimeout(200);const r=await new AxeBuilder({page:p}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(r.violations.map(v=>v.id+' '+v.nodes.map(n=>n.target).join()),[])};
const pw='a long enough password';
(async()=>{const b=await chromium.launch({...(process.env.GIFTY_CHROME?{executablePath:process.env.GIFTY_CHROME}:{}),headless:true});const errors=[];
const sign=async(name,email)=>{const c=await b.newContext({locale:process.env.GIFTY_LOCALE||'en-GB',viewport:{width:390,height:844}});const p=await c.newPage();p.on('pageerror',e=>errors.push(e.message));await p.goto(base+'/#signup');await p.getByLabel('Your name').fill(name);await p.getByLabel('Email address').fill(email);await p.getByLabel('Password',{exact:true}).fill(pw);await p.getByRole('button',{name:'Create an account'}).click();await p.getByRole('link',{name:'Account',exact:true}).waitFor();return p};
const s=Date.now(),boss=await sign('Boss','boss@example.com').catch(()=>null);
// The admin address must be confirmed first.
const bp=boss||(()=>{throw Error('could not sign up the admin; use a fresh data file')})();
assert.equal(await bp.getByRole('link',{name:'Admin',exact:true}).count(),0);
const m=mails().split('email to ').filter(x=>x.startsWith('boss@example.com')).at(-1).match(/#verify\/([A-Za-z0-9_-]+)/);await bp.goto(base+'/#verify/'+m[1]);await bp.getByText('Email address confirmed',{exact:false}).first().waitFor();await bp.reload();
await bp.getByRole('link',{name:'Admin',exact:true}).click();await bp.getByRole('heading',{name:'Admin'}).waitFor();
const guest=await sign('Guest '+s,`guest${s}@example.com`);
await bp.reload();await bp.getByText(`guest${s}@example.com`).waitFor();await audit(bp);await bp.screenshot({path:out+'/admin.png',fullPage:true});
await bp.getByRole('button',{name:'Delete Guest '+s}).click();await bp.getByRole('button',{name:'Delete account'}).click();await bp.getByText('Deleted.').waitFor();assert.equal(await bp.locator('#main').getByText(`guest${s}@example.com`).count(),0);
// A guest has no admin page.
const g2=await sign('Other '+s,`other${s}@example.com`);await g2.goto(base+'/#admin');assert.equal(await g2.getByRole('heading',{name:'Admin'}).count(),0);
// Self-service deletion.
await g2.getByRole('link',{name:'Account',exact:true}).click();await g2.getByRole('button',{name:'Delete account'}).click();await g2.getByLabel('Your password',{exact:true}).fill('wrong password here');await g2.getByRole('button',{name:'Delete my account'}).click();await g2.getByText('That password isn’t right.').waitFor();await audit(g2);await g2.screenshot({path:out+'/delete-account.png'});
await g2.getByLabel('Your password',{exact:true}).fill(pw);await g2.getByRole('button',{name:'Delete my account'}).click();await g2.getByText('Your account has been deleted.').waitFor();
await g2.goto(base+'/#login');await g2.getByLabel('Email address').fill(`other${s}@example.com`);await g2.getByLabel('Password',{exact:true}).fill(pw);await g2.getByRole('button',{name:'Sign in'}).click();await g2.getByText('Email or password is incorrect.').waitFor();
assert.deepEqual(errors,[]);await b.close();console.log('admin ok');})().catch(e=>{console.error(e);process.exit(1)});
