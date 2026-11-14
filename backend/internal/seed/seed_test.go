package seed

import "testing"

func TestLoadKeepsProfileOrderAndTicketFields(t *testing.T) {
	d, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range d.Profiles {
		ids = append(ids, p.ID)
	}
	want := []string{"intern_01", "lead_01", "mentor_01", "intern_02", "intern_03", "intern_04"}
	if len(ids) != len(want) {
		t.Fatalf("profiles = %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("profiles = %v", ids)
		}
	}
	if d.Profiles[1].Grade != "" || d.Profiles[1].Grants == nil {
		t.Error("lead has empty grade and non-nil grants")
	}
	if len(d.Profiles[5].Grants) != 0 || d.Profiles[3].MentorID != "mentor_01" || len(d.Docs) != 5 {
		t.Error("intern_04 has no grants, interns have a mentor, the knowledge base is loaded")
	}
	byID := map[string]int{}
	for i, tk := range d.Tickets {
		byID[tk.ID] = i
	}
	if d.Tickets[byID["BACK-121"]].Classification != "" {
		t.Error("BACK-121 has no classification")
	}
	if d.Tickets[byID["BACK-112"]].OKRPriority != 2 || d.TeamGoal == "" || len(d.Channels) != 2 {
		t.Error("ticket priority, goal and channels are loaded")
	}
}

func TestFAQHasTenOrMoreCompleteEntries(t *testing.T) {
	d, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.FAQ) < 10 {
		t.Fatalf("faq has %d entries, want at least 10", len(d.FAQ))
	}
	seen := map[string]bool{}
	for _, f := range d.FAQ {
		if f.ID == "" || seen[f.ID] || f.Team != "Поиск" || f.Topic == "" || f.Question == "" || f.Answer == "" || len(f.Match) == 0 {
			t.Errorf("incomplete or duplicate entry %+v", f)
		}
		seen[f.ID] = true
	}
}

func TestParseRejectsBrokenData(t *testing.T) {
	for _, raw := range []string{`{`, `{"profiles": []}`, `{"profiles": {"a": 1}}`} {
		if _, err := parse([]byte(raw)); err == nil {
			t.Errorf("%s must fail", raw)
		}
	}
}
