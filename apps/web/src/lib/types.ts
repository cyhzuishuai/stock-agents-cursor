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
}

// Strategies
export type ExecutionMode = "auto_reject_breaches" | "require_approval";

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
