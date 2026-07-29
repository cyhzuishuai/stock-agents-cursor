"""Human-in-the-loop input request tool."""

from __future__ import annotations


def validate_human_input_args(args: dict) -> tuple[dict | None, str | None]:
    q = (args or {}).get("question")
    if not isinstance(q, str) or not q.strip():
        return None, "question_required"
    out: dict = {"question": q.strip()}
    if "context" in (args or {}) and isinstance(args["context"], dict):
        out["context"] = args["context"]
    if "options" in (args or {}) and isinstance(args["options"], list):
        out["options"] = [str(x) for x in args["options"]]
    return out, None


def request_human_input(ctx, **args):
    req, err = validate_human_input_args(args)
    if err:
        return {"ok": False, "error": err}
    return {"ok": True, "data": req, "interrupt": True}
