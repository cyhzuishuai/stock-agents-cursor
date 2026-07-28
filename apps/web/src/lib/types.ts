/** Types mirrored from packages/contracts/api_dto.md */

// Auth
export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
}

export interface MeResponse {
  id: number;
  username: string;
}

// Overview
export interface LatestRunSummary {
  id: number;
  trade_date: string;
  status: string;
}

export interface PositionSummary {
  symbol: string;
  qty: number;
  market_value: number;
  weight: number;
}

export interface NavPoint {
  trade_date: string;
  nav: number;
}

export interface OverviewResponse {
  cash: number;
  equity: number;
  nav: number;
  pending_approvals_count: number;
  latest_run: LatestRunSummary | null;
  positions_summary: PositionSummary[];
  nav_series: NavPoint[];
}

// Portfolio
export interface PortfolioPosition {
  symbol: string;
  qty: number;
  avg_cost: number;
  stop_loss: number | null;
  take_profit: number | null;
  market_price: number;
  unrealized_pnl: number;
  weight: number;
}

export interface PortfolioResponse {
  cash: number;
  positions: PortfolioPosition[];
}

// Runs
export interface RunListItem {
  id: number;
  trade_date: string;
  status: string;
  created_at: string;
  strategy_id: number | null;
  strategy_name: string;
  trigger: string;
}

export interface WorkflowStep {
  id: number;
  run_id: number;
  step: string;
  status: string;
  payload_json: string;
}

export type AgentStopReason = "final" | "max_rounds" | "timeout" | "error";

export interface AgentToolCall {
  id?: string;
  name: string;
  args?: Record<string, unknown>;
}

export interface AgentToolResult {
  id?: string;
  name: string;
  ok: boolean;
  latency_ms?: number;
  result_preview?: string | null;
  error?: string | null;
}

export interface AgentTraceRound {
  i?: number;
  llm?: { model?: string; latency_ms?: number };
  assistant?: {
    content?: string | null;
    tool_calls?: AgentToolCall[];
  };
  tools?: AgentToolResult[];
}

export interface AgentTrace {
  agent: "analyst" | "portfolio";
  started_at?: string;
  ended_at?: string;
  rounds: AgentTraceRound[];
  stop_reason?: AgentStopReason;
  usage?: { prompt_tokens?: number; completion_tokens?: number };
}

export interface AnalystResultItem {
  symbol: string;
  bias: string;
  side: string;
  confidence?: number;
  thesis?: string;
  urgency?: string;
  rationale?: string;
}

export interface AnalystResult {
  items: AnalystResultItem[];
  warnings?: string[];
}

export interface PortfolioProposalResult {
  symbol: string;
  side: string;
  qty: number;
  estimated_notional?: number;
  [key: string]: unknown;
}

export interface PortfolioResult {
  proposals: PortfolioProposalResult[];
  warnings?: string[];
}

export interface AgentRunEnvelope {
  result: AnalystResult | PortfolioResult | Record<string, unknown>;
  trace: AgentTrace;
}

export interface TradeProposal {
  id: number;
  run_id: number;
  symbol: string;
  side: string;
  qty: number;
  [key: string]: unknown;
}

export interface Order {
  id: number;
  run_id: number;
  symbol: string;
  side: string;
  qty: number;
  [key: string]: unknown;
}

export interface RunDetail {
  id: number;
  trade_date: string;
  status: string;
  strategy_id: number | null;
  strategy_name: string;
  trigger: string;
  steps: WorkflowStep[];
  proposals: TradeProposal[];
  orders: Order[];
}

export interface EodRunRequest {
  trade_date?: string;
}

export interface EodRunResponse {
  run_id: number;
}

export interface OkResponse {
  ok: true;
}

// Approvals
export type ApprovalStatus = "pending" | "approved" | "rejected";

export interface ApprovalItem {
  id: number;
  proposal_id: number;
  symbol: string;
  side: string;
  qty: number;
  breach_reasons: string[];
  created_at: string;
}

export type ApprovalDecision = "approved" | "rejected";

export interface ApprovalDecideRequest {
  decision: ApprovalDecision;
  note?: string;
}

// Settings
export interface WatchlistItem {
  symbol: string;
  can_hold: boolean;
}

export interface SettingsResponse {
  watchlist: WatchlistItem[];
  risk_rules: Record<string, unknown>;
  market_data_provider: string;
}

export interface SymbolSearchResult {
  symbol: string;
  name: string;
  price?: number | null;
  change?: number | null;
  change_pct?: number | null;
  asset_class?: string;
}

// Strategies
export type ExecutionMode = "auto_reject_breaches" | "require_approval" | "bypass_risk";

export interface Strategy {
  id: number;
  name: string;
  description: string;
  is_system_default: boolean;
  is_active: boolean;
  pre_open_minutes: number;
  intraday_every_minutes: number;
  intraday_start_et: string;
  intraday_end_et: string;
  execution_mode: ExecutionMode;
}

export interface StrategyWriteBody {
  name: string;
  description: string;
  pre_open_minutes: number;
  intraday_every_minutes: number;
  intraday_start_et: string;
  intraday_end_et: string;
  execution_mode: ExecutionMode;
}
