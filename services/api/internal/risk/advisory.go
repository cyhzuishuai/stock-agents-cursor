package risk

// Annotate merges a non-binding risk-agent advisory into an authoritative rule decision.
//
// Policy: the Python risk agent is advisory only; Go rule evaluation is authoritative.
// suggestedAction ("auto" or "review") is persisted separately by the workflow for
// audit; this helper does not store it on Decision. Annotate never mutates
// AutoExecute or BreachReasons — in particular, suggested "review" must not force a
// breach when rules already pass (AutoExecute stays true).
func Annotate(d Decision, suggestedAction string) Decision {
	_ = suggestedAction
	return d
}
