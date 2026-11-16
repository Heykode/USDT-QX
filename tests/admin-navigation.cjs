const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs/promises');
const path = require('node:path');
const assert = require('node:assert/strict');
const output = process.env.QX_UI_OUTPUT || require('node:os').tmpdir() + '/usdt-qx-navigation';
const source = path.resolve(__dirname, '../src/www');
const config = {enabled:false,currency:'cny',coingecko_plan:'pro',min_price:0,max_price:0,reference_price:0,max_spread_percent:1,max_jump_percent:2,refresh_seconds:300,max_age_seconds:600,max_time_skew_seconds:180,fallback_enabled:false,fallback_price:0,fallback_expires_at:''};
(async()=>{
 await fs.mkdir(output, {recursive:true});
 const browser = await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 const context = await browser.newContext({viewport:{width:1440,height:1100}});
 await context.addCookies([{name:'access_token',value:encodeURIComponent(JSON.stringify('local-mock')),url:'http://qx.local'},{name:'PARAGLIDE_LOCALE',value:'zh-CN',url:'http://qx.local'}]);
 await context.addInitScript(()=>localStorage.setItem('auth_user',JSON.stringify({username:'demo@example.test',role:'admin'})));
 const requests=[],errors=[],mutations=[],documents=[];
 let expired=false, failSave=false, releaseDelayed;
 let delayRates=false;
 let security={username:'demo@example.test',mfa_enabled:true,recovery_codes_remaining:8};
 const fixtures={
  '/admin/api/v1/auth/me':{username:'demo@example.test',role:'admin'},
  '/admin/api/v1/dashboard/overview':{total_asset:0,volume:0,order_count:0,success_rate:0,seven_day_volume:0,active_addresses:0,online_chains:1},
  '/admin/api/v1/dashboard/order-stats':{order_count:0,success_count:0,success_rate:0,avg_payment_seconds:0,expired_unpaid_count:0},
  '/admin/api/v1/dashboard/asset-trend':[],
  '/admin/api/v1/dashboard/revenue-trend':[],
  '/admin/api/v1/dashboard/recent-orders':[],
  '/admin/api/v1/dashboard/rpc-stats':{generated_at:'2026-11-16T12:00:00Z',summary:{total_chains:1,enabled_chains:1,active_chains:1,ok_chains:1,warning_chains:0,down_chains:0,unknown_chains:0,disabled_chains:0,success_count:5,failure_count:0,success_rate:1},chains:[{network:'binance',display_name:'BSC',enabled:true,has_data:true,status:'ok',success_count:5,failure_count:0,success_rate:1,latest_block_height:133900000,last_sync_at:'2026-11-16T12:00:00Z'}]},
  '/admin/api/v1/settings/rate/verified':{mode:'auto',config,cmc_key_configured:false,coingecko_key_configured:false,usable:false,fallback_available:false},
  '/admin/api/v1/notification-channels':[],
  '/admin/api/v1/auth/security':security,
  '/admin/api/v1/settings':[],
  '/admin/api/v1/settings/rate/status':{mode:'auto',cache_ttl_seconds:300,bases:[]}
 };
 await context.route('**/*',async route=>{
  const u=new URL(route.request().url());
  if(u.host!=='qx.local')return route.abort();
  if(route.request().isNavigationRequest())documents.push(u.pathname);
  if(u.pathname.startsWith('/admin/api/')){
   requests.push(u.pathname);
   const method=route.request().method();
   const json=data=>route.fulfill({contentType:'application/json',body:JSON.stringify({status_code:200,message:'success',data})});
   if(expired)return route.fulfill({status:401,contentType:'application/json',body:'{}'});
   if(method!=='GET'){
    mutations.push({path:u.pathname,body:route.request().postDataJSON(),authorization:route.request().headers().authorization});
    if(failSave)return route.fulfill({status:500,contentType:'application/json',body:'{}'});
    if(u.pathname==='/admin/api/v1/settings/rate/verified'){
     Object.assign(config,route.request().postDataJSON());return json(fixtures[u.pathname]);
    }
    if(u.pathname==='/admin/api/v1/auth/mfa/recovery-codes')return json({token:'rotated-mock',recovery_codes:['TEST-RECOVERY-01','TEST-RECOVERY-02']});
    if(u.pathname==='/admin/api/v1/auth/mfa/setup')return json({secret:'TEST-ENROLLMENT-SECRET',qr_png:'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==',expires_at:'2026-11-17T12:00:00Z'});
    if(u.pathname==='/admin/api/v1/auth/logout')return json({});
   }
   if(delayRates&&u.pathname==='/admin/api/v1/settings/rate/verified')await new Promise(resolve=>{releaseDelayed=resolve;});
   if(!Object.hasOwn(fixtures,u.pathname))throw Error('Unmocked API '+u.pathname);
   const envelope={status_code:200,message:'success',data:fixtures[u.pathname]};
   return route.fulfill({contentType:u.pathname.endsWith('/rpc-stats')?'text/event-stream':'application/json',body:u.pathname.endsWith('/rpc-stats')?'event: rpc_stats\ndata: '+JSON.stringify(envelope)+'\n\n':JSON.stringify(envelope)});
  }
  try{
   const p=u.pathname.includes('.')?u.pathname:'/index.html';
   const body=await fs.readFile(path.join(source,p));
   return route.fulfill({body,contentType:p.endsWith('.js')?'application/javascript':p.endsWith('.css')?'text/css':p.endsWith('.html')?'text/html':p.endsWith('.png')?'image/png':undefined});
  }catch{return route.fulfill({status:404});}
 });
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 const reports=[];
 const record=name=>reports.push(name);
 const loaded=async()=>page.locator('#splash').waitFor({state:'detached'});
 const openMenu=async name=>{const button=page.getByRole('button',{name,exact:true});if(await button.getAttribute('aria-expanded')!=='true')await button.click();};
 const rates=page.locator('[data-qx-page="qx-rate-guard"]');
 const sec=page.locator('[data-qx-page="qx-security"]');
 const goRates=async()=>{await openMenu('系统配置');await page.locator('[data-qx-entry="rates"] a,a[data-qx-entry="rates"]').first().click();await rates.locator('#status').waitFor();};
 const goSecurity=async()=>{await openMenu('账户安全');await page.locator('[data-qx-entry="security"] a,a[data-qx-entry="security"]').first().click();await sec.locator('#account').getByText('demo@example.test',{exact:true}).waitFor();};
 await page.goto('http://qx.local/dashboard');await loaded();
 await page.getByText('133,900,000',{exact:true}).first().waitFor();
 await page.evaluate(()=>{window.initialDocument=document;window.shell=document.querySelector('[data-slot="sidebar-wrapper"]')});
 await goRates();await page.getByText('普通自动模式',{exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('[data-qx-entry="rates"] a')?.dataset.active==='true');
 assert.equal(await page.evaluate(()=>window.initialDocument===document&&window.shell===document.querySelector('[data-slot="sidebar-wrapper"]')),true);
 await page.screenshot({path:output+'/rates-desktop.png',fullPage:true});record('Sidebar to rates preserves document and shell; correct active item');
 // Save unchanged disabled policy, then a server failure, without external writes.
 await rates.getByRole('button',{name:'保存设置',exact:true}).click();
 await rates.getByText('已保存，请查看上方正在生效的设置。',{exact:true}).waitFor();
 assert.equal(mutations.length,1);assert.equal(mutations[0].body.enabled,false);assert.equal(mutations[0].body.refresh_seconds,300);
 failSave=true;await rates.getByRole('button',{name:'保存设置',exact:true}).click();
 await rates.locator('#notice[data-error="true"]').waitFor();failSave=false;
 record('Rate save payload and visible server error');
 // Switch to original payment page: same section header, same document.
 await page.evaluate(()=>window.sectionHeader=document.querySelector('header'));
 await page.locator('aside a[href="/settings/pay"]').click();await page.waitForURL('**/settings/pay');
 await page.getByText('汇率服务状态',{exact:true}).waitFor();
 assert.equal(await page.evaluate(()=>window.sectionHeader===document.querySelector('header')),true);
 await page.locator('aside a[href="/settings/verified-rates"]').click();await rates.locator('#status').waitFor();
 record('Original payment settings and native section links retain header');
 await goSecurity();await sec.getByText('已启用两步验证',{exact:true}).waitFor();
 await sec.locator('[name="password"]').fill('mock-password');await sec.locator('[name="code"]').fill('123456');
 await sec.locator('#rotate').click();await sec.getByText('TEST-RECOVERY-01',{exact:true}).waitFor();
 assert.equal(await page.evaluate(async()=>(await import('/assets/auth-store-uaDKRR4l.js')).t.state.accessToken),'rotated-mock');
 assert.equal(JSON.parse(decodeURIComponent((await context.cookies()).find(c=>c.name==='access_token').value)),'rotated-mock');
 await page.screenshot({path:output+'/security-desktop.png',fullPage:true});
 await sec.locator('a.secondary-link').click();await page.waitForURL('**/account/password');await page.locator('[name=old_password]').waitFor();
 await page.goBack();await sec.locator('#account').getByText('demo@example.test',{exact:true}).waitFor();
 assert.equal(await sec.locator('#recovery').isHidden(),true);assert.equal(await sec.locator('[name="password"]').inputValue(),'');
 assert.equal(await sec.getByText('TEST-RECOVERY-01').count(),0);
 record('Recovery code rotation syncs session; navigating away and back clears secrets');
 await page.goBack();await rates.locator('#status').waitFor();await page.goForward();await sec.locator('#status').waitFor();
 assert.equal(await page.evaluate(()=>window.initialDocument===document&&window.shell===document.querySelector('[data-slot="sidebar-wrapper"]')),true);
 assert.deepEqual(documents,['/dashboard']);record('History back/forward without document navigation');
 await page.reload();await loaded();await sec.getByText('已启用两步验证',{exact:true}).waitFor();record('Direct route reload');
 // Enrollment cleanup, including browser pagehide.
 security.mfa_enabled=false;
 await goRates();await goSecurity();await sec.locator('[name="password"]').fill('mock-password');await sec.locator('#setup').click();
 await sec.getByText('TEST-ENROLLMENT-SECRET',{exact:true}).waitFor();
 await page.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pagehide')));
 assert.equal(await sec.locator('#enrollment').isHidden(),true);assert.equal(await sec.locator('#secret').textContent(),'');
 await goRates();await goSecurity();assert.equal(await sec.locator('#enrollment').isHidden(),true);record('Enrollment secrets cleared on pagehide and route change');
 // Verify theme comes from the actual shell, including a manual override.
 await page.emulateMedia({reducedMotion:'reduce'});
 await page.locator('header button').filter({has:page.locator('.lucide-sun')}).click();
 await page.getByRole('menuitem',{name:/深色/}).click();await page.getByRole('menu').waitFor({state:'detached'});
 await page.waitForFunction(()=>document.querySelector('[data-qx-page]')?.dataset.qxMode==='dark');
 const colors=await sec.evaluate(el=>({text:getComputedStyle(el).color,card:getComputedStyle(el.shadowRoot.querySelector('.card')).backgroundColor,body:getComputedStyle(document.body).backgroundColor}));
 assert.equal(colors.card,colors.body);await page.screenshot({path:output+'/security-dark.png',fullPage:true});record('Theme inheritance');
 await page.locator('header button').filter({has:page.locator('.lucide-sun')}).click();await page.getByRole('menuitem',{name:/浅色/}).click();await page.getByRole('menu').waitFor({state:'detached'});
 // Slow API must not write into a different page or leave an unhandled rejection.
 delayRates=true;await goRates();await page.waitForFunction(()=>document.querySelector('[data-qx-page="qx-rate-guard"]')?.shadowRoot?.querySelector('#policy'));
 await goSecurity();releaseDelayed?.();delayRates=false;
 assert.equal(await sec.locator('#secret').textContent(),'');record('In-flight request cancellation on route change');
 // Collapsed sidebar and the top account menu are portalled outside #app.
 await page.locator('[data-sidebar="trigger"]').click();
 await page.getByRole('button',{name:'系统配置',exact:true}).click();
 await page.getByRole('menu').locator('a[href="/settings/verified-rates"]').click();
 await rates.getByText('普通自动模式',{exact:true}).waitFor();await page.getByRole('menu').waitFor({state:'detached'});
 await page.locator('header button:has([data-slot="avatar"])').click();
 await page.getByRole('menuitem',{name:'修改密码',exact:true}).focus();await page.keyboard.press('ArrowDown');
 assert.equal(await page.getByRole('menuitem',{name:'登录安全',exact:true}).evaluate(el=>el===document.activeElement),true);await page.keyboard.press('Enter');
 await sec.locator('#account').getByText('demo@example.test',{exact:true}).waitFor();await page.getByRole('menu').waitFor({state:'detached'});
 await page.locator('[data-sidebar="trigger"]').click();record('Collapsed sidebar and keyboard-activated account menu navigate and dismiss');
 await openMenu('系统配置');await openMenu('账户安全');await page.locator('[data-sidebar="content"] [data-qx-entry="security"]').waitFor();
 assert.equal(await page.locator('[data-sidebar="content"] [data-qx-entry="rates"]').count(),1);
 assert.equal(await page.locator('[data-sidebar="content"] [data-qx-entry="security"]').count(),1);record('Repeated visits do not duplicate sidebar entries');
 // Existing bookmarks enter the same authenticated layout.
 await page.goto('http://qx.local/qx-rate-guard.html');await page.waitForURL('**/settings/verified-rates');await loaded();await rates.getByText('普通自动模式',{exact:true}).waitFor();
 await page.goto('http://qx.local/qx-security.html');await page.waitForURL('**/account/security');await loaded();await sec.locator('#account').getByText('demo@example.test',{exact:true}).waitFor();record('Both legacy bookmarks');
 // Mobile native selector and sidebar drawer.
 await page.setViewportSize({width:390,height:844});await page.reload();await loaded();await sec.locator('#status').waitFor();
 const selector=page.locator('aside').getByRole('combobox');assert.match(await selector.textContent(),/登录安全/);
 await selector.click();await page.getByRole('option',{name:/修改密码/}).click();await page.waitForURL('**/account/password');
 await page.locator('aside').getByRole('combobox').click();await page.getByRole('option',{name:/登录安全/}).click();await sec.locator('#status').waitFor();
 await page.locator('[data-sidebar="trigger"]').click();await openMenu('系统配置');
 await page.locator('[data-mobile="true"] a[href="/settings/verified-rates"]').click();await rates.getByText('普通自动模式',{exact:true}).waitFor();
 await page.locator('[data-mobile="true"]').waitFor({state:'detached'});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 const scroll=await rates.evaluate(el=>({height:el.clientHeight,scroll:el.scrollHeight,width:el.clientWidth,scrollWidth:el.scrollWidth}));assert.ok(scroll.scroll>scroll.height);assert.ok(scroll.scrollWidth<=scroll.width);
 await rates.getByRole('button',{name:'保存设置',exact:true}).scrollIntoViewIfNeeded();assert.equal(await rates.getByRole('button',{name:'保存设置',exact:true}).isVisible(),true);
 await rates.evaluate(el=>el.scrollTop=0);await page.screenshot({path:output+'/rates-mobile.png',fullPage:true});record('Mobile selector, drawer dismissal, no horizontal overflow and scrollable form');
 // English follows the same cookie used by upstream language selection.
 await context.addCookies([{name:'PARAGLIDE_LOCALE',value:'en-US',url:'http://qx.local'}]);await page.reload();await loaded();
 await rates.getByRole('heading',{name:'Verified USDT rates',exact:true}).waitFor();assert.match(await page.locator('aside').getByRole('combobox').textContent(),/Verified rates/);
 await page.screenshot({path:output+'/rates-mobile-en.png',fullPage:true});record('English page and mobile menu');
 // Session expiry must return to sign-in, never render an anonymous settings page.
 expired=true;await page.reload();await page.waitForURL('**/sign-in?*');await page.locator('[name=username]').waitFor();record('Expired session redirects to sign-in');expired=false;
 await context.clearCookies();await page.goto('http://qx.local/account/security');await page.waitForURL('**/sign-in?*');await page.locator('[name=username]').waitFor();record('Anonymous direct route denied');
 assert.deepEqual(errors,[]);assert.equal(context.pages().length,1);
 await fs.writeFile(output+'/verification.json',JSON.stringify({mock_api_only:true,real_payment:false,checks:reports,errors,mutations:mutations.map(({path,body})=>({path,body})),documents},null,2));
 console.log(JSON.stringify({passed:reports.length,checks:reports,errors,output},null,2));await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
