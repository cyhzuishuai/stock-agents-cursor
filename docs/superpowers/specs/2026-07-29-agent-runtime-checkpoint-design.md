# Agent Runtime Checkpoint + HITL — Design Spec

**Date:** 2026-07-29  
**Status:** Approved for implementation planning  
**Related:** `2026-07-28-agent-runtime-tool-loop-design.md`, `2026-07-28-agent-runtime-plan-router-design.md`

## 1. Goal

给 `agent-runtime` 的 plan → act → reflect LangGraph 加上**真正的 checkpoint**：状态写入本地 SQLite，支持（1）崩溃后按 `thread_id` 续跑；（2）按需 Human-in-the-loop——仅当模型调用 `request_human_input` 时暂停。Go 做最小适配（识别 interrupted、落库、提供 resume API）；**V1 不做 Web UI**。

### 1.1 Success criteria

- Plan-loop 使用 LangGraph checkpointer（SqliteSaver），每次 invoke/resume 带 `thread_id`。
- 未调用 `request_human_input` 时，对外行为与现网一致：一次 `POST /v1/run` 返回完整 `{ result, trace, ... }`。
- 调用 `request_human_input` 时：HTTP 200 + `status: "interrupted"` envelope；图状态保留在 SQLite。
- `POST /v1/resume` 注入 `human_response` 后可继续至完成或再次 interrupted。
- Go：step `interrupted`、run `awaiting_agent_input`；`POST /api/v1/runs/:id/agent-resume` 可续跑后续 chain。
- 无可用 checkpointer（SQLite 不可写）时 fail closed（503），不假装可恢复。

### 1.2 Out of scope

- Web UI / Runs 页 HITL 交互
- Postgres / 多副本共享 checkpointer
- 每轮固定 interrupt（reflect/finalize 前强制暂停）
- 自动静默崩溃重试（避免重复副作用）
- 改动 trade-proposal `awaiting_approval` 审批流
- Checkpoint TTL 清理任务

## 2. Decisions (locked)

| Topic | Choice |
|-------|--------|
| Goals | 崩溃恢复 **+** HITL |
| HITL trigger | 显式工具 `request_human_input`（按需，非每轮） |
| Checkpointer | LangGraph **SqliteSaver**，本地文件 |
| Path env | `AGENT_CHECKPOINT_SQLITE_PATH`（默认容器内可挂卷路径） |
| Approach | SqliteSaver + tools 节点内 `interrupt(payload)` |
| Orchestration | Python 完整能力 + Go **最小**适配；UI 后做 |
| `thread_id` | `"{run_id}:{agent}"`（analyst / portfolio 各一条） |
| Completed thread | 禁止对已完成 thread 静默重跑 → `409`；可选 `force_new` 默认关 |
| Crash retry | 文档 + 同 `thread_id` 再调 `/v1/run`；V1 **不**自动重试 |

## 3. Architecture

```text
Go Runner                    agent-runtime                     SQLite
   │                              │                               │
   │ POST /v1/run                 │                               │
   │  thread_id=run_id:agent ────►│ compile(checkpointer)         │
   │                              │ invoke(config) ──────────────►│ writes
   │                              │                               │
   │◄── 200 {result,trace}        │ 正常结束                       │
   │   或 {status:interrupted,    │◄─ interrupt(human_request)    │ paused
   │       thread_id, human_request, partial_trace}
   │                              │                               │
   │ step=interrupted             │                               │
   │ run=awaiting_agent_input     │                               │
   │                              │                               │
   │ POST /v1/resume              │                               │
   │  {thread_id, human_response} │ Command(resume=…) ───────────►│ continue
   │◄── result | interrupted      │                               │
```

### 3.1 Trust boundaries

| Actor | May | Must not |
|-------|-----|----------|
| Analyst / Portfolio | 调用 `request_human_input` 暂停；读写 checkpoint 状态 | 跳过 Go risk；直接交易 |
| Go API | 落 interrupted；代理 resume；续跑 chain | 把 agent HITL 与 trade approval 混成同一状态机 |
| Browser (V1) | 无专用 UI | 直连 runtime / SQLite |

## 4. API contracts

### 4.1 Python `POST /v1/run`

