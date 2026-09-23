# Changelog

## v1.2.2 — 账号页按插件分组（2026-09-23）

**前端**
- Added 账号页默认按插件分组：一个插件一块，块头显示插件名与账号数，并提供「刷新本组」。
- Added 顶部插件筛选（全部 / 各插件，带账号数）：账号多时不必在一张长表里翻找。
- Changed 分组后移除冗余的「插件」列（块头已标明），窄屏仍保留插件徽标。

## v1.2.1 — 升级可靠性 · 模型目录可读性（2026-09-22）

**插件安装 / 升级**
- Fixed 安装阶段与 HTTP 连接解绑（`installCtx` = `context.WithoutCancel` + 5 分钟超时）。
  此前上传 26MB 包时反代在 ~60s 掐断连接 → `r.Context()` 取消 → 旧进程已停、二进制已换、
  新进程没起来，管理页显示「stopped + 旧版本号」，必须手动点启动才能恢复。
- Fixed 安装 / 升级成功后立即把实例 manifest 写回 `plugins` 表：插件处于停止状态时
  管理页不再显示升级前的旧版本（此前要等下次启动或重建容器才刷新）。

**前端**
- Fixed 账号详情「模型目录」徽标改为显示上游模型名（Auto / GLM-5.3 / Kimi-K3 / Qwen3.8-Max …），
  内部 key（dmodel / gmodel / kmodel_latest）作为次要文字保留——满屏 key 看不出是什么模型。

**网关 / 路由**
- Fixed `/v1/models` 补回账号目录模型：对外模型 = 路由名 ∪ 账号目录模型，删掉路由不再让模型从列表消失。
- Fixed 直连模型被误判 403：非路由请求改用 `HasRouteBinding`（key 是否显式绑定路由）判定。
  此前用 `AuthorizedModels` 判空——它对未绑定 key 返回「全部路由名」（非空），
  于是实例里只要存在一条路由，所有直连模型调用都会返回 `403 not in authorized routes`。
  受限 key（显式绑定路由）语义不变：只能使用被授权的路由名。

**插件清单**
- Changed `internal/admin/offline_market.json` 同步到 qoder 0.1.7 / qoderwork 0.1.8：
  修复上游嵌套 SSE 信封里 `statusCode` 是字符串 `"OK"` 时整帧被丢弃导致的「上游返回空内容」。

## v1.2.0 — 指纹注入 · 路由 UA · 安装进度 · 系统备份（2026-09-22）

**网关注入（按入口协议）**
- Added 客户端指纹注入：核心按入口协议生成 Claude Code（`messages`）与 Codex
  （`chat_completions` / `responses`）指纹头，序列化为 JSON 放进 `ChatRequest.extra` 的
  `fingerprint_headers`（SDK 常量 `ExtraFingerprintHeaders`）。只生成下发，是否采用由插件决定。
- Added 对话 UA 三级解析：`extra` 的 `client_user_agent` = 路由级 UA > 全局网关 UA > 客户端自带
  （SDK 常量 `ExtraClientUserAgent`）；路由弹窗与设置页均可配置。

**插件市场**
- Changed 市场安装改为 NDJSON 进度流：`downloading(received/total)` → `stopping` / `installing` /
  `starting` → `installed` / `error`；下载走 ctx（前端取消即中断），200ms 节流。
  前端逐行渲染，20MB+ 包不再只有一个转圈。

**运维**
- Added 系统信息 `GET /admin/system/info`：版本 / 契约 / Go 版本 / 数据目录 / 库体积 /
  迁移状态 / 各表计数 / 运行时长 / goroutine 数。
- Added 备份导出 `GET /admin/system/backup`：`VACUUM INTO` 一致性快照 + `secret.key` +
  `meta.json` 打包 zip（不中断服务）。
- Added 备份恢复 `POST /admin/system/restore`：校验后暂存 `<data>/restore`，重启时由
  `database.ApplyPendingRestore` 换入，旧库与旧密钥自动另存 `.bak-<时间戳>`，并清理 `-wal/-shm`。

