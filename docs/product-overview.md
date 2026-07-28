# 产品说明：美股策略驱动纸面交易多智能体系统

**文档类型：** 产品说明（现状快照）  
**日期：** 2026-07-28  
**依据：** 当前已实现功能与已交付设计规格  
**配套 PRD：** [`docs/prd.md`](./prd.md)

---

## 1. 产品定位

本产品是一套**自托管**的美股 **Alpaca Paper** 纸面交易系统：由**激活策略**决定开盘前 / 盘中调度节奏与风控执行模式；Go API 负责编排、确定性风控与经纪商网关；单一 **agent-runtime**（Analyst + Portfolio LangGraph 工具循环）产出结构化分析与交易提案；Next.js 控制台提供总览、持仓、Runs、审批与设置。

一句话：**策略驱动节奏 → 多智能体提案 → Go 风控门控 → Alpaca Paper 成交权威 → 人工仅在需要时介入。**

---

## 2. 目标用户与使用场景

| 角色 | 说明 |
|------|------|
| 单管理员 | JWT 登录；无多租户、无多角色 |

**典型场景**

- 用固定观察列表（约 10–30 只美股）做纸面组合管理，而非纯信号看板。
- 按策略在美东常规交易时段自动跑工作流（开盘前 + 盘中），或在 UI 手动触发一次。
- 查看 Agent 逐步产出、提案与订单结果；对超限提案按模式自动拒绝或人工审批。
- 在 Settings 维护策略、观察列表（含「可持仓」）与风控阈值。

---

## 3. 核心能力

### 3.1 策略驱动调度

- 系统维护多份策略，**同一时刻仅一份 `is_active`**。
- 激活策略决定：
  - **Pre-open**：常规开盘（09:30 ET）前 N 分钟触发（`0` 表示关闭）。
  - **Intraday**：美东窗口内按间隔触发（间隔 `0` 关闭；`>0` 时 ≥15 分钟）。
  - **`execution_mode`**：风控超限后的处理方式（见下）。
- 激活或修改激活策略的调度字段后，进程内调度器**热重载**。
- 无激活策略时不注册自动 tick。
- **手动触发**：UI「Run now」或 `POST /api/v1/runs/trigger`（JWT）；可选 `trade_date`（默认美东当日）。
- **内部触发**：`POST /internal/runs/trigger`，请求头 `X-Internal-Token: $INTERNAL_RUN_TOKEN`（调度器 pre-open / intraday 与脚本冒烟）。

**系统默认策略「整体策略1」示例：** 开盘前 10 分钟（09:20 ET）；盘中 10:00–15:00 ET 每 60 分钟；`execution_mode=auto_reject_breaches`。

### 3.2 Analyst → Portfolio 工具循环

Go 注入 Alpaca 账户快照（现金 / 权益 / 持仓 / 未成交订单）与 `risk_context`，依次调用 **agent-runtime** 两次（`agent=analyst` → `agent=portfolio`）。每步返回 `{result, trace}`；Go 持久化完整 envelope，并将 `result` 传给下一步。Runs 详情可展开工具时间线（`trace.rounds`）。

- 同一 run 内，Go 将 Analyst 的 `handoff` / `working_memory` 以 `prior_step_outputs.analyst_handoff` 与 `analyst_working_memory` 注入 Portfolio；`analyst` 仍为 result（含 `items`）。缺手交信息不导致失败。

Analyst 与 Portfolio 均走 **plan → act → reflect → finalize** 控制流：模型先产出分步计划（plan），按步调用工具（act），每步结束后反思（reflect）以决定继续、改计划或进入 finalize 输出结构化 JSON。trace 除 `rounds` 外还含 `plan`、`events[]` 与 `working_memory` 快照；Analyst envelope 可附带 `handoff` 供下游 Portfolio 使用。JSON 解析失败时在同一 provider 上 repair 重试，不切换 ModelRouter。

