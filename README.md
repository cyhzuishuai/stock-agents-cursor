# stock-agents-cursor

美股日终（EOD）模拟盘多智能体系统（V1）：Go API 编排与账本，Python Agent 分析/决策，Next.js 总览与审批。

设计规格：`docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md`  
部署说明：`deploy/README.md`

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
| `LLM_API_KEY` / `LLM_BASE_URL` | 实模型时填写；本地可用 Compose override 的 `LLM_MODE=mock` |
| `MARKET_DATA_PROVIDER` | 默认 `free`；Alpaca 为 stub，需密钥后再接 |
| `INTERNAL_EOD_TOKEN` | 内部触发 EOD 的 token |

`deploy/env.example` 是可入库模板；`deploy/.env` 已被根目录 `.gitignore` 忽略，勿提交。

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

健康检查：`curl http://localhost:8080/healthz`  
默认登录：`admin` / `admin123`（以你的 `.env` 为准）

EOD 冒烟：`.\deploy\smoke_eod.ps1` 或 `./deploy/smoke_eod.sh`

## 仓库结构（简要）

```text
apps/web/              Next.js 前端
services/api/          Go (Gin) API + 账本 + 工作流
services/agents/       Python Agent 容器（data/research/decision/portfolio/risk）
packages/contracts/    JSON Schema / API DTO
deploy/                Compose、env.example、.env（本地）、smoke 脚本
```
