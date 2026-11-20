// Register QX routes before the upstream RouterProvider mounts. Keep all pinned
// upstream imports here/in qx-admin-pages.js; see docs/admin-navigation.md.
import { n as router } from './assets/router-aZWjwYfd.js';
import { n as createRoute, t as createRootRouteWithContext } from './assets/route-CC6ptPdG.js';
import { AuthenticatedLayout, SettingsLayout, AccountLayout, RatePage, SecurityPage } from './qx-admin-pages.js';

const authenticated = router.routesById['/_authenticated'];
const settings = router.routesById['/_authenticated/settings'];
const account = router.routesById['/_authenticated/account'];
authenticated.update({ component: AuthenticatedLayout });
settings.update({ component: SettingsLayout });
account.update({ component: AccountLayout });
settings.addChildren([...settings.children, createRoute({
  getParentRoute: () => settings, path: 'verified-rates', component: RatePage,
})]);
account.addChildren([...account.children, createRoute({
  getParentRoute: () => account, path: 'security', component: SecurityPage,
})]);

// Router.update rebuilds route lookup tables only for a new routeTree identity.
// Reuse every existing child and guard, and reparent them through the public API.
const oldRoot = router.routeTree;
const root = createRootRouteWithContext()(oldRoot.options);
root.addChildren(oldRoot.children.map(route => route.update({ getParentRoute: () => root })));
router.update({ routeTree: root });
await import('./assets/index-DMvm0YrM.js');
