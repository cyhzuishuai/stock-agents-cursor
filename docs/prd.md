# 产品需求文档（PRD）

**产品：** 美股策略驱动纸面交易多智能体系统（stock-agents-cursor）  
**版本：** 现状交付版（As-shipped）  
**日期：** 2026-07-28  
**状态：** 基于已实现功能整理；非前瞻愿景稿  
**产品说明：** [`docs/product-overview.md`](./product-overview.md)

---

## 1. 背景与目标

### 1.1 背景

需要一套可自托管的美股纸面交易台：用多智能体生成结构化交易提案，用确定性风控约束执行，并以 Alpaca Paper 作为账户与成交权威，便于验证「策略节奏 + Agent 链 + 风控门控 + 人工审批」闭环。

### 1.2 产品目标

1. 以**激活策略**配置交易节奏与执行模式，并驱动进程内调度。
2. 固定流水线调用 Data → Research → Decision → Portfolio → Risk（建议），产出可审计的逐步结果。
3. 仅由 Go API 在门控通过（或 bypass / 人工批准）后向 **Alpaca Paper** 提交市价单。
4. 提供单用户控制台：总览、持仓、Runs、审批、Settings。
5. 全栈可通过 Docker Compose 一键拉起，并用冒烟 / API E2E 验证主路径。

### 1.3 成功标准（已交付口径）

| ID | 标准 | 验收线索 |
|----|------|----------|
| S1 | 激活策略驱动 pre-open / intraday；热重载 | Settings 激活策略后调度行为变化；无激活策略无自动 tick |
| S2 | 手动触发工作流可用 | UI Run now / `POST /api/v1/runs/eod` / `POST /internal/eod/run` |
| S3 | 五步 Agent + 逐步 payload 可查 | Run 详情 `steps[].payload_json` |
| S4 | 三种 `execution_mode` 行为正确 | 超限 → 待审 / 拒绝 / 跳过风控 |
| S5 | Alpaca 为 cash/positions/orders/fill 权威 | Overview/Portfolio/Orders 读经纪商；提案成交同步 |
| S6 | Settings 可编辑观察列表与风控数值 | 增删符号、`can_hold`、PATCH risk key |
| S7 | Compose + 密钥服务端 | `deploy/.env`；浏览器无 Alpaca 密钥 |
| S8 | API E2E 主路径通过 | `deploy/e2e_api.ps1` / `.sh`（需 Paper 密钥） |

---

## 2. 范围

### 2.1 In Scope（当前产品必须具备）

- 单管理员 JWT 认证
- 策略库 CRUD（系统默认不可删；不可删激活中策略）、激活唯一、调度字段、三种执行模式
- 策略调度（美东日历工作日假设；常规开盘 09:30 ET）+ 手动 run
- 五 Agent HTTP 工作流、Schema 校验失败则 run `failed` 且本 run 不向 Alpaca 提交
- Go 风控规则评估（非 `bypass_risk`）
- Alpaca Paper 下单与账户/持仓/订单展示；订单镜像入库
- 人工审批（按笔）与 run 取消
- Runs 列表/详情可观测（含 strategy、trigger、step payload）
- Settings：观察列表搜索/增删/`can_hold`；已有风控键改值；行情 provider 只读展示
- 前端分层轮询；可选 SSE（可关闭）
- Docker Compose 部署文档与冒烟/E2E 脚本

### 2.2 Out of Scope（明确不做 / 未作为当前交付）

- 实盘交易默认路径与实盘专用 UI
- 多租户、多角色、SSO
- 期权 / Crypto 产品化、复杂保证金策略
- 完整回测引擎、工作流可视化编辑器
- 浏览器直连 Alpaca WebSocket
- 策略级独立观察列表 / 策略级风控覆盖（全局 Settings 为准）
- 多策略同时激活
- 盘中止盈止损自动触发执行
- 审批自动过期
- 广域选股 / 全市场扫描 UI（仅观察列表 + 符号搜索）

---

## 3. 角色与权限

| 角色 | 认证 | 权限 |
|------|------|------|
| Admin | `POST /api/v1/auth/login` → JWT | 全部 `/api/v1/*` 已认证接口；内部触发另需 `INTERNAL_EOD_TOKEN` |

无访客只读模式；无多用户隔离。

---

## 4. 功能需求

### 4.1 认证

| ID | 需求 | 验收 |
|----|------|------|
| AUTH-1 | 使用配置的管理员账号密码登录，返回 JWT | 错误凭证拒绝；正确凭证可访问受保护 API |
| AUTH-2 | 受保护路由无 Token 或非法 Token 拒绝 | 401/等价错误 |
| AUTH-3 | `GET /api/v1/auth/me` 返回当前用户信息 | 登录后可用 |

