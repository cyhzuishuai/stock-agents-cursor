import { formatStartedAt } from "@/lib/datetime";
import type { AgentTraceEvent } from "@/lib/types";

function asString(value: unknown): string | null {
  if (typeof value === "string" && value.trim()) return value;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  if (typeof value === "boolean") return value ? "true" : "false";
  return null;
}

function planSummary(event: AgentTraceEvent): string {
  const current = asString(event.current_step_id);
  const plan = event.plan;
  const count = Array.isArray(plan) ? plan.length : null;
  const parts: string[] = [];
  if (count !== null) parts.push(`${count} step${count === 1 ? "" : "s"}`);
  if (current) parts.push(`current ${current}`);
  const source = asString(event.source);
  if (source) parts.push(source);
  return parts.join(" · ") || "plan updated";
}

function eventSummary(event: AgentTraceEvent): string {
  switch (event.type) {
    case "plan":
      return planSummary(event);
    case "step_start":
      return asString(event.step_id) ? `step ${asString(event.step_id)}` : "step start";
    case "llm": {
      const model = asString(event.model) ?? "model";
      const phase = asString(event.phase);
      const fallback = event.fallback_used ? "fallback" : null;
      return [phase, model, fallback].filter(Boolean).join(" · ");
    }
    case "tool": {
      const name = asString(event.name) ?? "tool";
      const status = event.ok === false ? "failed" : event.ok === true ? "ok" : null;
      const latency =
        typeof event.latency_ms === "number" ? `${event.latency_ms} ms` : null;
      return [name, status, latency].filter(Boolean).join(" · ");
    }
    case "reflect": {
      const decision = asString(event.decision) ?? "reflect";
      const reason = asString(event.reason);
      return reason ? `${decision} · ${reason}` : decision;
    }
    case "handoff":
      return asString(event.handoff_preview) ?? "handoff recorded";
    case "finalize": {
      const stop = asString(event.stop_reason);
      const ok =
        event.ok === true ? "ok" : event.ok === false ? "failed" : null;
      return [ok, stop].filter(Boolean).join(" · ") || "finalize";
    }
    default: {
      const name = asString(event.name);
      const decision = asString(event.decision);
      return name ?? decision ?? asString(event.type) ?? "event";
    }
  }
}

export function AgentTimeline({ events }: { events: AgentTraceEvent[] }) {
  if (!events.length) return null;

  return (
    <ol className="runs__timeline runs__agent-timeline" aria-label="Agent timeline">
      {events.map((event, index) => {
        const type = asString(event.type) ?? "event";
        return (
          <li
            key={`${type}-${event.at ?? index}-${index}`}
            className="runs__timeline-item runs__agent-timeline-item"
          >
            <span className="runs__event-type" data-type={type}>
              {type}
            </span>
            <time className="runs__event-at" dateTime={asString(event.at) ?? undefined}>
              {formatStartedAt(asString(event.at))}
            </time>
            <span className="runs__event-summary">{eventSummary(event)}</span>
          </li>
        );
      })}
    </ol>
  );
}