- 请求：现有 `agent_run_request` + 可选 `thread_id`（缺省 = `{run_id}:{agent}`）。
- 契约：`additionalProperties` 需允许 `thread_id`（或显式加入 schema）。
- 成功完成：与现网兼容的 envelope（`result` / `trace` / `working_memory` / 可选 `handoff`）。
- Interrupted（HTTP **200**）：

```json
{
  "status": "interrupted",
  "thread_id": "123:analyst",
  "human_request": {
    "question": "...",
    "context": {},
    "options": ["yes", "no"]
  },
  "trace": {}
}
```

### 4.2 Python `POST /v1/resume`

```json
{
  "thread_id": "123:analyst",
  "human_response": {
    "text": "optional free text",
    "data": {}
  }
}
```

- 成功：同完成 envelope，或再次 interrupted。
- Thread 不存在 / 未 paused：`404` / `409`。
- Agent 与 thread 语义不匹配：由 Go 在调用前校验 `agent`；runtime 以 `thread_id` 为准。

### 4.3 Python `GET /v1/threads/{thread_id}`（可选，建议做）

返回 paused / completed / unknown 与最近 `human_request`，便于调试。

### 4.4 Go

| Item | Detail |
|------|--------|
| Step status | 新增 `interrupted` |
| Run status | 新增 `awaiting_agent_input`（≠ `awaiting_approval`） |
| HTTP | `POST /api/v1/runs/:id/agent-resume` body: `{ "agent", "human_response" }` |
| Client | `agentsclient` 识别 interrupted（非错误）；新增 `Resume()` → runtime `/v1/resume` |
| Runner | interrupted 时 persist step、设 run status、**停止**后续 agent；resume 成功后从该 step 更新并续跑余下 chain |

## 5. Components

### 5.1 Python

| Unit | Responsibility |
|------|----------------|
| `checkpoint.py` | SqliteSaver 单例；读 env；setup 失败 → 503 |
| `tools/human_input.py` | `request_human_input(question, context?, options?)` |
| `plan_loop` tools node | 本轮若含该 tool：其它 tool 先执行；对该 call 调 `interrupt(human_request)`；不写假 tool result |
| `graph.compile(checkpointer=...)` | 所有 invoke / resume 带 `configurable.thread_id` |
| `main.py` | `/v1/resume`；interrupted 返回 200 envelope |

**Resume 续跑：** `interrupt()` 返回值 = `human_response`，写入 messages 作为该 tool 的 result，然后沿现有边进入 `reflect`。同一 run 允许多次 interrupt。

**Prompt：** system/tool description 说明仅在不确定、需确认、缺关键事实时调用。

### 5.2 Go

见 §4.4。崩溃恢复：重试同 step 时带同一 `thread_id` 调 `/v1/run`；V1 不自动重试。

### 5.3 SQLite

- 单实例文件；Compose volume 挂载。
- V1 无 TTL 清理。

## 6. Error handling

| Scenario | Behavior |
|----------|----------|
| `request_human_input` 缺 `question` | tool `ok:false`，**不** interrupt |
| Resume，thread 不存在 | `404` |
| Resume，thread 未 paused | `409` |
| Resume 后再 interrupt | 再次 interrupted；Go 保持 `awaiting_agent_input` |
| Resume 后完成 | step → `ok`；继续后续 agent / 交易流 |
| SQLite 不可写 | `503` fail closed |
| 已完成 thread 再 `/v1/run` | `409`（除非显式 `force_new`，默认关） |
| Go resume 时 run 非 `awaiting_agent_input` | `409` |
| Agent HITL vs trade approval | 阶段互斥；HITL 在 proposals 之前 |

## 7. Testing

1. Python：invoke → interrupt → resume → 合法 result；多次 interrupt；未知 thread；缺 question。
2. Python：不调用 human tool 时回归（与现 plan_loop 行为一致）。
3. Go：mock interrupted → 状态正确；`agent-resume` 后继续 chain。
4. 可选：`LLM_MODE=mock` 脚本触发 human tool。

## 8. Rollout notes

- 部署需为 `agent-runtime` 配置 SQLite 路径与 volume。
- 现有无 HITL 的 mock/e2e 应无需改断言（除非严格校验 envelope 无额外字段——interrupted 路径单独测）。
- Contracts：更新 `agent_run_request`（`thread_id`）；新增 interrupted / resume 相关 schema 或文档化 envelope（与现有 trace envelope 风格一致）。