**SDK**
- Fixed `sdk.Host` 的 nil 宿主防护：未注入 / 单测场景下 `Log` / `Settings` / `StoreGet` /
  `StorePut` 返回零值，而不是空指针 panic。

**其它**
- Fixed `internal/gateway/aggregate.go` 整体复制 `pb.Usage`（内含 protoimpl 锁）的 vet 报错。
- Changed 前端开启 `noUnusedLocals` / `noUnusedParameters`，清理 4 处未使用 import。
## v1.1.0 — 协议兼容 · 可观测 · 模型中心（2026-09-22）

**网关 / 协议**
- Added 按入口协议下发流式错误帧：Responses → `response.failed`、Anthropic → `error` 事件、
  OpenAI → error chunk（此前是三种协议都不认的裸帧，断流会被当成正常结束）。
- Added 请求参数补全：`max_completion_tokens` / `reasoning_effort` / `frequency_penalty` /
  `presence_penalty` / `seed` / `parallel_tool_calls` / `response_format` / `user` / `top_k` /
  `metadata.user_id`，显式 `temperature: 0` 与「未传」区分。
- Added 缓存写入 token 全链路：`cph.proto` → SDK 解析 → 三协议用量 → 日志 → 前端。
- Changed **对外模型 = 路由名 ∪ 账号目录模型**：账号里的模型默认可直接调用（凭据与出站代理照常注入），
  路由只承担改名 / 映射；`router.ResolveDirect` + `/v1/models` 并集，受限 key 仍只透出自有路由。

**可观测**
- Added 日志详情：客户端请求原文 + 完整上游返回（插件回传，8KB 上限）；大字段 `json:"-"` 不进列表，
  详情接口 `GET /admin/logs/{id}/detail` 按需拉取。
- Added 日志 CSV 导出（带 UTF-8 BOM，含缓存写入与积分列）。

**路由 / 任务**
- Added 策略 `sticky_expiring`：会话粘性 + 快过期积分优先（新会话先烧快到期额度，同会话不换号）。
- Changed 「同步上游模型」生成的默认策略为 `sticky_expiring`。
- Fixed 任务规则「指定账号」此前前端传了 `target_json` 但后端未接收，永远落成空数组；
  同时新增 `PUT /admin/task-rules/{id}`（编辑触发方式 / 账号范围）。

**管理端 UI**
- Added 账号新增向导（选客户端 → 授权 → 配置）、账号编辑弹窗、**账号详情 + 在线测试**（直调上游）。
- Added 路由新建 / 编辑 / 删除弹窗（策略 / 分组权重 / 真实模型 / 首字超时 / 失败降级）。
- Added 模型中心页：系列 / 积分倍率 / 上下文 / 最大输出 / 推理档位 + 搜索、系列与能力筛选、倍率排序。
- Added 分组重命名（`PUT /admin/groups/{id}`）。
- Changed 移动端适配：窄屏侧栏改抽屉、表格折叠次要列，390px 实测无横向滚动。
- Changed 授权链接只展示不自动打开（复制 / 打开授权页由用户决定）。

**插件**
- workbuddy `0.1.11`：直连腾讯模型接口（显示名 / 上下文 / 最大输出 / 推理档位 / 积分倍率），
  三路合并 `/v2`、`/console`、`/v3/config`，过滤非对话模型。
- lobsterai `0.1.11`：模型目录补倍率 / 思考档位 / 上下文 / 最大输出（容错别名映射）。
- 两者 HTTP 失败时回传完整上游返回（`TaskFailed.detail`），配合日志详情排查 400/500。

## 第 1 步重构：协议公共层 + 管理后台拆包 + 前端组件化（2026-09-21）

**后端（纯搬家，行为不变）**
- Added `internal/gateway/aggregate.go`：三种出口（OpenAI Chat / Responses / Anthropic Messages）的
  非流式聚合收敛为 `aggregateCore`（文本 + 工具调用按 index 归位 + 用量 + 结束原因 + 出现顺序），
  三份近乎相同的 `feed()` 合并为一；`aggrTool` 从 anthropic.go 移到公共处。