### 4.2 Overview / Portfolio / Orders

| ID | 需求 | 验收 |
|----|------|------|
| DESK-1 | Overview 展示 NAV、Equity、Cash、待审数量、最近 run、持仓摘要等 | 数据来自 Alpaca（+ DB 的 runs/approvals） |
| DESK-2 | Portfolio 展示持仓与相关指标 | 与 Alpaca 持仓一致（缓存 TTL 内可略延迟） |
| DESK-3 | 可列出订单（含与提案/run 的关联展示能力） | 读 Alpaca + 本地镜像 join |
| DESK-4 | 前端按开闭市分层轮询；页签隐藏暂停；支持手动 Refresh | 开市更勤、闭市更稀或停 |

### 4.3 策略与调度

| ID | 需求 | 验收 |
|----|------|------|
| STR-1 | 列出 / 创建 / 编辑 / 删除策略 | 系统默认不可删；激活中不可删 |
| STR-2 | 激活策略为全局唯一 | 激活 A 后其余 `is_active=false` |
| STR-3 | 可配置 pre-open 分钟、intraday 间隔与 ET 窗口、`execution_mode` | 校验：间隔为 0 或 ≥15；时间合法；模式 ∈ 三枚举 |
| STR-4 | 激活或修改激活策略调度字段后调度器热重载 | 无需重启 API 进程即可换节奏 |
| STR-5 | 无激活策略时不注册自动调度 | 仅手动/内部触发可用 |
| STR-6 | 种子默认策略「整体策略1」存在且可激活 | 默认值符合产品说明中的示例节奏 |

### 4.4 工作流 Runs

| ID | 需求 | 验收 |
|----|------|------|
| RUN-1 | 手动触发创建 run，`trigger=manual`，挂上当前激活 `strategy_id`（若有） | Runs API/UI 可见 |
| RUN-2 | 调度触发分别标记 `pre_open` / `intraday` | 字段可查 |
| RUN-3 | 同账户同时仅一个工作流执行 | Redis busy 锁；冲突则跳过/不并行 |
| RUN-4 | 顺序执行五 Agent；每步结果入库 | 详情可见 step 状态与 payload |
| RUN-5 | Agent/Schema/关键失败 → run `failed`，本 run 不向 Alpaca 提交 | 无部分假成交 |
| RUN-6 | 终态：`executed` / `awaiting_approval` / `failed` / `cancelled` | 语义见产品说明 |
| RUN-7 | 允许同一 run 内部分提案提交、部分拒绝或待审 | 与风控模式一致 |
| RUN-8 | 可取消 run：取消待审；已提交订单保留 | Cancel API/UI |

### 4.5 风控、`can_hold` 与下单

| ID | 需求 | 验收 |
|----|------|------|
| RISK-1 | 非 `bypass_risk` 时对每笔提案评估已配置规则 | 含 notional / 单票权重 / 最低现金比等 |
| RISK-2 | `require_approval` 超限 → 创建 approval，不提交 | Approvals 可见 breach 原因 |
| RISK-3 | `auto_reject_breaches` 超限 → 提案 `rejected`，不创建 approval | 无待审时可 `executed` |
| RISK-4 | `bypass_risk` 跳过 Go 风控，可执行提案直接提交 | 仍受经纪商拒绝等约束 |
| RISK-5 | 通过门控后提交 Alpaca **市价单**，并同步至 terminal 或超时 | proposal 状态与镜像订单更新 |
| RISK-6 | `can_hold=false` 的标的不得新增买入成交 | 买入侧门控 |
| RISK-7 | 缺 Alpaca 凭证时失败清晰，不静默本地假成交 | run/配置错误可感知 |
| RISK-8 | Python Risk 输出仅审计，不覆盖 Go 判定 | 存储但不改门控结果 |

### 4.6 审批

| ID | 需求 | 验收 |
|----|------|------|
| APR-1 | 列出待审 / 历史审批项 | Approvals 页 |
| APR-2 | 按笔 approve → 向 Alpaca 提交；reject → 提案拒绝 | 状态与订单侧一致 |
| APR-3 | 可附决策备注 | 持久化可查 |

### 4.7 Settings

| ID | 需求 | 验收 |
|----|------|------|
| SET-1 | 读取观察列表（symbol + `can_hold`）、风控 map、行情 provider | `GET /settings` |
| SET-2 | 符号搜索（后端代理）后添加观察列表 | 重复 → 冲突错误 |
| SET-3 | 切换 `can_hold`、删除符号 | PATCH/DELETE |
| SET-4 | 仅允许修改已存在风控键的数值 | 未知 key → 404；不可创建/删除键 |
| SET-5 | Strategies 区块完成 STR-* 交互 | 与 API 一致 |

