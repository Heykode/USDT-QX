// Upstream only ships compiled navigation. Extend its sidebar and account
// menus, while routing through the same router and mobile sidebar context.
export const entries = [
  { section: 'settings', anchor: '/settings/pay', href: '/settings/verified-rates', key: 'rates', zh: '双源汇率', en: 'Verified rates' },
  { section: 'account', anchor: '/account/password', href: '/account/security', key: 'security', zh: '登录安全', en: 'Login security' },
];

export function installNavigation(router, closeMobile) {
  const controller = new AbortController();
  const augmentedMenus = new WeakSet();
  let frame;
  function install() {
    const chinese = (document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1] || navigator.language).startsWith('zh');
    for (const entry of entries) {
      // Section navigation uses native React links; only extend the global
      // sidebar and portalled account/collapsed-sidebar menus here.
      for (const original of document.querySelectorAll(`a[href="${entry.anchor}"]`)) {
        if (!original.hasAttribute('data-sidebar') && original.getAttribute('role') !== 'menuitem') continue;
        const sibling = original.parentElement?.tagName === 'LI' ? original.parentElement : original;
        const parent = sibling.parentElement;
        if (!parent) continue;
        let item = parent.querySelector(`:scope > [data-qx-entry="${entry.key}"]`);
        if (!item) {
          const link = document.createElement('a');
          link.href = entry.href;
          link.className = original.className.replace(/\bbg-muted\b|\bbg-sidebar-accent\b|\bbg-secondary\b/g, '');
          for (const name of ['data-sidebar', 'data-slot', 'data-size', 'role']) {
            if (original.hasAttribute(name)) link.setAttribute(name, original.getAttribute(name));
          }
          if (link.getAttribute('role') === 'menuitem') link.tabIndex = -1;
          const icon = original.querySelector('svg');
          if (icon) link.append(icon.cloneNode(true));
          const label = document.createElement('span');
          label.dataset.qxLabel = '';
          link.append(label);
          link.addEventListener('click', event => {
            if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
            event.preventDefault();
            if (link.closest('[role="menu"]')) {
              // The added item is outside Radix's compiled item collection;
              // dismiss that open menu through its standard Escape handler.
              link.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
            }
            closeMobile();
            router.navigate({ to: entry.href });
          }, { signal: controller.signal });
          item = sibling === original ? link : document.createElement('li');
          if (item !== link) {
            item.className = sibling.className;
            item.append(link);
          }
          item.dataset.qxEntry = entry.key;
          sibling.insertAdjacentElement('afterend', item);
        }
        const menu = item.closest('[role="menu"]');
        if (menu && !augmentedMenus.has(menu)) {
          augmentedMenus.add(menu);
          // Include added entries in keyboard navigation; Radix's compiled
          // collection only knows about its original React menu items.
          menu.addEventListener('keydown', event => {
            if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key) || event.target.closest('[role="menu"]') !== menu) return;
            const items = [...menu.querySelectorAll('[role="menuitem"]')].filter(el => el.closest('[role="menu"]') === menu && !el.hasAttribute('data-disabled') && el.getAttribute('aria-disabled') !== 'true' && el.getClientRects().length);
            if (!items.length) return;
            const current = items.indexOf(document.activeElement);
            const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (current + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
            event.preventDefault();
            event.stopPropagation();
            items[next].focus();
          }, { capture: true, signal: controller.signal });
        }
        const link = item.matches('a') ? item : item.querySelector('a');
        const active = router.state.location.pathname === entry.href;
        link.dataset.active = String(active);
        if (active) link.setAttribute('aria-current', 'page');
        else link.removeAttribute('aria-current');
        link.classList.toggle('bg-sidebar-accent', active);
        const label = item.querySelector('[data-qx-label]');
        const text = chinese ? entry.zh : entry.en;
        if (label && label.textContent !== text) label.textContent = text;
      }
    }
  }
  const schedule = () => {
    if (frame) return;
    frame = requestAnimationFrame(() => { frame = undefined; install(); });
  };
  const observer = new MutationObserver(schedule);
  observer.observe(document.body, { childList: true, subtree: true });
  const unsubscribe = router.subscribe('onResolved', schedule);
  install();
  return () => {
    controller.abort();
    observer.disconnect();
    unsubscribe();
    cancelAnimationFrame(frame);
    document.querySelectorAll('[data-qx-entry]').forEach(item => item.remove());
  };
}