- Changed `internal/admin`：新增 `plugins.go`（插件列表与授权方式视图），账号 CRUD/登录 handler 归入
  `accounts.go`，`server.go` 只留装配与通用工具。
- 净减约 250 行重复代码；`go test ./...` 全绿（含三协议流式/非流式回归）。

**前端**
- Added 公共组件：`PageHeader` / `DataTable` / `StatusTag` / `TokenCell` / `Pager`，
  以及 `usePagedList` 组合式函数与 `utils/format.ts` 格式化工具。
- Changed `main.ts` 从 TDesign 全量注册改为**按需注册 43 个组件**（新增组件需在此补一行）。
- Changed Vite 增加 vendor 分包：入口 JS **1504 KB → 38.6 KB**，`vendor-tdesign` / `vendor-vue` /
  `vendor-misc` 可长期缓存，`vendor-echarts` 只在概览页加载。
- Changed 各页右上角操作区统一为 `.page-actions`（标题由 Layout 顶栏承担，不再各写一份标题区）。
- Changed `Accounts.vue` 抽出 `AccountDetailDrawer.vue`（账号详情 + 动态区块 + 任务历史渲染）。
- Changed `TokenCell` 用 SVG 图标 + 文字标签替代 `↓↑⚡` 符号（可访问性：不靠符号单独表意）。

**账号页拆分（同一批完成）**
- `Accounts.vue` 1228 → 975 行，抽出三个子组件：
  `AccountDetailDrawer`（详情 + 动态区块 + 任务历史）、`AccountEditDialog`（改名/分组/代理/模型）、
  `AccountTestDrawer`（在线测试）。父级只保留列表与新增向导。
- Fixed 在线测试的模型候选改为**该账号自己的**模型目录（旧实现复用编辑弹窗的残留状态，
  切换账号后会带出上一个账号的模型）。

**待办（下一步）**
- `Accounts.vue` 的「新增向导」仍是最大的一块（插件选择 → 授权 → 配置三步），可作为一个独立组件抽出。
- `Tasks.vue` 的运行状态标签可复用 `StatusTag`（当前仍是内联 t-tag，语义与 HTTP 状态不同）。

## 用量口径统一：缓存命中不再算两遍（2026-09-21）

背景：日志详情里出现过「输入 70638 / 输出 1948 / 缓存命中 64320 / 总 Token 136906」，
总 Token 把命中部分算了两遍，命中率分母也偏大。根因是各上游对 input_tokens 的语义不一致：
OpenAI 的 prompt_tokens **含**缓存命中（命中是其子集），Anthropic 的 input_tokens **不含**
（完整输入 = input_tokens + cache_read + cache_creation）。两种口径混在一起，加总就错。

- Changed 统一信封口径：`Usage.input_tokens` = 完整输入（含缓存命中），
  `Usage.cached_tokens` = 其中的命中子集，`Usage.output_tokens` = 输出；总 Token = 输入 + 输出。
- Fixed `sdk/anthropicup`：把 `input_tokens + cache_read + cache_creation` 换算成完整输入再上报
  （此前只报 input_tokens，导致 Anthropic 通道的输入偏小）。
- Fixed `sdk/openaiup`：缓存候选字段（顶层 cached_tokens / prompt_tokens_details /
  input_tokens_details 各家叫法）改为**取最大值**而不是相加 —— 它们是同一份命中的别名，
  相加会把命中量算成两倍；并夹到不超过输入总量。
- Fixed 网关 Anthropic 出口：回给 Claude 客户端时 `input_tokens` 扣掉命中部分
  （Anthropic 语义），`cache_read_input_tokens` 单独给，客户端展示的节省才对得上。
- Added 网关 OpenAI 出口补 `prompt_tokens_details.cached_tokens`、Responses 出口补
  `input_tokens_details.cached_tokens`，客户端能直接读缓存明细。
