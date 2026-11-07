// A narrow adapter for the upstream compiled login/password forms. Server-side
// authentication enforces MFA; this file only renders the extra input step.
(() => {
 const originalFetch=window.fetch.bind(window);
 const zh=()=>(document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1]||navigator.language).startsWith('zh');
 const text=(cn,en)=>zh()?cn:en;
 function showLimit(response){
  if(response.status!==429)return;
  const form=document.querySelector('input[name="username"],input[name="old_password"]')?.closest('form');
  if(!form)return;
  let notice=form.querySelector('[data-qx-login-limit]');
  if(!notice){notice=document.createElement('p');notice.dataset.qxLoginLimit='true';notice.setAttribute('role','alert');notice.style.color='#b44d44';form.append(notice);}
  const seconds=Number(response.headers.get('Retry-After'));
  notice.textContent=text('尝试次数过多，请稍后再试。','Too many attempts. Please try again later.')+(seconds>0?' '+text('约需等待 ','Wait about ')+Math.ceil(seconds/60)+text(' 分钟。',' minutes.'):'');
 }
 async function askCode(error,signal){
  if(signal?.aborted)return null;
  return new Promise(resolve=>{
   const dialog=document.createElement('dialog');dialog.className='qx-mfa-dialog';
   const form=document.createElement('form');form.method='dialog';
   const heading=document.createElement('h2');heading.id='qx-mfa-title';dialog.setAttribute('aria-labelledby',heading.id);heading.textContent=text('两步验证','Two-step verification');
   const note=document.createElement('p');note.textContent=error?text('验证码无效或已使用，请输入新的验证码或恢复码。','Invalid or already used code. Enter a new code or a recovery code.'):text('请输入验证器的 6 位验证码，或使用一次性恢复码。','Enter your 6-digit authenticator code or a one-time recovery code.');
   const input=document.createElement('input');input.name='code';input.autocomplete='one-time-code';input.required=true;input.maxLength=64;input.setAttribute('aria-label',text('验证码或恢复码','Verification or recovery code'));
   const actions=document.createElement('div');actions.className='qx-mfa-actions';
   const cancel=document.createElement('button');cancel.type='button';cancel.textContent=text('取消','Cancel');
   const submit=document.createElement('button');submit.type='submit';submit.textContent=text('验证','Verify');
   actions.append(cancel,submit);form.append(heading,note,input,actions);dialog.append(form);document.body.append(dialog);
   let done=false;
   const finish=value=>{if(done)return;done=true;signal?.removeEventListener('abort',abort);dialog.close();dialog.remove();resolve(value);};
   const abort=()=>finish(null);
   cancel.onclick=()=>finish(null);dialog.addEventListener('cancel',e=>{e.preventDefault();finish(null);});
   form.onsubmit=e=>{e.preventDefault();if(input.value.trim())finish(input.value.trim());};
   signal?.addEventListener('abort',abort,{once:true});dialog.showModal();input.focus();
  });
 }
 window.fetch=async(input,init)=>{
  const request=new Request(input,init);const url=new URL(request.url);
  const paths=['/admin/api/v1/auth/login','/admin/api/v1/auth/password'];
  if(url.origin!==location.origin||request.method!=='POST'||!paths.includes(url.pathname))return originalFetch(request);
  const saved=request.clone();let response=await originalFetch(request);
  showLimit(response);
  let result;try{result=await response.clone().json();}catch{return response;}
  if(result.data?.mfa_required){
   let body;try{body=await saved.json();}catch{return response;}
   for(let attempt=0;attempt<5;attempt++){
    const code=await askCode(attempt>0,saved.signal);
    if(!code)return new Response(JSON.stringify({status_code:400,message:'Verification cancelled'}),{status:400,headers:{'Content-Type':'application/json'}});
    response=await originalFetch(new Request(saved.url,{method:'POST',headers:saved.headers,credentials:saved.credentials,signal:saved.signal,body:JSON.stringify({...body,code})}));
    showLimit(response);
    if(response.status!==400)break;
   }
  }
  if(url.pathname.endsWith('/password')&&response.ok){
   const data=await response.clone().json();
   if(!data.data?.mfa_required){document.cookie='access_token=; Path=/; Max-Age=0';localStorage.removeItem('auth_user');setTimeout(()=>location.replace('/sign-in'),150);}
  }
  return response;
 };
})();
