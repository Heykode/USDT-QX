// Old bookmarks still work, but all settings now live in the authenticated SPA.
const paths = {
  '/qx-rate-guard.html': '/settings/verified-rates',
  '/qx-security.html': '/account/security',
};
const target = paths[location.pathname];
if (target) location.replace(target + location.search + location.hash);
