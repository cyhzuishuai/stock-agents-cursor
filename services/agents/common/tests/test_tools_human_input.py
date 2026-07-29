from __future__ import annotations

from stock_agents_common.tools.human_input import validate_human_input_args


def test_missing_question():
    req, err = validate_human_input_args({})
    assert req is None and err == "question_required"


def test_valid_question():
    req, err = validate_human_input_args({"question": "Approve thesis?"})
    assert err is None and req["question"] == "Approve thesis?"