- Fixed 日志页：总 Token 改为 输入 + 输出，命中率分母改为输入总量并夹到 100%；
  详情抽屉的输入行标注「含缓存命中」，缓存行标注命中率。
- Note 口径修正需要插件用新 SDK 重编（已发 0.1.5）；**历史日志的 input_tokens 仍是旧口径**，
  只有新请求的数字是统一的。

## 缓存命中统计修复 + 日志账号/积分 + 批量运维 + UI 重构（2026-09-21）

**缓存命中一直是 0（核心根因在 SDK / 插件侧）**
- Fixed `sdk/openaiup`：此前只解析 `prompt_tokens` / `completion_tokens`，`cached_tokens`、
  `prompt_tokens_details.cached_tokens`、`input_tokens_details.cached_tokens`、
  `cache_read_input_tokens` 全部丢弃 —— 这就是日志里缓存命中恒为 0 的直接原因。
- Fixed `sdk/anthropicup`：`message_start` 里的 `input_tokens`、`cache_read_input_tokens`、
  `cache_creation_input_tokens` 之前只解析不使用，`message_finish` 只带 `output_tokens`；
  现在按字段取较大值合并上报（上游分片上报，非累加语义），并补两条回归测试。
- Added 网关 Anthropic 出口透出缓存字段，Claude 协议客户端能看到 `cache_read_input_tokens`。
- Note 该修复在 SDK 内，需要插件用新 SDK 重新编译后才生效（插件 0.1.4 已重新发布）。

**日志：账号 + 积分消耗**
- Added `Usage.credit_used` protobuf 字段（field 4，proto3 默认 0，旧插件天然兼容）与
  `request_logs.credit_used` 列（迁移 `000005`）。
- Added `account_credit_daily` 表：每次账号刷新按「当天基线 - 当前剩余」推算今日积分消耗，
  充值会把基线上移，不会算成负消耗。
- Added 日志列表「账号」列（点开详情仍有完整字段）与「积分」列（未上报显示 `-`），
  Token 单元格补充缓存命中率，工具条显示本页汇总（Σ Token / 命中率 / 积分）。

**批量运维入口**
- Added `POST /admin/accounts/refresh-all`：并发（默认 3）刷新所有账号，返回逐条失败明细。
- Added `POST /admin/routes/sync-models`：把各分组下账号的上游模型并集补齐成路由，
  **只新建缺失的同名路由**，已存在的路由（含人工策略/权重/降级）一律不动；
  `refresh=true` 时先拉一遍上游模型目录。
- Added `POST /admin/task-rules/run-all`：后台串行执行规则（签到类逐账号），立即返回已触发条数，
  结果写任务历史 —— 避免几十个账号把 HTTP 请求拖超时。
- Added `GET /admin/accounts` 返回 `today_tokens` / `today_cached` / `today_credits`（含
  `today_credits_estimated` 估算标记）/ `today_requests`。

**UI 重构**
- Fixed 侧栏展开时右侧内容被遮挡：内容区改双向滚动，表格统一包一层横向滚动容器，
  多个 `t-menu` 的 `height: 100%` 把后几组菜单挤出屏幕的问题也一并修掉。
- Changed 整套主题重做（`assets/theme.css`）：统一色板/圆角/描边/阴影，卡片、表格、菜单、
  表单控件对齐同一套设计变量；日志表明暗两套配色都重调了对比度。
- Changed 侧栏按「总览 / 资源 / 流量 / 运维」分组，品牌区加副标题，选中态改为左侧指示条；
  账号页「一键刷新」、路由页「同步上游模型」、任务页「全部执行」都进了页面头部操作区。
- Changed 日志表列合并：请求模型与上游模型合成一列（路由改写时显示 `别名 → 真实模型`），
  协议与流式合成一列（流式用绿点区分），窄窗口下横向滚动不再是「被裁掉」。
- Changed 顶栏 GitHub 与发布页地址抽到 `web/src/utils/repo.ts`，默认指向二开仓库
  `Sndeok/ClawProxyHub`（改回上游只改这一个文件）。

