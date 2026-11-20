# 后台内部导航

双源汇率和登录安全属于后台设置，现在使用与原菜单相同的 TanStack Router，在保留侧栏的情况下切换内容。刷新、直接访问和浏览器前后退也使用同一组路由。

| 功能 | 当前路径 | 兼容的旧地址 |
|---|---|---|
| 双源汇率 | `/settings/verified-rates` | `/qx-rate-guard.html` |
| 登录安全 | `/account/security` | `/qx-security.html` |

系统配置和账户安全继续使用上游的 Header、Main、SidebarNav 和菜单数据。双源汇率、登录安全同时加入桌面侧栏、收起侧栏的菜单、账户菜单，以及手机的页面选择器；在手机侧栏点击后关闭抽屉。修改密码等内部链接经过同一个路由器。外部文档链接、登录失效、退出和改密后的重新登录仍保持其原有行为。

## 编译前端适配

上游仓库没有提供完整前端工程。本实现不重写压缩 bundle，也不使用 iframe：

- `qx-admin-bootstrap.js` 在加载原应用入口之前，向已有鉴权父路由添加子路由。通过 `createRootRouteWithContext` 和 `router.update` 重建路由表，保留原有子路由与鉴权。
- `qx-admin-pages.js` 复用上游组件、语言菜单数据和会话存储；设置表单放入 Shadow DOM，隔离样式，继承实际后台主题。每次进入重新挂载，离开时中止请求、清理监听器和敏感内容。
- `qx-rate-link.js` 只为上游编译侧栏与 portal 菜单补入口，导航调用已有 router；页面内导航及手机选择器使用上游 React 组件。
- `qx-rate-guard-content.html` 和 `qx-security-content.html` 是唯一的表单模板；对应旧 HTML 仅做地址兼容。
- `qx-auth-bridge.css` 仅供原登录和改密验证码弹窗使用，其他表单样式不再加载到全局。

适配依赖当前已提交的 `assets/*` 导出。上游前端升级后，要重新核对 `qx-admin-bootstrap.js`、`qx-admin-pages.js` 的导入文件名、导出、路由 ID、菜单数据及 `index.html` 的入口。不能只替换 assets 后跳过浏览器验证。原上游的语言选择操作会刷新页面，继续保留这个行为。

本次没有修改支付建单、汇率计算、链查询或回调，也不修改已有服务器设置。

## 浏览器回归

`tests/admin-navigation.cjs` 在真正的已编译后台上运行，所有 API 都由本地模拟，阻止外网。需要本地已安装 Playwright 和 Chromium：

```sh
node tests/admin-navigation.cjs
```

若 Playwright 或浏览器不在默认路径，可设置 `PLAYWRIGHT_MODULE`（模块绝对路径）和 `CHROME_PATH`（浏览器绝对路径）。可通过 `QX_UI_OUTPUT` 指定截图及验证 JSON 的目录，默认写入系统临时目录。

测试验证：后台框架与页面切换、原支付配置与修改密码页面、菜单选中、保存请求及服务端错误、会话令牌同步、秘密信息清理、导航期间取消请求、前后退、刷新、旧书签、手机选择器和抽屉、长表单滚动、中英文、实际主题切换、匿名与过期登录。测试不使用真实凭据，不创建真实付款订单，不构成真实转账验收。
