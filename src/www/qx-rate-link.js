// Add a maintainable entry without rewriting the upstream's compiled bundles.
const install = () => {
 if (!location.pathname.startsWith('/settings')) return;
 for (const original of document.querySelectorAll('a[href="/settings/pay"]')) {
  if (original.parentElement?.querySelector('[data-qx-rate-link]')) continue;
  const link=document.createElement('a');
  link.href='/qx-rate-guard.html';link.dataset.qxRateLink='true';
  link.className=original.className;
  link.textContent=document.documentElement.lang.startsWith('zh')?'双源汇率':'Verified USDT rates';
  original.insertAdjacentElement('afterend',link);
 }
};
new MutationObserver(install).observe(document.getElementById('app'),{childList:true,subtree:true});
install();
