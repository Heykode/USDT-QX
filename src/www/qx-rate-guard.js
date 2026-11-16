const $ = (id) => document.getElementById(id);
const form = $('policy');
let language=(document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1]||localStorage.getItem('qx-rate-language')||navigator.language).startsWith('zh')?'zh':'en';
const zh = Object.fromEntries([...document.querySelectorAll('[data-i18n]')].map(el => [el.dataset.i18n, el.textContent]));
const en = {
 navHome:'USDT-QX · Admin',navPay:'Payment settings',navRates:'Verified rates',navSecurity:'Login security',effectiveTitle:'Effective settings',savedState:'This status reflects saved settings. Form changes take effect after saving.',setupTitle:'To enable verification',sourceNote:'The built-in default source updates daily. Its primary and backup addresses serve the same data; only the two providers on this page perform a cross-provider price check.',
 fallbackTitle:'Per-order fixed fallback',fallbackEnable:'Use a fixed price when an automatic quote is unavailable',fallbackNote:'Each quote tries both providers first. A still-valid trusted quote can be reused; only an unusable automatic quote falls back. The next order tries again without changing the global mode. Enabling this increases provider requests.',fallbackPrice:'Fixed price: billing currency per 1 USDT',fallbackExpiry:'Fixed price valid until (local time, optional)',fallbackExpiryNote:'Leave blank for no expiry. A continuing outage may still cause consecutive orders to use the fixed price. Keep alerts enabled and review the price.',back:'← Back to rate settings',title:'Verified USDT rates',subtitle:'CoinMarketCap quotes, checked against CoinGecko. Automatic quotes first, with optional per-order fixed fallback.',current:'Provider verification history',spread:'Provider difference',refresh:'Check providers now',policy:'Quote protection',enable:'Enable in automatic mode',scope:'Converts the selected billing currency to USDT on all USDT payment networks. Other automatic pairs will be unavailable when enabled. Fixed mode keeps its existing rules.',currency:'Order billing currency',plan:'CoinGecko plan',keys:'Keys are never displayed. Leave blank to keep a saved key. Use a plan licensed for your commercial use; selecting Demo does not establish commercial permission.',bounds:'Price limits · billing currency per 1 USDT',reference:'Manually verified reference',min:'Minimum acceptable price',max:'Maximum acceptable price',referenceNote:'The reference is set by the administrator and does not drift with provider data. Review and update it after a genuine large market move.',maxSpread:'Maximum provider difference (%)',maxJump:'Maximum reference / previous quote change (%)',interval:'Refresh interval (seconds)',maxAge:'Maximum source quote age (seconds)',skew:'Maximum provider time difference (seconds)',failureNote:'Temporary outages may use a still-valid verified quote. Conflicting or anomalous prices immediately pause quotes. When enabled, a fixed price may cover the current order. Thresholds are adjustable starting values, not guarantees.',save:'Save settings',saveNote:'Saving changed settings invalidates the old configuration’s quotes until rechecked.',alerts:'Rate alerts',alertsNote:'Subscribe existing notification channels to rate alerts. Continuing failures notify at most every 15 minutes. The notification channel must also be enabled.',footer:'Discounts apply before currency conversion. Existing orders keep their payable amount.'
};
const t = (cn,en) => language==='zh'?cn:en;
let latest;
function applyLanguage(){
 document.documentElement.lang=language==='zh'?'zh-CN':'en';
 document.title='USDT-QX · '+t('双源汇率','Verified rates');
 for(const el of document.querySelectorAll('[data-i18n]')) el.textContent=(language==='zh'?zh:en)[el.dataset.i18n];
 $('language').textContent=language==='zh'?'English':'中文';
 if(latest)render(latest,false);
}
$('language').onclick=()=>{language=language==='zh'?'en':'zh';localStorage.setItem('qx-rate-language',language);document.cookie='PARAGLIDE_LOCALE='+(language==='zh'?'zh-CN':'en-US')+'; Path=/; SameSite=Lax; Max-Age=34560000';message('');applyLanguage();loadChannels();};
function message(text,error=false){$('notice').textContent=text;$('notice').dataset.error=String(error);}
function token(){try {const raw=document.cookie.split('; ').find(v=>v.startsWith('access_token='));return raw?JSON.parse(decodeURIComponent(raw.slice('access_token='.length))):'';}catch{return '';}}
async function api(path,method='GET',body){
 const accessToken=token();
 if(!accessToken){location.replace('/sign-in?redirect='+encodeURIComponent('/qx-rate-guard.html'));throw Error(t('请先登录。','Please sign in.'));}
 const response=await fetch('/admin/api/v1'+path,{method,headers:{Authorization:'Bearer '+accessToken,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),cache:'no-store'});
 if(response.status===401){location.replace('/sign-in?redirect='+encodeURIComponent('/qx-rate-guard.html'));throw Error(t('登录已过期。','Session expired.'));}
 const result=await response.json();
 if(!response.ok || (result.status_code!==undefined && result.status_code!==200 && result.status_code!==0)) throw Error(t('保存或请求失败，请检查配置和登录状态。','Request failed. Check the configuration and session.'));
 return result.data;
}
const time=(v)=>!v||v.startsWith('0001-')?'—':new Date(v).toLocaleString(language==='zh'?'zh-CN':'en-US');
function render(data,fill=true){
 latest=data;
 if(fill)for(const [key,value]of Object.entries(data.config)){const input=form.elements.namedItem(key);if(!input)continue;if(input.type==='checkbox')input.checked=value;else if(key==='fallback_expires_at'){const d=value?new Date(value):null;input.value=d?new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16):'';}else input.value=value;}
 if(fill){form.elements.cmc_api_key.value='';form.elements.coingecko_api_key.value='';}
 const keyState=(saved)=>saved?t('已保存；留空保留','Saved; leave blank to keep'):t('尚未配置','Not configured');
 $('cmc-key-state').textContent=keyState(data.cmc_key_configured);$('cg-key-state').textContent=keyState(data.coingecko_key_configured);
 const state=data.state||{};
 const active=data.mode==='auto'&&data.config.enabled;
 const available=active&&data.usable;
 const fallbackAvailable=active&&data.fallback_available;
 $('status').textContent=!active?(data.mode==='fixed'?t('固定汇率模式','Fixed rate mode'):t('普通自动模式','Single-source automatic mode')):available?t('双源报价可用','Verified quote available'):fallbackAvailable?t('自动价异常 · 可本单回退','Automatic unavailable · fallback available'):t('双源报价暂停／待核验','Verified quotes paused / awaiting check');
 $('status').dataset.state=available?'ok':active?'stop':'';
 $('mode-note').textContent=data.mode==='fixed'?t('当前按支付设置中的固定汇率收款。本页双源核验不参与换算。','Payments use the fixed rates in Payment settings. Provider verification on this page does not participate in conversion.'):!active?t('当前使用支付设置中的普通自动汇率。双源核验尚未启用，不会由两家供应商对价。','Payments use the single-source automatic rates in Payment settings. Two-provider verification is not enabled.'):t('当前使用双源自动汇率：CoinMarketCap 报价，CoinGecko 核验，通过检查后用于新订单。','Payments use verified automatic rates: CoinMarketCap supplies the quote, checked against CoinGecko before use by new orders.');
 $('fallback-note').textContent=!active?t('本页固定价回退当前不生效；它需要同时开启自动模式和双源核验。','The fixed fallback on this page is inactive; it requires both automatic mode and provider verification.'):!data.config.fallback_enabled?t('本单固定价回退未开启。自动报价不可用时，新的付款报价会被拒绝。','Per-order fixed fallback is disabled. New payment quotes are refused when no automatic quote is usable.'):data.usable?t('已开启固定价回退；当前自动报价可用，仍优先使用自动价。需要回退时会重新检查固定价和有效期。','Fixed fallback is enabled. Automatic pricing is currently usable and takes priority; the fixed price and expiry are checked if fallback becomes necessary.'):data.fallback_available?t('本单固定价回退已就绪。每笔先检查自动价，只有自动价不可用才回退，下一笔重新尝试。','Per-order fixed fallback is ready. Each quote checks automatic pricing first; fallback applies only when it is unavailable, and the next quote tries again.'):t('已开启固定价回退，但当前不可用。请检查固定价、允许范围和有效期。','Fixed fallback is enabled but currently unavailable. Check its price, allowed range and expiry.');
 const steps=[];
 const missingKeys=[!data.cmc_key_configured&&'CoinMarketCap',!data.coingecko_key_configured&&'CoinGecko'].filter(Boolean);
 if(missingKeys.length)steps.push(t('填写并保存 ','Enter and save ')+missingKeys.join(' / ')+t(' 的 API Key。',' API keys.'));
 const cfg=data.config;
 if(!(cfg.min_price>0&&cfg.max_price>cfg.min_price&&cfg.reference_price>=cfg.min_price&&cfg.reference_price<=cfg.max_price))steps.push(t('填写人工核对的参考价和合理价格上下限。','Set a manually verified reference price and acceptable price limits.'));
 if(!cfg.enabled)steps.push(t('打开“在自动模式下启用”，保存设置。','Turn on “Enable in automatic mode” and save.'));
 if(data.mode!=='auto')steps.push(t('在支付设置中选择自动汇率模式。','Select automatic rate mode in Payment settings.'));
 if(steps.length)steps.push(t('完成后点击“重新核验”，确认报价可用。','Then run “Check providers now” and confirm the quote is available.'));
 $('setup-steps').replaceChildren(...steps.map(text=>{const item=document.createElement('li');item.textContent=text;return item;}));
 $('setup-guide').hidden=steps.length===0;
 const p=state.observed_primary?.price?state.observed_primary:state.primary;
 const c=state.observed_check?.price?state.observed_check:state.check;
 const price=(q)=>q?.price>0?q.price.toLocaleString('en-US',{maximumFractionDigits:8})+' '+data.config.currency.toUpperCase():'—';
 $('quote-values').hidden=!(p?.price>0||c?.price>0);
 $('quotes-note').textContent=!active?t('以下仅是双源供应商的核验记录，不代表当前收款汇率。当前实际汇率请查看支付设置。','These are provider verification records, not the active payment rate. See Payment settings for the effective rate.'):!(p?.price>0||c?.price>0)?t('暂时没有供应商报价。完成配置后重新核验。','No provider quotes yet. Complete the configuration and run a check.'):t('价格单位为每 1 USDT 对应的计价币金额。异常报价不代表订单已采用此价格。','Prices show billing currency per 1 USDT. An observed anomalous quote does not mean an order used that price.');
 $('primary').textContent=price(p);$('check').textContent=price(c);
 $('primary-time').textContent=time(p?.quoted_at);$('check-time').textContent=time(c?.quoted_at);
 $('spread').textContent=p?.price>0&&c?.price>0?Number(state.spread_percent).toFixed(3)+'%':'—';
 $('expiry').textContent=t('可信报价有效至：','Trusted quote expires: ')+time(state.expires_at);
 $('trusted').textContent=t('最近接受的主报价：','Last accepted primary price: ')+price(state.primary);
 $('reason').textContent=data.error||state.last_error||(!data.usable&&active?t('暂无有效的双源报价。请检查 Key、参考价和范围，然后重新核验。','No valid verified quote. Check keys, reference and bounds, then run a check.'):'');
 $('refresh').disabled=!active;
}
form.onsubmit=async(event)=>{
 event.preventDefault();const button=form.querySelector('[type=submit]');button.disabled=true;
 try{
 const payload={};for(const el of form.elements){if(!el.name)continue;payload[el.name]=el.type==='checkbox'?el.checked:el.type==='number'?Number(el.value):el.value.trim();}
 if(payload.fallback_expires_at)payload.fallback_expires_at=new Date(payload.fallback_expires_at).toISOString();
 if(payload.fallback_enabled&&(!(payload.fallback_price>0)||payload.fallback_price<payload.min_price||payload.fallback_price>payload.max_price))throw Error(t('固定回退价必须在设置的价格范围内。','Fixed fallback must be within your price limits.'));
 if(payload.enabled&&((!payload.cmc_api_key&&!latest?.cmc_key_configured)||(!payload.coingecko_api_key&&!latest?.coingecko_key_configured)))throw Error(t('请先配置两家供应商的 API Key。','Configure both provider API keys first.'));
 if(payload.enabled&&(!(payload.min_price>0)||payload.max_price<=payload.min_price||payload.reference_price<payload.min_price||payload.reference_price>payload.max_price||payload.max_age_seconds<payload.refresh_seconds||payload.max_time_skew_seconds>payload.max_age_seconds))throw Error(t('请检查价格范围、参考价与有效时间。','Check price bounds, reference and validity periods.'));
 render(await api('/settings/rate/verified','PUT',payload));message(t('已保存，请查看上方正在生效的设置。','Saved. Review the effective settings above.'));
 }catch(e){message(e.message,true);}finally{button.disabled=false;}
};
$('refresh').onclick=async()=>{const b=$('refresh');b.disabled=true;try{render(await api('/settings/rate/verified/refresh','POST',{}),false);message(t('核验完成，请查看报价状态。','Check completed. Review the quote status.'));}catch(e){message(e.message,true);}finally{b.disabled=!(latest?.mode==='auto'&&latest?.config.enabled);}};
async function loadChannels(){
 try{
 const rows=await api('/notification-channels');const parent=$('channels');parent.replaceChildren();
 if(!rows?.length){parent.textContent=t('暂无通知渠道，请先在通知设置中添加。','No channels. Add one in notification settings first.');return;}
 for(const row of rows){
 const box=document.createElement('label');box.className='channel';const name=document.createElement('span');name.textContent=row.name||row.type;const input=document.createElement('input');input.type='checkbox';let events;try{events=typeof row.events==='string'?JSON.parse(row.events):row.events||{};}catch{events={};}input.checked=!!events.rate_alert;
 input.onchange=async()=>{const selected=input.checked;input.disabled=true;try{await api('/notification-channels/'+row.id,'PATCH',{events:{...events,rate_alert:selected}});events.rate_alert=selected;message(t('通知订阅已更新。','Alert subscription updated.'));}catch(e){input.checked=!selected;message(e.message,true);}finally{input.disabled=false;}};
 box.append(name,input);parent.append(box);
 }
 }catch{ $('channels').textContent=t('通知渠道暂时无法读取。','Notification channels could not be loaded.'); }
}
applyLanguage();
try{render(await api('/settings/rate/verified'));await loadChannels();}catch(e){message(e.message,true);form.querySelector('[type=submit]').disabled=true;}
