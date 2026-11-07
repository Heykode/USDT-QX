# Fork 优化逐项复审

日期：2026-11-07（北京时间）。QX 基线 `3c92cc3fce4fa8a5ab1e600ba753ff89f7689d5b`。本轮仅修改 USDT-QX 源码，不部署、不改真实凭据、不接触真实付款。

## 范围与证据

重新请求 GitHub 官方 API 核实以下 16 个候选分支的当前 HEAD；它们与 11 月 5 日保留的 compare 数据一致。沿用这些准确 SHA 的提交差异，结合当前 QX 调用链逐项评审主要改动。不是对全部公开 Fork 或每个文件作完整安全审计。

先判断改动是否已在 QX、是否适用于单实例和现有 New API 接口，再用复现测试确认收益。采用的是经过适配的小补丁，没有整条分支 cherry-pick，也没有执行 Fork 安装器。

| Fork / 分支 | 当前提交 | 结论 | 审查结果 |
|---|---|---|---|
| `TavroxAI/epusdt` · `master` | [e6df17c3ffbf](https://github.com/TavroxAI/epusdt/commit/e6df17c3ffbf0b77ccce20e2bd45ee57bc2bb4e2) | 部分吸收 | 并发校验、回跳 URL、Telegram 按钮鉴权与 Token 错误脱敏本轮采用；登录限流/会话撤销此前已适配。Ed25519、原生币、确认队列、数据库跳过迁移及通用网络出口策略不整包合入。 |
| `xianyvbang/epusdt` · `master` | [7f069ae5d190](https://github.com/xianyvbang/epusdt/commit/7f069ae5d190da714cd4808086faa158b9d25b1d) | 已吸收有用部分 | 易支付查单和父子单统计去重已在 QX。其余主要是 OKX Chromium 抓取、默认节点和发布配置；会增加浏览器运行依赖及维护面，本轮不采用。 |
| `ralph-wren/epusdt` · `codex/epusdt-order-date-cache` | [1d6e98206a75](https://github.com/ralph-wren/epusdt/commit/1d6e98206a7519b8095330ffcea5b191deaede94) | 保留参考 | 报价快照/有效期/逐单回退已有 QX 实现，不替换成币安专用实现。其活跃订单查询只包含待支付且未超时订单，按此停扫需要额外验证延迟付款；MySQL 迁移、交易唯一占用、回调出口需单独审查。 |
| `quqi1599/epusdt` · `codex/epusdt-sqlite-rpc-health-fix` | [7e02de3b935c](https://github.com/quqi1599/epusdt/commit/7e02de3b935cf35d876e10d8a595568b8bf418c6) | 已有修复＋局部补强 | SQLite/RPC 能力、latest 订阅已采用。本轮借鉴区间限制思路并实现自动缩小，不把所有 BSC 节点固定降为 10 块，不叠加整套按订单窗口跳跃调度。 |
| `AkinoKaede/epusdt` · `fix/payment-reservation-lifecycle` | [4f9cd7b3b6e1](https://github.com/AkinoKaede/epusdt/commit/4f9cd7b3b6e1baf130bbd2c7a1d630d4774f9a59) | 已吸收 | 金额锁订单归属和锁期限同步已在 QX，保留并发/重复交易/回滚测试，不重复应用。 |
| `mybot102/epusdt` · `master` | [53319719858d](https://github.com/mybot102/epusdt/commit/53319719858d1b57f70dd87b2a28fdc17909ba14) | 暂缓 | Base/Arbitrum、新旧签名兼容属于功能扩展。付款收尾重试有价值，但 Fork 的该段仍按地址/金额释放锁，不能覆盖 QX 的订单归属锁；父子单故障恢复需单独做状态机验证。 |
| `HRET-Dev/epusdt` · `master` | [cfc369aef518](https://github.com/HRET-Dev/epusdt/commit/cfc369aef518d03165a65e763250e6ad2dc6ea53) | 本轮不采用 | 币安 C2C 端点解析不是当前双源保护的替代品；QX 已统一使用 binance 作为 BSC 网络键，不重复改名。 |
| `SuhaifengEric/epusdt` · `fix/REQ-011-api-key-secret-aead` | [a809e2f4e7d3](https://github.com/SuhaifengEric/epusdt/commit/a809e2f4e7d34888a183762e4f9f344d3e2292e1) | 部分吸收 | 采用 EVM 范围缩小思路，并让健康检查共用同一策略，排除限流/配额类失败。其 API Key 加密会要求新主密钥并拒绝未迁移的旧明文，直接替换可能导致服务无法启动或签名失败，本轮不采用。 |
| `qxyuan853/epusdt` · `hunter/fix-unique-tx-claim` | [1d47f59d4fbf](https://github.com/qxyuan853/epusdt/commit/1d47f59d4fbf8f4abc7024c405271c096a0f2ed7) | 暂缓 | 数据库唯一占用用于多实例交易防重。当前优先单实例，现有进程锁和订单查重继续保留；新增账本需要历史兼容、父子单、回滚及多数据库并发测试，不能只加一张表就宣称完整支持多实例。 |
| `kidrauhl123/epusdt` · `feat/idle-scan-gate` | [877d938ff3d4](https://github.com/kidrauhl123/epusdt/commit/877d938ff3d497edf458a5d2e8130ba6b3be02f6) | 本轮不采用 | 空闲停扫、恢复时跳过区块、30 分钟过期宽限及重复建单恢复会改变现有付款窗口/重复建单语义；没有实际 RPC 用量瓶颈证据，不为节流修改识别范围。 |
| `jw-star/epusdt` · `fix/shuffle-wallet-assignment` | [f6548e886d79](https://github.com/jw-star/epusdt/commit/f6548e886d791be05adfd511300ea56f2cfc76ac) | 本轮不采用 | 随机钱包分配会改变地址选择顺序、增加余额分散和归集成本；目前没有多地址负载均衡需求。其测试还包含随机概率断言，不直接带入。 |
| `self4m/epusdt` · `master` | [0299f1ebe4cf](https://github.com/self4m/epusdt/commit/0299f1ebe4cfac3bb57bb37098b1b6e584a97312) | 暂缓 | TRON 停用链/无钱包时暂停有价值，但 Fork 退出后重新 NewScanner/Init，从最新块开始，未保存退出前游标；暂停再启用的补扫边界需补齐，不能仅为省 RPC 直接移植。 |
| `pood1e/epusdt` · `fix/checkout-net-received` | [18c001425b55](https://github.com/pood1e/epusdt/commit/18c001425b551efa2c6afb749aa6155248230dff) | 文案参考 | 解释交易所提现手续费后实际到账数量的提醒合理，但此分支直接修改压缩收银台资源。New API 钱包已有精确付款提醒，本轮不替换现有 UI；后续统一后台/收银台源码时可纳入。 |
| `Yufeifeio/epusdt` · `fix/checkout-exact-amount-notice` | [7d5f43a26294](https://github.com/Yufeifeio/epusdt/commit/7d5f43a262941b40666fb44e10799894aa03b9c9) | 已覆盖主要目的 | 足额按显示金额付款的提醒已在 New API 弹窗；其他旧分支涉及静态路径/回调/补扫的内容部分已在官方 v2，不能整分支重放。 |
| `yakun0622/epusdt` · `master` | [5e6d7dfed21b](https://github.com/yakun0622/epusdt/commit/5e6d7dfed21be66e9e56c8239f4bb2e4a129fbc2) | 本轮不采用 | 前端恢复工程仍是脚手架和接口类型切片，作者文档明确尚未运行依赖安装/测试/构建；不能替换现有完整后台。 |
| `googidaddy/epusdt` · `secure-profile-20261031` | [ed533dc4c1a2](https://github.com/googidaddy/epusdt/commit/ed533dc4c1a20ec5f581e95f49c3686de668fbfa) | 架构不适用 | secure profile 移除管理后台、易支付、公开收银、换链及旧回调 worker；与 New API 的现有收款对接目标不同，不能作为维护补丁。 |

## 本轮采用的五项改进

### 1. 请求校验不再修改全局语言状态

来源：TavroxAI `e6df17c3ffbf0b77ccce20e2bd45ee57bc2bb4e2` 的 controller/base.go。共享 Controller 原来写入每个请求的 Locale，并调用 gookit/validate 的 RegisterGlobal 修改全局消息表。新增 16 个并发请求、不同语言及有效/无效参数的行为测试，旧代码在 race 下实际失败。

QX 改为每次请求创建 Validation 并注册本次语言，去掉仓库内没有调用者的共享字段/旧注册方法，不引入全局串行锁。保留原有参数错误码和语言回退。

### 2. 收银台返回地址限制为 HTTP(S)

来源：同一 TavroxAI 提交的 redirect_url.go、order_service.go、epay_return.go。新订单入口拒绝危险协议；读取历史订单和易支付返回跳转时再次过滤，避免旧数据绕过入口检查。QX 额外检查 Hostname，拒绝 `https://:443` 这种没有主机的地址。

兼容边界：空返回地址及绝对 HTTP/HTTPS 返回地址继续可用，原查询参数和片段保留；相对地址、协议相对地址和非 HTTP(S) 地址现在拒绝。正常 New API 的绝对返回地址不需改变。这里只控制浏览器跳转，未把商户返回域名限制为某家站点，也不改支付签名。

### 3. Telegram 动态钱包按钮统一鉴权

来源：TavroxAI 统一保护菜单回调的思路。旧代码只保护 `/start` 和文字输入，动态注册的钱包启用/禁用/删除按钮没有继承白名单；实测非管理员点击可改变钱包状态。

QX 把白名单应用于 bot 根中间件，自动覆盖后来注册的按钮，并捕获管理员 ID，拒绝空发送者。与 Fork 不同，保留现有按发送者个人 ID 授权的语义，不能把“来自指定群聊”解释成“群内所有成员均可管理钱包”。负数群聊 ID 仍可用于通知渠道，不因此获得管理权限。测试走真实 telebot Update 分发和数据库状态变更，验证外人不能禁用/删除，管理员仍可启用/禁用。

### 4. Telegram 错误信息隐藏 Bot Token

来源：TavroxAI 的通知错误脱敏。QX 复用一个错误处理函数覆盖通知发送、命令机器人初始化/命令设置及 OnError；同时处理原始和 URL 编码后的 Token，保留连接失败原因。用真实本地 HTTP 连接失败验证返回错误中不含测试 Token。没有改通知目的地或消息模板。

### 5. EVM RPC 范围限制自动适配

来源：SuhaifengEric `698bec20b77f` 的范围缩小思路，以及 quqi1599 对 BSC 小区间节点的适配。保留当前 BSC 默认 50 块、其他 EVM 默认 1000 块；仅在节点明确返回范围/结果大小限制时，维持起点并缩小终点。成功后只持久化实际查完的区间，后续从紧邻下一块继续。

QX 的额外约束：

- 健康探测共用同一策略，避免扫链能适配而健康检查仍把节点标记为不可用。
- 限流、配额、鉴权、方法不可用、普通参数错误以及含糊的 `limit exceeded` 不触发缩小重试。
- 最小区间 1 块，重试次数随区间减半自然有界；沿用请求总超时和取消。
- 成功缩小仅在当前扫描循环记忆，不改节点设置；重新连接重新评估。
- 查询失败不推进游标；健康探测不推进游标；不添加按订单窗口跳块逻辑。
- 范围受限时需要额外少量探测请求；不宣称所有节点都能接受或所有 RPC 错误都可恢复。

## 暂缓部分中仍值得后续专项研究的内容

1. **回调出站网络保护**：当前创建时有 URL 检查，但连接时的 DNS 重绑定、重定向及代理路径需要一起处理。TavroxAI 的实现保留环境代理和测试放行开关，ralph-wren 的实现禁用代理/重定向，不能直接认定与现有部署无兼容代价。本轮没有把创建时校验描述成完整 SSRF 防护。
2. **父子单付款收尾恢复**：mybot102 重试收尾的方向有价值；直接带入其旧式解锁会退回已经修复的金额锁行为。需要结合 QX 故障注入和重启恢复另做修改。
3. **API Key 静态加密、跨实例交易账本**：涉及历史数据迁移、恢复和密钥丢失处理，和本轮不迁移生产配置的小补丁性质不同。
4. **空闲扫链调度、TRON 暂停恢复、WS 心跳**：必须验证长时间离线、同秒多个块、付款早于订单过期但扫描晚到等场景。TavroxAI 的 WS 改动还和确认深度队列耦合，不直接复制。

这些条目没有被判定为“无价值”，而是不能凭当前证据作为无副作用补丁合入。

## 验证与实际边界

- 新增测试在原版代码上实际复现：并发校验 race、危险返回地址被接受、非管理员按钮改变钱包、区间受限 RPC 停扫且被判定 down。
- 修复后针对性测试通过：有效/无效参数和语言并发；危险/合法/历史返回地址；Telegram 实际 Update 权限；本地连接失败 Token 脱敏。
- EVM 使用本地 JSON-RPC 和临时 SQLite，包含首末区间两笔模拟 USDT 付款成功、连续覆盖、游标持久化后恢复、10 块限制健康检查、限流不重试、1 块终止、取消和失败不推进游标。
- 本地全量验证已通过：`CGO_ENABLED=0 go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`，以及 `CGO_ENABLED=0` 的 Linux amd64 / arm64 编译。
- 合并前还需通过本次 PR 对应提交的 GitHub Actions `QX validation`（Linux 全量测试、竞态检查、静态检查及双架构编译）；具体运行结果保留在 PR 检查记录中。
- 本轮没有真实链上转账、线上服务升级或多实例/多数据库生产验收。测试通过证明已覆盖场景，不能承诺绝对没有 bug。