agent-runtime 通过 `LLM_MODE=mock|live` 切换 mock 与实模型；live 时由 ModelRouter 路由：主 provider（`LLM_PRIMARY_*`，默认 Volcengine Ark）失败一次后切至备用（`LLM_FALLBACK_*`，需显式设置 MiniMax `BASE_URL`）。Analyst 工具含日线行情、**Finnhub** 新闻、**Tavily** 网页搜索（缺 key 时优雅降级）、账户视图与风控上下文；Portfolio 工具含账户、风控、收盘价与 `size_proposals`。

| 步骤 | Graph | 产出要点 |
|------|--------|----------|
| 1 | Analyst | 每标的 bias / confidence / thesis / buy·sell·hold 意图（工具：日线、新闻、网页搜索、账户视图、风控上下文） |
| 2 | Portfolio | 可执行提案（数量/权重、止盈止损估计等；工具：账户、风控、收盘价、`size_proposals`） |

Agent-runtime **永不**调用 Alpaca Trading，也不直接改现金 / 持仓 / 订单。**Go 确定性风控**为最终门控（无 Python Risk 步骤）。

### 3.3 风控与执行模式

权威风控在 **Go**（`risk_rule_configs`），与激活策略的 `execution_mode` 组合：

| `execution_mode` | Go 风控 | 超限行为 | 向 Alpaca 提交 |
|------------------|---------|----------|----------------|
| `require_approval` | 是 | 创建待审 approval | 通过或人工批准后 |
| `auto_reject_breaches` | 是 | 提案直接 `rejected`，不创建待审 | 仅通过时 |
| `bypass_risk` | 否 | N/A | 可执行提案立即提交 |

默认规则示例：单笔最大名义金额 10000 USD、单票最大权重 20%、最低现金比例 10%（可在 Settings 改数值，不可增删规则键）。

另：观察列表符号的 **`can_hold`** 为 false 时，工作流对买入侧有门控（不可新增买入成交）。

### 3.4 Alpaca Paper 权威

- **Alpaca Paper** 为现金、权益、持仓、订单与成交价的权威来源。
- Go API 是唯一与 Alpaca Trading 交互的组件；浏览器不持有 `ALPACA_*`。
- Postgres 存 runs / steps / proposals / approvals / strategies，以及订单镜像（审计），**不是**成交权威。
- 下单类型：市价单；`client_order_id` 关联提案；成交后 REST 轮询同步状态。
- `INITIAL_CASH` 仅用于离线 / mock 测试。

### 3.5 人工审批与 Runs 可观测

- 待审列表支持按笔 approve / reject（可附备注）；可取消整次 run（已提交订单保留）。
- Runs 列表与详情展示状态、触发来源（`manual` / `pre_open` / `intraday` 等）、关联策略。
- Run 详情可展开各步骤，查看 Agent 返回的 `payload_json`（含 `{result, trace}` 工具轨迹）。
- 步骤内展示 **Agent 时间线**（`trace.events[]`：plan / step_start / llm / tool / reflect / handoff / finalize）；有 `handoff` 时显示摘要；仍保留 `trace.rounds[]` 工具轮次展开。无 `events` 的历史 run 自动回退为仅 rounds 视图。
- **LangSmith**（可选）：`LANGSMITH_TRACING=true` 且配置 `LANGSMITH_API_KEY` 时，agent-runtime 并行导出 trace 至 LangSmith 项目 UI；默认关闭，导出失败不阻断交易。本地 `payload_json.trace` 仍为完整审计权威。

### 3.6 控制台刷新

- Overview / Portfolio / Orders 等以 Alpaca（及短 TTL 缓存）为展示源；前端按美东开闭市做分层 REST 轮询，页签隐藏时暂停。
- 可选 Go 代理 SSE（`ALPACA_STREAM_ENABLED`）；关闭时流式接口返回 503。

---

## 4. 系统架构（简述）

```text
Web (Next.js, JWT)
  → Go API (Gin)：认证、调度、工作流、风控、审批、Alpaca 网关、可选 SSE
       → Alpaca Paper Trading / Market Data
       → agent-runtime (Python)：Analyst → Portfolio 工具循环
  PostgreSQL：编排与审计数据
  Redis：工作流 busy 锁、短时缓存等
```

