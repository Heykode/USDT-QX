const $ = (id) => document.getElementById(id);
const form = $('policy');
let language=(document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1]||localStorage.getItem('qx-rate-language')||navigator.language).startsWith('zh')?'zh':'en';
const zh = Object.fromEntries([...document.querySelectorAll('[data-i18n]')].map(el => [el.dataset.i18n, el.textContent]));
const en = {
 fallbackTitle:'Per-order fixed fallback',fallbackEnable:'Use a fixed price when an automatic quote is unavailable',fallbackNote:'Each quote tries both providers first. A still-valid trusted quote can be reused; only an unusable automatic quote falls back. The next order tries again without changing the global mode. Enabling this increases provider requests.',fallbackPrice:'Fixed price: billing currency per 1 USDT',fallbackExpiry:'Fixed price valid until (local time, optional)',fallbackExpiryNote:'Leave blank for no expiry. A continuing outage may still cause consecutive orders to use the fixed price. Keep alerts enabled and review the price.',back:'← Back to rate settings',title:'Verified USDT rates',subtitle:'CoinMarketCap quotes, checked against CoinGecko. Automatic quotes first, with optional per-order fixed fallback.',current:'Current quotes',spread:'Provider difference',refresh:'Check providers now',policy:'Quote protection',enable:'Enable in automatic mode',scope:'Converts the selected billing currency to USDT on all USDT payment networks. Other automatic pairs will be unavailable when enabled. Fixed mode keeps its existing rules.',currency:'Order billing currency',plan:'CoinGecko plan',keys:'Keys are never displayed. Leave blank to keep a saved key. Use a plan licensed for your commercial use; selecting Demo does not establish commercial permission.',bounds:'Price limits · billing currency per 1 USDT',reference:'Manually verified reference',min:'Minimum acceptable price',max:'Maximum acceptable price',referenceNote:'The reference is set by the administrator and does not drift with provider data. Review and update it after a genuine large market move.',maxSpread:'Maximum provider difference (%)',maxJump:'Maximum reference / previous quote change (%)',interval:'Refresh interval (seconds)',maxAge:'Maximum source quote age (seconds)',skew:'Maximum provider time difference (seconds)',failureNote:'Temporary outages may use a still-valid verified quote. Conflicting or anomalous prices immediately pause quotes. When enabled, a fixed price may cover the current order. Thresholds are adjustable starting values, not guarantees.',save:'Save settings',saveNote:'Saving changed settings invalidates the old configuration’s quotes until rechecked.',alerts:'Rate alerts',alertsNote:'Subscribe existing notification channels to rate alerts. Continuing failures notify at most every 15 minutes. The notification channel must also be enabled.',footer:'Discounts apply before currency conversion. Existing orders keep their payable amount.'
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
 $('status').textContent=data.usable?t('自动报价可用','Automatic quote available'):data.fallback_available?t('自动价异常 · 可本单回退','Automatic unavailable · fallback available'):!active?t('未启用','Inactive'):t('报价暂停／待核验','Paused / awaiting check');
 $('status').dataset.state=data.usable?'ok':active?'stop':'';
 $('mode-note').textContent=data.mode==='fixed'?t('当前为固定汇率。配置保存后，请在原汇率设置中选择自动模式。','Fixed mode is active. After saving, select automatic mode in the existing rate settings.'):t('当前为自动汇率。双源开关关闭时使用原有单源逻辑。','Automatic mode is active. The original single-source logic applies when verification is disabled.');
 const p=state.observed_primary?.price?state.observed_primary:state.primary;
 const c=state.observed_check?.price?state.observed_check:state.check;
 const price=(q)=>q?.price>0?q.price.toLocaleString('en-US',{maximumFractionDigits:8})+' '+data.config.currency.toUpperCase():'—';
 $('primary').textContent=price(p);$('check').textContent=price(c);
 $('primary-time').textContent=time(p?.quoted_at);$('check-time').textContent=time(c?.quoted_at);
 $('spread').textContent=p?.price>0&&c?.price>0?Number(state.spread_percent).toFixed(3)+'%':'—';
 $('expiry').textContent=t('可信报价有效至：','Trusted quote expires: ')+time(state.expires_at);
 $('trusted').textContent=t('最近接受的主报价：','Last accepted primary price: ')+price(state.primary);
 $('reason').textContent=data.error||state.last_error||(!data.usable&&active?t('暂无有效的双源报价。请检查 Key、参考价和范围，然后重新核验。','No valid verified quote. Check keys, reference and bounds, then run a check.'):'');
 $('refresh').disabled=!data.config.enabled;
}
form.onsubmit=async(event)=>{
 event.preventDefault();const button=form.querySelector('[type=submit]');button.disabled=true;
 try{
 const payload={};for(const el of form.elements){if(!el.name)continue;payload[el.name]=el.type==='checkbox'?el.checked:el.type==='number'?Number(el.value):el.value.trim();}
 if(payload.fallback_expires_at)payload.fallback_expires_at=new Date(payload.fallback_expires_at).toISOString();
 if(payload.fallback_enabled&&(!(payload.fallback_price>0)||payload.fallback_price<payload.min_price||payload.fallback_price>payload.max_price))throw Error(t('固定回退价必须在设置的价格范围内。','Fixed fallback must be within your price limits.'));
 if(payload.enabled&&((!payload.cmc_api_key&&!latest?.cmc_key_configured)||(!payload.coingecko_api_key&&!latest?.coingecko_key_configured)))throw Error(t('请先配置两家供应商的 API Key。','Configure both provider API keys first.'));
 if(payload.enabled&&(!(payload.min_price>0)||payload.max_price<=payload.min_price||payload.reference_price<payload.min_price||payload.reference_price>payload.max_price||payload.max_age_seconds<payload.refresh_seconds||payload.max_time_skew_seconds>payload.max_age_seconds))throw Error(t('请检查价格范围、参考价与有效时间。','Check price bounds, reference and validity periods.'));
 render(await api('/settings/rate/verified','PUT',payload));message(t('已保存。新报价将按这组设置重新核验。','Saved. New quotes will be checked using this configuration.'));
 }catch(e){message(e.message,true);}finally{button.disabled=false;}
};
$('refresh').onclick=async()=>{const b=$('refresh');b.disabled=true;try{render(await api('/settings/rate/verified/refresh','POST',{}),false);message(t('核验完成，请查看报价状态。','Check completed. Review the quote status.'));}catch(e){message(e.message,true);}finally{b.disabled=!latest?.config.enabled;}};
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
