import { t as react } from './assets/react-CO2uhaBc.js';
import { n as router } from './assets/router-aZWjwYfd.js';
import { n as Outlet } from './assets/Match-5D-p-fyl.js';
import { component as UpstreamShell } from './assets/route-TItYb9Ps.js';
import { i as getNavigation, O as useSidebar } from './assets/search-provider-DPXFLA4q.js';
import { n as Header, t as Main } from './assets/main-DpDyQAvX.js';
import { t as PageHeader } from './assets/page-header-IBw7XS2q.js';
import { t as SidebarNav } from './assets/sidebar-nav-Cwns29Yf.js';
import { t as Separator } from './assets/separator-CuUwHLH5.js';
import { gi as settingsDescription, lp as accountDescription } from './assets/messages-Cl9iE86Z.js';
import { r as setSessionToken } from './assets/auth-store-uaDKRR4l.js';
import { installNavigation, entries } from './qx-rate-link.js';

const { createElement: h, Fragment, useEffect, useRef } = react();
const chinese = () => (document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1] || navigator.language).startsWith('zh');

function Navigation() {
  const { setOpenMobile } = useSidebar();
  useEffect(() => installNavigation(router, () => setOpenMobile(false)), [setOpenMobile]);
  return null;
}

export function AuthenticatedLayout() {
  return h(UpstreamShell, null, h(Fragment, null, h(Navigation), h(Outlet)));
}

// Use upstream components and its localized menu data for both desktop links
// and the mobile selector. Existing settings keep their original route content.
function SectionLayout({ section }) {
  const entry = entries.find(item => item.section === section);
  const group = getNavigation().navGroups.flatMap(item => item.items)
    .find(item => item.items?.some(child => child.url === entry.anchor));
  const items = group.items.map(item => ({
    title: item.title, href: item.url, icon: h(item.icon || group.icon, { size: 18 }),
  }));
  const position = items.findIndex(item => item.href === entry.anchor);
  items.splice(position + 1, 0, { title: chinese() ? entry.zh : entry.en, href: entry.href, icon: h(group.icon, { size: 18 }) });
  return h(Fragment, null,
    h(Header),
    h(Main, { fixed: true },
      h(PageHeader, { title: group.title, description: section === 'settings' ? settingsDescription() : accountDescription() }),
      h(Separator, { className: 'my-4 lg:my-6' }),
      h('div', { className: 'flex flex-1 flex-col space-y-2 overflow-hidden lg:flex-row lg:space-x-12 lg:space-y-0' },
        h('aside', { className: 'top-0 lg:sticky lg:w-1/5' }, h(SidebarNav, { items })),
        h('div', { className: 'flex min-h-0 w-full min-w-0 overflow-y-hidden p-1' }, h(Outlet)),
      ),
    ),
  );
}

export function SettingsLayout() { return h(SectionLayout, { section: 'settings' }); }
export function AccountLayout() { return h(SectionLayout, { section: 'account' }); }

async function resource(path, signal) {
  const response = await fetch(path, { signal, cache: 'no-cache' });
  if (!response.ok) throw new Error('Unable to load admin page');
  return response.text();
}

function AdminPage({ name }) {
  const container = useRef(null);
  useEffect(() => {
    const host = container.current;
    const root = host.shadowRoot || host.attachShadow({ mode: 'open' });
    const controller = new AbortController();
    const { signal } = controller;
    const oldTitle = document.title;
    const syncTheme = () => { host.dataset.qxMode = document.documentElement.dataset.mode; };
    syncTheme();
    const observer = new MutationObserver(syncTheme);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-mode'] });
    root.textContent = chinese() ? '正在加载…' : 'Loading…';
    const navigate = event => {
      const link = event.target.closest('a[href]');
      if (!link || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const url = new URL(link.href);
      if (url.origin !== location.origin || link.target || link.hasAttribute('download')) return;
      event.preventDefault();
      router.navigate({ to: url.pathname, hash: url.hash.slice(1) });
    };
    root.addEventListener('click', navigate, { signal });
    // Dynamic import is cached, but mount is called once per visit. No retained
    // form state, QR secrets, recovery codes or listeners across route changes.
    (async () => {
      const [html, styles, module] = await Promise.all([
        resource(`/${name}-content.html`, signal),
        Promise.all(['/qx-rate-guard.css', ...(name === 'qx-security' ? ['/qx-security.css'] : [])].map(path => resource(path, signal))),
        import(`./${name}.js`),
      ]);
      signal.throwIfAborted();
      const template = document.createElement('template');
      template.innerHTML = html;
      const style = document.createElement('style');
      style.textContent = styles.join('\n');
      root.replaceChildren(style, template.content.cloneNode(true));
      module.mount(root, { signal, onSessionToken: setSessionToken });
    })().catch(() => {
      if (!signal.aborted) root.textContent = chinese() ? '页面加载失败，请刷新重试。' : 'Unable to load this page. Please refresh to retry.';
    });
    return () => {
      controller.abort();
      observer.disconnect();
      root.replaceChildren();
      document.title = oldTitle;
    };
  }, [name]);
  return h('div', { className: 'min-h-0 w-full min-w-0 overflow-y-auto', 'data-qx-page': name, ref: container });
}

export function RatePage() { return h(AdminPage, { name: 'qx-rate-guard' }); }
export function SecurityPage() { return h(AdminPage, { name: 'qx-security' }); }