| 服务 | 职责 |
|------|------|
| `web` | 登录与交易台 UI |
| `api` | 编排、风控、经纪商、策略调度 |
| `agent-runtime` | Analyst + Portfolio LangGraph 工具循环（单进程，:8001） |
| `postgres` / `redis` | 持久化与锁/缓存 |

部署以 Docker Compose 为主；本地 Web `http://localhost:3000`，API `http://localhost:8080`。细节见 [`deploy/README.md`](../deploy/README.md)。

---

## 5. 页面导览

| 路由 | 用途 |
|------|------|
| `/login` | 管理员登录 |
| `/` | Overview：NAV / Equity / Cash、待审提示、最近 run、持仓摘要、美东时钟、NAV 历史等 |
| `/portfolio` | 持仓、权重与盈亏等（Alpaca 权威） |
| `/runs` | 工作流历史；可手动 **Run now** |
| `/runs/[id]` | 步骤时间线、Agent events 时间线、提案结果、逐步 payload |
| `/approvals` | 待审提案的批准 / 拒绝 |
| `/settings` | 观察列表（搜索增删、`can_hold`）、风控阈值、策略 CRUD / 激活 |

---

## 6. 典型一天（产品叙事）

1. **开盘前**：激活策略在 09:20 ET（示例）触发 pre-open run；Analyst 工具循环分析观察列表 → Portfolio 产出提案 → Go 按模式门控 → 合规则提交 Alpaca。
2. **盘中**：例如每小时再跑；同一账户不并行跑两次工作流（Redis 全局 busy 锁）。
3. **人工**：若为 `require_approval` 且有超限，管理员在 Approvals 处理；也可随时 Run now 做冒烟或补跑。
4. **配置**：在 Settings 切换策略节奏或 execution_mode、调整观察列表与阈值；调度热重载，无需重启容器（策略侧）。

Run 终态简述：`executed`（无待审且提案均已终态）、`awaiting_approval`、`failed`（链路失败且本 run 不向 Alpaca 提交）、`cancelled`。

---

## 7. 信任边界（必读）

1. Agents 只产出提案，不写账、不下单。
2. **Go 规则引擎**为风控最终判定（`bypass_risk` 时跳过）；无 Python Risk 步骤。
3. Alpaca Paper 为账户与成交权威；本地账本路径不作为生产成交权威。
4. 密钥与经纪商凭证仅服务端持有。

更细的流程图见 [`docs/workflow-flowchart.md`](./workflow-flowchart.md)。

---

## 8. 明确不在当前产品范围内

以下**不是**当前交付能力（详见 PRD 非目标）：

- 实盘（非 Paper）默认路径或实盘 UI
- 多租户 / 多角色权限
- 期权、杠杆策略产品化、完整回测引擎
- 浏览器直连 Alpaca WebSocket
- 盘中止盈止损自动触发（字段可存，不作为当前自动执行能力）

---

## 9. 相关文档索引

| 文档 | 内容 |
|------|------|
| [`docs/prd.md`](./prd.md) | 产品需求与验收（现状） |
| [`README.md`](../README.md) | 快速启动与环境变量 |
| [`deploy/README.md`](../deploy/README.md) | Compose、冒烟与 E2E |
| [`docs/workflow-flowchart.md`](./workflow-flowchart.md) | 工作流与风控流程图 |
| [`docs/superpowers/specs/2026-07-28-remove-eod-naming-live-llm-design.md`](./superpowers/specs/2026-07-28-remove-eod-naming-live-llm-design.md) | 去除 EOD 命名与 live LLM 兼容 |
| [`docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md`](./superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md) | V1 基线（部分已被后续规格取代） |
| [`docs/superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md`](./superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md) | 策略调度与 Runs 可观测 |
| [`docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`](./superpowers/specs/2026-07-28-alpaca-paper-authority-design.md) | Alpaca Paper 权威 |
| [`docs/superpowers/specs/2026-07-28-settings-watchlist-risk-edit-design.md`](./superpowers/specs/2026-07-28-settings-watchlist-risk-edit-design.md) | Settings 观察列表与风控编辑 |
