const STATUS_LABELS: Record<string, string> = {
  created: "Created",
  failed: "Failed",
  awaiting_approval: "Awaiting approval",
  executed: "Executed",
  cancelled: "Cancelled",
};

function statusClass(status: string): string {
  switch (status) {
    case "executed":
      return "run-status-badge run-status-badge--success";
    case "failed":
      return "run-status-badge run-status-badge--danger";
    case "awaiting_approval":
      return "run-status-badge run-status-badge--warning";
    case "cancelled":
      return "run-status-badge run-status-badge--muted";
    default:
      return "run-status-badge";
  }
}

export function RunStatusBadge({ status }: { status: string }) {
  const label = STATUS_LABELS[status] ?? status;
  return (
    <span className={statusClass(status)} data-status={status}>
      {label}
    </span>
  );
}