## 多模态 Responses / Chat / Anthropic 内容贯通（2026-09-21）

- Added `EnvelopeMessage.content_json` protobuf 字段（向后兼容，不升 `ProtocolVersion`）：
  统一承载规范化后的 OpenAI Chat 风格 content-part 数组；旧插件忽略未知字段，纯文本链路不变。
- Added Responses / OpenAI Chat / Anthropic Messages 的内容块归一化：
  `input_image` / `image`、`input_file` / `document`、`input_audio`、`input_video`、
  `image_url`、`file` 等内容保留，不再只拼成 `Text`。
- Added SDK 适配器多模态输出：`sdk/openaiup` 优先发送 content 数组；`sdk/anthropicup`
  把图片/文件 data URL 转成 Anthropic `image` / `document` block。老插件可继续接收纯文本，
  新版插件 0.1.2 已重新构建。
- Added 多模态回归测试：网关解析、protobuf round-trip、OpenAI Chat 适配、Anthropic 适配。
- Published `lobsterai v0.1.3` / `workbuddy v0.1.3`，插件仓库 CI 对着本 fork 的核心 SDK 重建发布。

## 移植上游 v1.0.2 / v1.0.3（B+C+D 批）

上游 6 个提交经逐条比对后**选择性移植**（未采用 git merge，原因见文末）。
v1.0.2 的工具调用与 Responses 收尾修复本仓库此前已用另一套实现独立修好，
此处仅补上当时漏掉的三处。

**Codex 兼容（v1.0.2 补齐）**
- Fixed 不再透传 `reasoning` / `reasoning.effort`：Responses 本无顶层 reasoning_effort，
  Codex 的 `xhigh` 等私有值上游不认会 500
- Fixed 合并相邻 assistant message 与 function_call 进同一条 assistant 的 tool_calls：
  并行工具调用时 tool 消息与声明它的 assistant 错位，上游拒绝整段历史
- Fixed function_call 的 `arguments` 为空时补 `{}`

**账号（v1.0.3）**
- Added 账号级出站代理（`account_proxies`，优先级 账号 > 分组），账号编辑弹窗可绑定
- Added 账号模型目录落库（`accounts.models_json`）：首次登录自动拉取，`?refresh=1` 手动同步，
  读取默认走库，以用户勾选为准
- Added 账号在线测试：选端点/模型/问题直调插件 Chat，绕过路由与密钥，不落 `request_logs`
- Added 账号编辑弹窗（改名 / 分组 / 代理 / 模型）

**出站代理与密钥（v1.0.3）**
- Added 代理编辑 `PUT /admin/proxies/{id}`（密码留空保留原值）与连通性测试
  `POST /admin/proxies/{id}/test`（经代理拨中立目标回时延，socks5 走 `x/net/proxy`）
- Added 密钥改名 `PUT /admin/keys/{id}`
- Added 前端通用可搜索绑定组件 `BindSelect`

**其它（v1.0.3）**
- Added 进程内事件总线（`internal/event`）：任务成功完成后自动刷新该账号积分
- Added 核心版本机制（`internal/version` + `version.json`）与检查更新 `GET /admin/version`，
  侧栏底部显示版本、有新版本时可跳发布页；远端清单与插件市场共用出站代理配置

**迁移**
- Added 迁移 `000004_account_proxy_models`：`account_proxies` 表 + `accounts.models_json`。
  上游把这份 schema 放在重写后的 000002；本仓库 000002(key_hash)/000003(调用日志) 已发布且
  生产库已执行到 version 3，若沿用 000002 该变更将永不执行，故按序追加为 000004。
- Added `internal/database/migrate_test.go`：迁移编号必须连续、up/down 成对，
  并模拟「上一版存量库」验证新迁移确实会执行、且不会破坏既有 schema。

## 与上游的有意分歧（保留本仓库实现）

上游 main 在 v1.0.2/v1.0.3 期间**回退**了若干既有加固，本仓库继续保留：

