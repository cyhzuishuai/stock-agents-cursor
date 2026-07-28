function asString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}

function thesisCount(handoff: Record<string, unknown>): number | null {
  const thesis = handoff.thesis_by_symbol;
  if (!thesis || typeof thesis !== "object" || Array.isArray(thesis)) return null;
  const count = Object.keys(thesis as Record<string, unknown>).length;
  return count > 0 ? count : null;
}

function openQuestions(handoff: Record<string, unknown>): string[] {
  const raw = handoff.open_questions;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item) => (typeof item === "string" ? item.trim() : ""))
    .filter(Boolean);
}

export function HandoffSummary({
  handoff,
}: {
  handoff?: Record<string, unknown> | null;
}) {
  if (!handoff || typeof handoff !== "object") return null;

  const symbols = thesisCount(handoff);
  const questions = openQuestions(handoff);
  const notes = asString(handoff.confidence_notes);

  if (symbols === null && questions.length === 0 && !notes) return null;

  return (
    <div className="runs__handoff" aria-label="Handoff summary">
      {symbols !== null ? (
        <p className="runs__handoff-meta">
          Thesis covering {symbols} symbol{symbols === 1 ? "" : "s"}
        </p>
      ) : null}
      {questions.length > 0 ? (
        <div className="runs__handoff-block">
          <p className="runs__handoff-label">Open questions</p>
          <ul className="runs__handoff-list">
            {questions.map((question) => (
              <li key={question}>{question}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {notes ? (
        <div className="runs__handoff-block">
          <p className="runs__handoff-label">Confidence notes</p>
          <p className="runs__handoff-notes">{notes}</p>
        </div>
      ) : null}
    </div>
  );
}
