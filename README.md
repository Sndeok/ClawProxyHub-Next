<div align="center">

# ClawProxyHub-Next

**把官方 AI 桌面客户端，变成你自己的 OpenAI 兼容网关**

`Go 1.26` · `Next.js 15 + Tailwind v4` · `gRPC 插件` · `SQLite` · `Docker`

</div>

---

## 这是什么

ClawProxyHub-Next 是 [ClawProxyHub](https://github.com/Sndeok/ClawProxyHub) 的重构版（下称"本项目"）。

它解决的问题很具体：**官方桌面客户端（LobsterAI / WorkBuddy 等）的账号，能不能变成标准 API 用？**

- 官方客户端怎么登录，你就怎么登录（手机验证码 / 浏览器授权 / 凭据文件），凭据由本项目加密托管
- 对外只暴露标准协议：OpenAI Chat Completions / Responses / Anthropic Messages，
  `Codex CLI`、`Claude Code`、`CC Switch`、`new-api` 这类客户端直接对接
- 多账号轮换、分组加权、失败降级、会话粘性、快过期积分优先，全部是配置项
- 每次调用都有结构化日志：走的哪个账号、命中哪条路由、首字耗时、缓存命中、重试次数

## 与上一代的区别

| | ClawProxyHub（上一代） | ClawProxyHub-Next（本项目） |
|---|---|---|
| 前端 | Vue 3 + TDesign，全量注册 | **Next.js 15 静态导出**，共享 JS 103 KB |
| 前端架构 | 13 个整页组件，逻辑与视图混写 | 页面 + UI kit + lib 分层，页面只做编排 |
| 后端结构 | `gateway` 三套协议各写一份聚合器 | 抽出 `aggregateCore` 公共层 |
| 设计语言 | 自研蓝紫主题 | shadcn/ui neutral（近黑 + 灰阶 + 10px 圆角） |
| 插件契约 | `cph.proto` v1 | **完全不变**（wire 兼容，旧插件可直接用） |

> Go module 路径为 `github.com/Sndeok/ClawProxyHub-Next`。插件仓库需按此路径编译，
> 但插件**二进制**与核心之间只靠 gRPC + protobuf wire 通信，**协议没变**。

## 功能

### 账号
- 多插件账号池：登录 / 刷新 / 过期标记 / 自动重新登录，凭据 AES-256-GCM 加密落库
- 账号级出站代理（优先级 账号 > 分组），支持 `socks5` / `socks5h` / `http(s)`
- 积分快照：剩余 / 总 / 已用 + **分池到期时间**（账号页直接看到"快过期积分"倒计时）
- 今日消耗：按调用日志聚合 token 与积分

### 路由
- 三级解析：对外模型名 → 分组（权重）→ 账号（策略）
- 策略：`轮询` / `随机` / `最少使用` / `会话粘性` / `过期优先`
- 会话粘性：同一会话固定账号，多轮对话连贯、上游 prompt 缓存命中率更高（保持时长/清理周期可配）
- 失败降级：4xx / 5xx 触发，切备用分组与备用模型
- 一键同步上游模型：账号可见模型并集 → 补齐缺失路由（默认会话粘性）

### 网关
- 三种协议入口 + 协议间自动转换（Responses ↔ Chat ↔ Messages）
- 多模态内容贯通：图片 / 文件 / 音频 / 视频按 content block 透传
- 工具调用跨协议重组：增量参数按 index 归位、缺 id 自动补齐
- 用量口径统一：`input_tokens` 含缓存命中，`cached_tokens` 是其中子集
- 首字超时（全局 + 路由级）、重试换号、流式错误下发

### 可观测
- 调用日志：服务端分页 + 多维过滤（时间/状态/协议/密钥/账号/模型/关键词/慢请求）
- 保留策略：按天自动清理（每 6 小时巡检）+ 手动清理
- 概览：今日请求、成功率、总 Token、趋势图、渠道积分概览、最近请求（分页）

### 运维
- 任务调度：签到等维护任务（周期/每日/一次性），一键全部执行 + 执行历史
- 插件市场：自建仓库索引，支持 SOCKS5/HTTP 代理
- 出站标识按插件配置（UA / 客户端名称 / 版本 / CLI 版本），默认对齐官方分发包
- 会话粘性策略、出站标识保存即生效，无需重启

## 架构

```
                    ┌──────────── 客户端 ────────────┐
                    │ Codex CLI / Claude Code / ... │
                    └───────────────┬───────────────┘
        /v1/chat/completions │ /v1/responses │ /v1/messages
                                    ▼
   ┌───────────────────────────────────────────────────────────┐
   │                     ClawProxyHub-Next                     │
   │  鉴权(密钥) → 路由解析(Route→Group→Account) → 出站代理     │
   │      │              │                         │           │
   │      │      会话粘性 / 过期优先              │           │
   │      ▼              ▼                         ▼           │
   │   调用日志     信封协议(protobuf)          gRPC 插件      │
   │  (SQLite)                                                 │
   └────────────────────────────────────────────┬──────────────┘
                                                 ▼
                        LobsterAI / WorkBuddy / ... 官方上游
```

核心只做编排（登录、路由、记账、日志）；协议适配在上游侧由插件完成 ——
新增一个上游 = 新增一个插件，核心不改。

前端是 **Next.js 静态导出**，经 `go:embed` 打进同一个二进制，因此部署只有一个文件、一个端口。

## 快速开始

### Docker Compose

```bash
git clone https://github.com/Sndeok/ClawProxyHub-Next.git
cd ClawProxyHub-Next
docker compose up -d --build
```

打开 `http://<服务器>:8080`，按引导设置管理员密码，然后：

1. **插件页** → 从市场安装 `LobsterAI` / `WorkBuddy`
2. **账号页** → 添加账号（扫码 / 验证码 / 凭据文件）
3. **路由页** → 「同步上游模型」一键生成路由
4. **密钥页** → 创建调用密钥
5. 客户端 Base URL 填 `http://<服务器>:8080/v1`，API Key 填刚创建的密钥

### 本地开发

```bash
# 后端（需要 CGO，SQLite 驱动依赖）
go run ./cmd/cph

# 前端（Next dev server，把 /admin /v1 代理到 :8080）
cd web-next && pnpm install && pnpm dev
```

前端产物必须经 `web/dist` 才能被 embed：

```bash
cd web-next && pnpm build   # next build → out/ → 自动同步到 ../web/dist
cd .. && go build ./cmd/cph # 此时二进制里才有最新前端
```

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `CPH_ADDR` | `:8080` | 监听地址 |
| `CPH_DATA_DIR` | `./data` | 数据目录（SQLite / 插件 / 密钥） |
| `CPH_DATABASE_DSN` | `<data>/cph.db` | 数据库 DSN |
| `CPH_PLUGIN_DIR` | `<data>/plugins` | 插件安装目录 |
| `CPH_MARKETPLACE_URL` | 自建仓库 index.json | 插件市场地址（首次启动写入设置） |
| `CPH_MARKET_PROXY` | 空 | 市场出站代理 |
| `CPH_ADMIN_PASSWORD` | 空 | 首次启动引导管理员密码 |

界面可配：首字超时、日志保留天数、插件市场地址与代理、GitHub 加速代理、
会话粘性策略、按插件的出站标识。

## 目录结构

```
cmd/cph/            进程入口：装配数据库 / 插件 / 路由 / 网关 / 管理后台
internal/
  account/          账号域：登录、刷新、积分快照、凭据加解密
  admin/            管理后台 API（按域分文件：accounts / keys / logs / tasks / ops / settings）
  database/         SQLite 打开与迁移（已发布编号只增不改）
  gateway/          协议出入口：aggregate.go 是三种协议的公共聚合层
  model/            GORM 模型
  plugin/           插件宿主：子进程、gRPC broker、市场安装
  router/           Route → Group → Account 解析 + 会话粘性 + 过期优先
  setting/          设置 KV（含热生效的粘性策略 / 出站标识）
  task/             任务调度引擎
sdk/                插件 SDK（proto + 上游适配器 openaiup / anthropicup）
web/                go:embed 包（嵌入 web/dist）
web-next/           Next.js 前端源码（页面 + components/ui + lib）
```

## 插件

插件是独立进程，通过 gRPC 与核心通信，SDK 就是本仓库的 `sdk/`。

- 插件仓库：[Sndeok/ClawProxyHubPlugins](https://github.com/Sndeok/ClawProxyHubPlugins)
- 新增上游 = 实现 `pb.ClawPluginServer`，声明鉴权方式与任务能力，打包成 `.cphplugin`

> 改了 `sdk/`（例如用量解析）必须重新编译插件，否则插件二进制里还是老逻辑。

## 当前进度

本项目正在按批次重构，已完成：

- [x] 后端结构重构（协议公共层、管理后台按域拆包）
- [x] Next.js 前端骨架 + 概览 / 日志 / 账号 / 路由四个核心页
- [ ] 插件页 / 密钥页 / 分组页 / 代理页 / 任务页 / 设置页（侧栏标注「开发中」）
- [x] Docker 单镜像（前端内嵌）

## 安全说明

- 账号凭据以 AES-256-GCM 加密落库，密钥文件在数据目录，**务必备份**
- 管理后台走 Bearer Token（管理员密码 bcrypt 存储）
- 出站标识做了换行与长度校验，避免请求头注入
- 本项目用于个人账号的自动化代理，请自行评估上游服务条款

## 许可

见 [LICENSE](./LICENSE)。