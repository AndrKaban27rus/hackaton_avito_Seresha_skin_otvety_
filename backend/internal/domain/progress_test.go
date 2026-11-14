package domain

import (
	"testing"
	"time"
)

func TestStatusWorkflow(t *testing.T) {
	if StatusName(StatusInProgress) != "В работе" || StatusName("done") != "done" {
		t.Error("status names")
	}
	if !KnownStatus(StatusReview) || KnownStatus("done") {
		t.Error("known statuses")
	}
	for from, want := range map[string]string{StatusAssigned: StatusInProgress, StatusInProgress: StatusReview} {
		if got, ok := NextStatus(from); !ok || got != want {
			t.Errorf("%s -> %s", from, got)
		}
	}
	if _, ok := NextStatus(StatusReview); ok {
		t.Error("review is final")
	}
}

func TestDemoDates(t *testing.T) {
	// 21:30 UTC is already the next day in Moscow.
	late := time.Date(2026, 11, 16, 21, 30, 0, 0, time.UTC)
	if got := Today(late, 0).String(); got != "2026-11-17" {
		t.Errorf("today = %s", got)
	}
	if got := Today(late, 2).String(); got != "2026-11-19" {
		t.Errorf("offset = %s", got)
	}
	d := DayOf(2026, 11, 19)
	if d.Label() != "19 ноября, чт" || !DayFromTime(d.Time()).Equal(d) {
		t.Errorf("label = %s", d.Label())
	}
	if DueDate(DayOf(2026, 11, 16), 0).String() != "2026-11-19" || DueDate(DayOf(2026, 11, 16), 4).String() != "2026-11-20" {
		t.Error("due date")
	}
	if DayOf(2026, 11, 16).DaysUntil(d) != 3 || !DayOf(2026, 11, 16).Before(d) {
		t.Error("days until")
	}
}

func TestReminderFor(t *testing.T) {
	due := DayOf(2026, 11, 19)
	cases := []struct {
		status string
		today  Day
		kind   string
	}{
		{StatusAssigned, due.Add(-2), ""},
		{StatusAssigned, due.Add(-1), ReminderDueSoon},
		{StatusInProgress, due, ReminderDueSoon},
		{StatusInProgress, due.Add(1), ReminderOverdue},
		{StatusAssigned, due.Add(5), ReminderOverdue},
		{StatusReview, due.Add(-1), ""},
		{StatusReview, due.Add(3), ""},
	}
	for _, c := range cases {
		kind, ok := ReminderFor(c.status, due, c.today)
		if kind != c.kind || ok != (c.kind != "") {
			t.Errorf("%s on %s: %q", c.status, c.today, kind)
		}
	}
}

func TestAsksAboutProgress(t *testing.T) {
	for _, m := range []string{"Как перевести задачу в работу?", "Какой срок?", "Когда ревью?", "Какой статус?"} {
		if !AsksAboutProgress(m) {
			t.Errorf("%q", m)
		}
	}
	for _, m := range []string{"С чего начать?", "Где посмотреть эту метрику?"} {
		if AsksAboutProgress(m) {
			t.Errorf("%q", m)
		}
	}
}
