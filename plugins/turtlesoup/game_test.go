package turtlesoup

import (
	"encoding/json"
	"testing"
)

func TestPuzzleAndVerdictContract(t *testing.T) {
	for _, p := range Defaults {
		if err := Validate(p); err != nil {
			t.Fatal(p.ID, err)
		}
	}
	p := Defaults[0]
	for _, bad := range []Verdict{{Intent: "guess", Answer: "yes", Solved: true, Facts: []string{"f1"}}, {Intent: "guess", Answer: "yes", Solved: true, Facts: []string{"missing"}}, {Intent: "unrelated", Answer: "yes", Facts: []string{"f1"}}, {Intent: "question", Answer: "fabricated"}} {
		if ValidateVerdict(bad, p) == nil {
			t.Fatalf("接受了无效裁定 %+v", bad)
		}
	}
	good := Verdict{Intent: "guess", Answer: "yes", Solved: true, Facts: p.Required}
	if ValidateVerdict(good, p) != nil || !Solved(good, p) {
		t.Fatal("完整推理未通关")
	}
	for _, raw := range []string{`{"intent":"question","answer":"no","extra":"泄题"}`, `{} {}`, `{"intent":"question"}多余正文`} {
		var v Verdict
		if Decode(raw, &v) == nil {
			t.Fatal("接受了未知字段或附加正文")
		}
	}
	raw, _ := json.Marshal(p)
	var decoded Puzzle
	if err := Decode("```json\n"+string(raw)+"\n```", &decoded); err != nil {
		t.Fatal(err)
	}
}
