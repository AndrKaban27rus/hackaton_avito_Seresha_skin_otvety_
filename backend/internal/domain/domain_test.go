package domain

import "testing"

func intern() Profile {
	return Profile{ID: "i", Role: RoleIntern, Team: "Поиск", Grade: "E1", Grants: []string{"search_dev"}}
}

func ticket() Ticket {
	return Ticket{ID: "T-1", Team: "Поиск", Requires: []string{"search_dev"}, Labels: []string{"starter"},
		Status: "open", Classification: "internal", Grades: []string{"E1"}}
}

func TestPermittedChecksEveryRule(t *testing.T) {
	if !Permitted(ticket(), intern()) {
		t.Fatal("base ticket must be permitted")
	}
	cases := map[string]func(*Ticket, *Profile){
		"closed":         func(t *Ticket, _ *Profile) { t.Status = "closed" },
		"confidential":   func(t *Ticket, _ *Profile) { t.Classification = "confidential" },
		"no class":       func(t *Ticket, _ *Profile) { t.Classification = "" },
		"other team":     func(t *Ticket, _ *Profile) { t.Team = "Платежи" },
		"no starter":     func(t *Ticket, _ *Profile) { t.Labels = []string{"bug"} },
		"other grade":    func(t *Ticket, _ *Profile) { t.Grades = []string{"E2"} },
		"no grades":      func(t *Ticket, _ *Profile) { t.Grades = nil },
		"no requires":    func(t *Ticket, _ *Profile) { t.Requires = nil },
		"missing grant":  func(t *Ticket, _ *Profile) { t.Requires = []string{"search_dev", "profiles_ki"} },
		"lead":           func(_ *Ticket, p *Profile) { p.Role = RoleLead },
		"intern no perm": func(_ *Ticket, p *Profile) { p.Grants = nil },
	}
	for name, change := range cases {
		tk, p := ticket(), intern()
		change(&tk, &p)
		if Permitted(tk, p) {
			t.Errorf("%s: ticket must be excluded", name)
		}
	}
	empty := ticket()
	empty.Requires = []string{}
	if !Permitted(empty, intern()) {
		t.Error("explicitly empty requirements are allowed")
	}
}

func TestCandidatesAndNoMatchReason(t *testing.T) {
	a, b := ticket(), ticket()
	b.ID = "T-2"
	got := Candidates([]Ticket{a, b}, map[string]bool{"T-1": true}, intern())
	if len(got) != 1 || got[0].ID != "T-2" {
		t.Fatalf("candidates = %+v", got)
	}
	if WhyNoMatch([]Ticket{a, b}, intern()) != ReasonTaken {
		t.Error("permitted but taken tickets mean ReasonTaken")
	}
	noGrant := intern()
	noGrant.Grants = []string{}
	if WhyNoMatch([]Ticket{a, b}, noGrant) != ReasonNone {
		t.Error("without grants the reason is ReasonNone")
	}
}

func TestWantsTicket(t *testing.T) {
	yes := []string{"Подбери мне стартовую задачу", "Дай задание", "Что можно взять в работу?",
		"что мне взять в работу", "Хочу тикет", "Нужна задача"}
	// README documents that free wording and a bare «Да» need the model.
	no := []string{"Привет", "С чего начать?", "Где посмотреть эту метрику?", "задача", "нужно",
		"Есть что-нибудь для меня на сегодня?", "Да"}
	for _, m := range yes {
		if !WantsTicket(m) {
			t.Errorf("%q must ask for a ticket", m)
		}
	}
	for _, m := range no {
		if WantsTicket(m) {
			t.Errorf("%q must not ask for a ticket", m)
		}
	}
}
