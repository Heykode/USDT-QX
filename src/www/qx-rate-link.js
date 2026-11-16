// Keep QX entries beside the existing links without changing compiled routes.
const entries = [
  { anchor: '/settings/pay', href: '/qx-rate-guard.html', key: 'rates', zh: '双源汇率', en: 'Verified rates' },
  { anchor: '/account/password', href: '/qx-security.html', key: 'security', zh: '登录安全', en: 'Login security' },
];

function install() {
  const chinese = (document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1] || navigator.language).startsWith('zh');
  for (const entry of entries) {
    for (const original of document.querySelectorAll(`a[href="${entry.anchor}"]`)) {
      const sibling = original.parentElement?.tagName === 'LI' ? original.parentElement : original;
      const parent = sibling.parentElement;
      if (!parent) continue;
      let item = parent.querySelector(`:scope > [data-qx-entry="${entry.key}"]`);
      if (!item) {
        const link = document.createElement('a');
        link.href = entry.href;
        link.className = original.className.replace(/\bbg-muted\b|\bbg-sidebar-accent\b/g, '');
        for (const name of ['data-sidebar', 'data-slot', 'data-size']) {
          if (original.hasAttribute(name)) link.setAttribute(name, original.getAttribute(name));
        }
        link.dataset.active = 'false';
        const icon = original.querySelector('svg');
        if (icon) link.append(icon.cloneNode(true));
        const label = document.createElement('span');
        label.dataset.qxLabel = '';
        link.append(label);
        item = sibling === original ? link : document.createElement('li');
        if (item !== link) {
          item.className = sibling.className;
          item.append(link);
        }
        item.dataset.qxEntry = entry.key;
        sibling.insertAdjacentElement('afterend', item);
      }
      const label = item.querySelector('[data-qx-label]');
      const text = chinese ? entry.zh : entry.en;
      if (label && label.textContent !== text) label.textContent = text;
    }
  }
}

// Menus may be rendered in a portal outside #app. Coalesce updates and avoid
// changing unchanged text, so our own mutations cannot create an observer loop.
let scheduled = false;
new MutationObserver(() => {
  if (scheduled) return;
  scheduled = true;
  requestAnimationFrame(() => {
    scheduled = false;
    install();
  });
}).observe(document.body, { childList: true, subtree: true });
install();