| 项 | 上游做法 | 本仓库 |
|---|---|---|
API Key 鉴权 | 去掉 `key_hash`，退回全量解密扫描 | 保留 `key_hash` 等值索引（O(1)） |
插件 KV 存储 | 改纯内存，删 `plugin_storage` 持久化 | 保留按插件隔离的持久化 |
插件 gRPC 取消 | `Chat` 去掉 ctx，改 `context.Background()` | 保留可取消 ctx（重试/超时可中断） |
请求体限制 | 删掉 32MB 预检 | 保留 32MB 预检（超限 413） |
流式错误告知 | 删掉失败时下发的 SSE 错误事件与 `drain` | 保留（客户端能感知上游失败） |
删分组清理 | `deleteGroup` 退回裸 `Delete` | 保留事务式清理路由 `groups_json`/降级指向 |
`internal/util.TruncStr` | 删除整个包 | 保留（多处在用） |

## 二开改动（Sndeok fork，基于 v1.0.2）

面向「Codex → new-api → cph → 上游」链路的排查与修复，核心侧改动只需替换二进制即可生效。

- Fixed 工具调用（function calling）链路完全不可用：OpenAI 方言上游只在首个增量带
  `tool_calls[].id/name`，`sdk/openaiup` 把后续增量的空 id 原样透传，而核心按 id 分组，
  导致同一调用被拆成「空 id 的新块」，客户端拿到残缺 tool_calls（表现为一般对话正常、
  一涉及查看链接等工具操作就没回复）。新增 `internal/gateway/toolcall.go` 统一归一，
  三协议（chat_completions / messages / responses）编码器共用；老插件无需重编即恢复。
- Fixed `/v1/responses` 流式缺 `response.output_item.done`（function_call）：Codex CLI
  只认 done 事件里的工具调用，缺失时工具永不执行。同时补齐
  `response.function_call_arguments.done`，`output_index` 改为按项递增，
  `response.completed` 带上 `output` 与 `usage`，`output_text.done` 回填完整文本。
- Fixed 插件宿主回调（日志 / 状态存储 / 读设置）在插件启动 5 秒后永久失效：
  go-plugin 的 broker 发出 ConnInfo 后只保留 5 秒，SDK 懒加载拨号必然错过，
  且失败被 `sync.Once` 缓存成终态；首次请求还会白等 5 秒。改为启动阶段异步
  warmup + 失败退避重试（需重新构建插件）。
- Fixed 插件子进程 stderr 被 go-plugin 吞掉（只记「收到 N 字节」）：现按行转发为核心
  日志并加 `[plugin:<name>]` 前缀，插件自身诊断不再丢失。
- Fixed 日志「总耗时」口径错误：原由各调用方传入局部 `time.Since(start)`，流式请求
  只统计首事件之后的输出阶段，出现「首字 5001ms、总耗时 1ms」。统一以 serve 入口为
  基准，在 `requestLogCtx.write` 内计算。
- Added 调用日志分页与多维过滤：`GET /admin/logs` 支持 page/page_size（≤200）、
  key_id、plugin_id、account_id、protocol、model、status(ok/error/4xx/5xx)、q、
  from/to、min_latency，返回 total/has_more；旧 `limit` 参数仍兼容。
  同时消除旧实现「每查一页日志就全表扫 keys」的 O(n) 开销（改为只反查当前页出现的 id）。
- Added 迁移 000003：`request_logs` 增加 requested_model（请求模型 / 路由别名）、
  route_id、group_id、stream、finish_reason、attempts（含重试换号降级的尝试次数）、
  error_type 与两个过滤索引。
- Added 日志保留策略 `logs.retention_days`（0 = 永久保留）+ 手动清理
  `POST /admin/logs/cleanup`（N 天前 / 全部），后台每 6 小时自动清理一次。
