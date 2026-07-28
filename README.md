# stock-agents-cursor

美股 **策略驱动** 纸面交易多智能体系统：激活策略决定开盘前 / 盘中调度节奏与风控执行模式；Go API 编排与 Alpaca Paper 网关；**agent-runtime**（Analyst + Portfolio 工具循环）分析/提案；Next.js 总览、Runs 与审批。

产品说明：`docs/product-overview.md`  
产品 PRD：`docs/prd.md`  
设计规格：`docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md`（V1 基线；**调度节奏已由策略规格取代**）  
策略调度：`docs/superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md`  
Alpaca Paper 权威：`docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`  
流程说明：`docs/workflow-flowchart.md`  
部署说明：`deploy/README.md`

**调度：** 由当前**激活策略**的 pre-open / intraday 配置驱动（热重载）；也可在 UI 手动触发一次 run（`POST /api/v1/runs/trigger`）或通过内部端点 `POST /internal/runs/trigger`（`X-Internal-Token: $INTERNAL_RUN_TOKEN`）。

**权威边界：** Alpaca Paper 为现金、持仓、订单与成交价的权威；Postgres 存 runs / proposals / approvals 及订单镜像。`INITIAL_CASH` 仅用于离线测试。

## 环境变量（`.env`）

**放这里：** `deploy/.env`（与 `docker-compose.yml` 同目录）

```bash
# 从模板复制（不要提交真实密钥）
cp deploy/env.example deploy/.env
```

编辑 `deploy/.env`，至少检查：

| 变量 | 说明 |
|------|------|
| `JWT_SECRET` | 登录 JWT 密钥（务必改掉默认值） |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | 单用户管理员账号 |
| `LLM_MODE` | `mock`（本地/CI 默认）或 `live`（实模型） |
| `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` | 兼容旧配置：未设 `LLM_PRIMARY_*` 时作为单一 provider（无 failover） |
| `LLM_PRIMARY_*` / `LLM_FALLBACK_*` | live 时推荐：主路由（Volcengine Ark）+ 备用（MiniMax）；见下方示例 |
| `AGENT_RUNTIME_URL` | Go → agent-runtime（Compose 内默认 `http://agent-runtime:8001`） |
| `FINNHUB_API_KEY` | 可选；新闻工具缺 key 时优雅降级 |
| `WEB_SEARCH_ENABLED` | 默认 `true` |
| `WEB_SEARCH_PROVIDER` | 默认 `tavily` |
| `WEB_SEARCH_API_KEY` | 可选；网页搜索缺 key 时优雅降级 |
| `MAX_TOOL_ROUNDS_ANALYST` / `MAX_TOOL_ROUNDS_PORTFOLIO` | 工具循环轮次上限（默认 8 / 8） |
| `ALPACA_API_KEY` / `ALPACA_API_SECRET` | **必填**（Paper 交易与行情）；仅服务端持有，勿暴露给浏览器 |
| `ALPACA_BASE_URL` | 默认 `https://paper-api.alpaca.markets` |
| `ALPACA_DATA_BASE_URL` | 默认 `https://data.alpaca.markets` |
| `MARKET_DATA_PROVIDER` | 默认 `alpaca`；无密钥时可设 `free`（Yahoo）作开发回退 |
| `ALPACA_STREAM_ENABLED` | 默认 `false`（E2E 期望 stream 返回 503）；`true` 时开 JWT SSE（需密钥） |
| `INTERNAL_RUN_TOKEN` | 内部/脚本触发工作流（`POST /internal/runs/trigger`，请求头 `X-Internal-Token`） |

`deploy/env.example` 是可入库模板；`deploy/.env` 已被根目录 `.gitignore` 忽略，勿提交。

**Live 模型路由（`LLM_MODE=live`）示例** — 在 `deploy/.env` 填入占位符（勿提交真实密钥）：

```text
# Primary: Volcengine Ark Doubao-Smart-Router (or ep-xxxxxxxx)
LLM_PRIMARY_API_KEY=
LLM_PRIMARY_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
LLM_PRIMARY_MODEL=Doubao-Smart-Router

# Fallback: MiniMax（务必显式设置 BASE_URL；未设时代码默认 https://api.openai.com/v1）
LLM_FALLBACK_API_KEY=
LLM_FALLBACK_BASE_URL=https://api.minimaxi.com/v1
LLM_FALLBACK_MODEL=MiniMax-M3
```

若仅配置 `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL`（不设 `LLM_PRIMARY_*`），行为与旧版相同：单 provider、无 failover。

本地非 Docker 跑 API 时，也可把同样变量导出到 shell，或仍用 `deploy/.env` 作参考。

## 快速启动（Docker）

```bash
cp deploy/env.example deploy/.env
# 编辑 deploy/.env

cd deploy
docker compose up --build
```

或从仓库根目录：

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.override.yml up --build
```

| 服务 | 地址 |
|------|------|
| Web | http://localhost:3000 |
| API | http://localhost:8080 |
| agent-runtime | http://localhost:8001 |

健康检查：`curl http://localhost:8080/healthz`  
默认登录：`admin` / `admin123`（以你的 `.env` 为准）

工作流冒烟：`.\deploy\smoke_run.ps1` 或 `./deploy/smoke_run.sh`  
API E2E（需先 `docker compose up --build`，且 `.env` 中配置 Alpaca Paper 密钥）：`.\deploy\e2e_api.ps1` 或 `./deploy/e2e_api.sh`  

E2E 覆盖：Alpaca overview / portfolio / orders、strategies、手动 run 终态、approvals、settings、stream（未开流式时期望 503）。本地最近一次：`e2e_api.ps1` **17/17 PASS**（2026-07-28）。详情见 `deploy/README.md`。

## 仓库结构（简要）

```text
apps/web/              Next.js 前端
services/api/          Go (Gin) API + Alpaca 网关 + 策略调度 + 工作流
services/agents/runtime/  agent-runtime（Analyst + Portfolio 工具循环）
services/agents/common/   共享工具、行情、LLM 客户端
packages/contracts/    JSON Schema / API DTO
deploy/                Compose、env.example、.env（本地）、smoke / e2e 脚本
```