### 4.8 流式（可选能力）

| ID | 需求 | 验收 |
|----|------|------|
| STRM-1 | `ALPACA_STREAM_ENABLED=false` 或未就绪时，stream 接口稳定返回 503 | E2E 期望 503 |
| STRM-2 | 开启时 JWT 可订阅 Go 代理的 market/account SSE | 浏览器无 Alpaca 密钥 |

---

## 5. 非功能需求

| ID | 类别 | 需求 |
|----|------|------|
| NFR-1 | 部署 | Docker Compose 可启动 web/api/agents/postgres/redis |
| NFR-2 | 安全 | `JWT_SECRET`、Alpaca、LLM 密钥仅服务端；`.env` 不入库 |
| NFR-3 | 可靠 | 工作流互斥；Agent 超时/有限重试后失败可观测 |
| NFR-4 | 性能（展示） | 账户/持仓短 TTL 缓存；开市轮询约 15–30s 量级（实现可微调） |
| NFR-5 | 可测 | 单元/集成不依赖真实 Alpaca；可选本地 E2E 使用 Paper 密钥 |
| NFR-6 | 可运维 | `/healthz`；内部 token 触发；smoke / e2e 脚本 |

---

## 6. 数据与权威边界

| 数据 | 权威来源 | 本地（Postgres）角色 |
|------|----------|----------------------|
| 现金 / 权益 / 持仓 / 成交价 | Alpaca Paper | 可选镜像 / 审计，非成交权威 |
| 订单（经纪商侧） | Alpaca Paper | 镜像行（proposal/run 关联） |
| Runs / Steps / Proposals / Approvals / Strategies / Watchlist / Risk 配置 | Postgres | 系统编排与配置 SoR |
| Agent 逐步产出 | Postgres `workflow_step_results.payload_json` | 可观测与审计 |

信任边界与流程图以 [`docs/eod-workflow-flowchart.md`](./eod-workflow-flowchart.md) 为准。

---

## 7. 用户故事（摘要）

1. **作为**管理员，**我希望**配置并激活一份策略，**以便**按美东开盘前与盘中节奏自动跑纸面交易工作流。
2. **作为**管理员，**我希望**在 Run 详情看到每步 Agent 输出，**以便**理解为何产生某笔提案。
3. **作为**管理员，**我希望**超限提案按模式自动拒绝或进入审批，**以便**在无人值守与人工把关之间切换。
4. **作为**管理员，**我希望**总览与持仓反映 Alpaca Paper 真实账户，**以便**纸面结果可信。
5. **作为**管理员，**我希望**编辑观察列表与「可持仓」开关，**以便**控制 Agent 宇宙与买入范围。
6. **作为**管理员，**我希望**随时手动触发一次 run，**以便**开发调试与补跑。

---

## 8. 已知限制与后续（非本 PRD 交付承诺）

以下内容可能在代码或规格中出现「阶段 / 后续」表述，**不纳入本 PRD 必达项**：

- 实盘切换与合规能力
- 更精细的交易日历（早收等）
- 策略级宇宙与风控覆盖
- 止盈止损自动执行
- 以 SSE 完全替代轮询作为唯一刷新路径
- 多账户 / 多租户

后续变更应另开设计规格与修订 PRD，避免与「现状交付版」混淆。

---

## 9. 验收对照（工程）

建议以现有脚本作为发布前回归：

| 检查 | 命令 / 入口 |
|------|-------------|
| 健康检查 | `GET /healthz` |
| 工作流冒烟 | `deploy/smoke_eod.ps1` 或 `smoke_eod.sh` |
| API E2E | `deploy/e2e_api.ps1` 或 `e2e_api.sh`（需 Paper 密钥；覆盖 overview/portfolio/orders、strategies、手动 run 终态、approvals、settings、stream 503 等） |

部署与环境变量说明：[`deploy/README.md`](../deploy/README.md)、根目录 [`README.md`](../README.md)。

---

## 10. 规格溯源

| 主题 | 规格 |
|------|------|
| V1 基线（部分已取代） | `docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md` |
| 策略调度 + Runs 可观测 | `docs/superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md` |
| Alpaca Paper 权威 | `docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md` |
| Settings 观察列表 / 风控编辑 | `docs/superpowers/specs/2026-07-28-settings-watchlist-risk-edit-design.md` |
| Desk UI（日结/展示相关） | `docs/superpowers/specs/2026-07-27-day-settlement-desk-ui-design.md` |

---

## 11. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-07-28 | 初版：依据已实现功能整理为现状交付版 PRD |
