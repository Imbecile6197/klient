package rules

import (
	"net/mail"
	"testing"
)

func TestMatching(t *testing.T) {
	m := Message{From: &mail.Address{Name: "Alza.cz", Address: "info@alza.cz"}, To: []*mail.Address{{Address: "ja@proton.me"}}, Subject: "Vaše objednávka"}
	rs := []Rule{
		{Field: FieldFrom, Contains: "ALZA", Action: ActionMove, Target: "shop", Enabled: true},
		{Field: FieldSubject, Contains: "objednávka", Action: ActionMove, Target: "other", Enabled: true},
		{Field: FieldSubject, Contains: "objednávka", Action: ActionStar, Enabled: true},
		{Field: FieldTo, Contains: "ja@proton", Action: ActionRead, Enabled: false},
		{Field: FieldFrom, Contains: "", Action: ActionRead, Enabled: true},
	}
	got := Matching(rs, m)
	if len(got) != 2 || got[0].Target != "shop" || got[1].Action != ActionStar {
		t.Errorf("got %+v", got)
	}
}