- Added 插件市场出站代理 `network.market_proxy`（也可用 `CPH_MARKET_PROXY` 设默认）：
  拉取索引与下载 .cphplugin 共用，支持 socks5 / socks5h / http / https，
  省略协议头按 socks5 处理、域名交代理解析；与原有 `network.github_proxy`
  （URL 前缀改写）并存。新增 `POST /admin/settings/test-market` 供页面自检连通性。
- Changed 默认插件市场地址改为自建插件仓库
  `https://raw.githubusercontent.com/Sndeok/ClawProxyHubPlugins/main/index.json`。
- Changed 仪表盘日志页重写：过滤条、服务端分页、请求模型/上游模型/流式/尝试次数列、
  行详情抽屉、自动刷新、清理日志；设置页新增日志保留天数与市场代理（含测试连接）。

## v1.0.2

- Fixed `CPH_SEED_API_KEY` 每次重启重复创建密钥：AES-GCM 随机 nonce 导致等值查重永不命中；改用 SHA-256 hash 判重
- Fixed API 密钥鉴权每次请求全表扫描解密（O(n)）：`keys` 表新增 `key_hash` 等值索引列，快路径 O(1) 命中，存量密钥自动回填 hash
- Fixed 网关重试/超时后插件事件流 goroutine 泄漏：gRPC Chat 调用改为传入可取消 context，重试前 cancel 旧流并后台排空 channel
- Fixed 插件 KV 状态存储全部插件共享且不持久化：每个插件实例持有独立 HostService，按插件名隔离 key 空间，持久化到 `plugin_storage` 表
- Fixed 删除分组后路由 `groups_json` / `failover_group_id` 残留引用：删除时事务式清理路由配置、降级指向、账号关联与代理绑定
- Fixed 流式响应中途上游失败客户端无感知：TaskFailed 时在 SSE 关闭前发出错误事件（含错误类型/消息/状态码）
- Fixed 请求体超 32MB 静默截断：预检 `Content-Length` + chunked 尾部检测，超限返回 413
- Fixed `truncStr` 截断中文产生无效 UTF-8：统一改用 `util.TruncStr`，回退到 rune 边界保证不切半个字符
- Fixed Windows 插件安装/升级时二进制文件锁导致删除失败：`removeWithRetry` 重试 3 次 × 200ms
- Added 出站代理参数校验：scheme 限定 `http`/`https`/`socks5`，port 限定 1–65535
- Added 数据库迁移 000002：`keys.key_hash` 列（带索引）+ `plugin_storage` 表（插件持久化 KV），存量数据自动兼容

## v1.0.1

- Fixed Codex CLI（`/v1/responses`）请求失败（表现为 502 / 上游 500）：`extractText` 未识别 `input_text` / `output_text` 内容块，导致消息文本被静默丢空、上游收到空请求
- Fixed `developer` 角色（Responses / Chat Completions）被透传给只认 `system/user/assistant/tool` 的上游而被拒；现归一为 `system`
- Fixed Codex 流式中断 `missing field input_tokens`：`response.completed` 的 `usage` 缺失时补零值（`input_tokens` / `output_tokens` / `total_tokens`）
- Added 从 Responses 请求提取 `reasoning.effort` 并透传上游 `reasoning_effort`（此前被丢弃）
- Added `TestParseResponsesRequestCodex` 回归测试，复刻 Codex CLI 真实请求

## v1.0.0

- 首个正式版本：Claw 类客户端统一管理反代网关，单二进制核心 + 插件化实现
- 三协议归一化入口（`/v1/messages`、`/v1/chat/completions`、`/v1/responses`），任意入口 × 任意上游方言全矩阵转换
- 路由（模型别名 → 分组 → 账号，多策略）、401 刷新换号、4xx/5xx 降级、首事件超时
- 账号多步登录、AES-256-GCM 凭据加密、429/无积分自动暂停、分组级出站代理
- 任务调度（interval/daily/once）、插件市场（在线索引 + sha256 校验 + 离线兜底）
- Vue3 + TDesign 双语暗色仪表盘，Docker 部署，SQLite + golang-migrate 自动迁移