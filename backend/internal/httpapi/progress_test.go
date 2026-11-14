package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Monday 16 November 2026, 10:00 in Moscow.
var monday = time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)

type progressState struct {
	Assignment *struct {
		TicketID   string `json:"ticket_id"`
		Status     string `json:"status"`
		StatusName string `json:"status_name"`
		DueDate    string `json:"due_date"`
		DueLabel   string `json:"due_label"`
		DaysLeft   int    `json:"days_left"`
		Overdue    bool   `json:"overdue"`
		NextStep   string `json:"next_step"`
		NextStatus string `json:"next_status"`
		Action     string `json:"action"`
	} `json:"assignment"`
	Events []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"events"`
	Team []struct {
		Name     string `json:"name"`
		TicketID string `json:"ticket_id"`
		Status   string `json:"status"`
		DueLabel string `json:"due_label"`
		Overdue  bool   `json:"overdue"`
	} `json:"team"`
	Clock struct {
		Today      string `json:"today"`
		Label      string `json:"label"`
		OffsetDays int    `json:"offset_days"`
	} `json:"clock"`
	Chat []struct {
		Answer string `json:"answer"`
	} `json:"chat"`
	Notifications []struct {
		Message string `json:"message"`
	} `json:"notifications"`
	Error string `json:"error"`
}

func newProgressEnv(t *testing.T) *env {
	e := newEnv(t)
	e.app.Now = func() time.Time { return monday }
	return e
}

// call posts to the API and decodes the progress fields of the state.
func (e *env) call(method, path, cookie string, body any) (int, progressState) {
	e.t.Helper()
	status, _, _ := e.do(method, path, cookie, body, nil)
	var st progressState
	_ = json.Unmarshal(e.lastRaw, &st)
	return status, st
}

func (e *env) progress(cookie string) progressState {
	e.t.Helper()
	status, st := e.call("GET", "/api/state", cookie, nil)
	if status != 200 {
		e.t.Fatalf("state: %d %s", status, st.Error)
	}
	return st
}

func (e *env) move(cookie, ticket, to string) (int, progressState) {
	return e.call("POST", "/api/task/status", cookie, map[string]string{"ticket_id": ticket, "status": to})
}

func (e *env) nextDay(cookie string) progressState {
	e.t.Helper()
	status, st := e.call("POST", "/api/clock", cookie, map[string]string{"action": "next_day"})
	if status != 200 {
		e.t.Fatalf("next day: %d %s", status, st.Error)
	}
	return st
}

func (e *env) reminders() int {
	e.t.Helper()
	n, err := e.app.RunReminders(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestStatusTransitionsAndDeadline(t *testing.T) {
	e := newProgressEnv(t)
	elena, alexey, daria, lead := e.login("intern_02"), e.login("intern_01"), e.login("intern_04"), e.login("lead_01")
	e.pick(elena)
	st := e.progress(elena)
	a := st.Assignment
	if a.DueDate != "2026-11-19" || a.DueLabel != "19 ноября, чт" || a.DaysLeft != 3 || a.StatusName != "Назначена" ||
		a.NextStatus != "in_progress" || a.Action != "Взять в работу" || !strings.Contains(a.NextStep, "замерь p95") {
		t.Fatalf("assignment = %+v", a)
	}
	mustContain(t, st.Chat[0].Answer, "Срок: 19 ноября, чт", "Статус в Jira: «Назначена»", "Взять в работу")
	mustContain(t, e.progress(lead).Notifications[0].Message, "Срок: 19 ноября, чт")
	if st.Clock.Today != "2026-11-16" || st.Clock.Label != "16 ноября, пн" {
		t.Errorf("clock = %+v", st.Clock)
	}

	// A question about the status never changes it.
	_, s := e.send(elena, "Как перевести задачу в работу?", "how")
	mustContain(t, s.Chat[1].Answer, "статус «Назначена»", "кнопкой «Взять в работу»", "19 ноября")
	if e.progress(elena).Assignment.Status != "assigned" {
		t.Fatal("question changed the status")
	}

	checks := []struct {
		who, ticket, to string
		want            int
	}{
		{lead, "BACK-112", "in_progress", 403},
		{alexey, "BACK-112", "in_progress", 403},
		{daria, "BACK-112", "in_progress", 403},
		{elena, "BACK-118", "in_progress", 403},
		{elena, "BACK-112", "done", 400},
		{elena, "BACK-112", "review", 409},
	}
	for _, c := range checks {
		if status, st := e.move(c.who, c.ticket, c.to); status != c.want {
			t.Errorf("%s %s -> %s: %d %s", c.who, c.ticket, c.to, status, st.Error)
		}
	}
	if st := e.progress(elena); st.Assignment.Status != "assigned" || len(st.Events) != 0 {
		t.Fatal("rejected transitions leave no trace")
	}

	status, st := e.move(elena, "BACK-112", "in_progress")
	if status != 200 || st.Assignment.Status != "in_progress" || st.Assignment.Action != "Отправить на ревью" || len(st.Events) != 1 {
		t.Fatalf("%d %+v", status, st)
	}
	mustContain(t, st.Events[0].Text, "Jira: BACK-112 переведена в «В работе»", "Отправить на ревью")
	if status, st := e.move(elena, "BACK-112", "in_progress"); status != 200 || len(st.Events) != 1 {
		t.Error("repeated confirmation is a no-op")
	}
	if status, _ := e.move(elena, "BACK-112", "assigned"); status != 409 {
		t.Error("no way back")
	}
	if status, st := e.move(elena, "BACK-112", "review"); status != 200 || st.Assignment.Status != "review" ||
		st.Assignment.Action != "" || len(st.Events) != 2 {
		t.Fatalf("review: %d %+v", status, st.Assignment)
	}
	_, s = e.send(elena, "Подбери мне стартовую задачу", "again")
	mustContain(t, s.Chat[2].Answer, "Статус: «На ревью»", "Дождись комментариев")
}

func TestTransitionFailureInJiraLeavesNoConfirmation(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.exec(`CREATE FUNCTION jira_down() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'Jira unavailable'; END $$`)
	e.exec(`CREATE TRIGGER jira_down BEFORE UPDATE ON jira_assignments FOR EACH ROW EXECUTE FUNCTION jira_down()`)
	if status, st := e.move(elena, "BACK-112", "in_progress"); status != 503 || !strings.Contains(st.Error, "Повторите") {
		t.Fatalf("%d %s", status, st.Error)
	}
	if st := e.progress(elena); st.Assignment.Status != "assigned" || len(st.Events) != 0 {
		t.Fatal("no false confirmation")
	}
	e.exec(`DROP TRIGGER jira_down ON jira_assignments`)
	if status, _ := e.move(elena, "BACK-112", "in_progress"); status != 200 {
		t.Fatal("retry works")
	}
}

func TestRemindersFollowDemoClockOnce(t *testing.T) {
	e := newProgressEnv(t)
	elena, lead := e.login("intern_02"), e.login("lead_01")
	e.pick(elena)
	if e.reminders() != 0 {
		t.Fatal("no reminder three days ahead")
	}
	e.nextDay(lead) // 17th: two days left
	if len(e.progress(elena).Events) != 0 {
		t.Fatal("too early")
	}
	st := e.nextDay(elena) // 18th: the day before
	if st.Clock.OffsetDays != 2 || len(st.Events) != 1 || st.Events[0].Kind != "due_soon" {
		t.Fatalf("due soon: %+v", st)
	}
	mustContain(t, st.Events[0].Text, "Напоминание: завтра, 19 ноября, чт, срок по BACK-112", "Статус в Jira: «Назначена»", "Следующий шаг")
	if e.reminders() != 0 || len(e.progress(e.login("intern_02")).Events) != 1 {
		t.Fatal("re-check and relogin do not resend")
	}
	if len(e.progress(lead).Events) != 0 {
		t.Error("the lead does not get intern reminders")
	}
	st = e.nextDay(elena) // 19th: deadline day, same reminder already sent
	if len(st.Events) != 1 || st.Assignment.DaysLeft != 0 || st.Assignment.Overdue {
		t.Fatalf("deadline day: %+v", st.Assignment)
	}
	st = e.nextDay(elena) // 20th: overdue
	if len(st.Events) != 2 || st.Events[1].Kind != "overdue" || !st.Assignment.Overdue {
		t.Fatalf("overdue: %+v", st)
	}
	mustContain(t, st.Events[1].Text, "Срок по BACK-112", "прошёл 19 ноября, чт", "что мешает")
	e.nextDay(elena)
	if len(e.progress(elena).Events) != 2 {
		t.Error("overdue is sent once")
	}
	team := e.progress(lead).Team
	if team[0].Name != "Алексей Сергеевич Ким" || team[0].Status != "none" || team[1].TicketID != "BACK-112" || !team[1].Overdue {
		t.Errorf("team = %+v", team)
	}

	if status, st := e.call("POST", "/api/clock", lead, map[string]string{"action": "reset"}); status != 200 || st.Clock.OffsetDays != 0 {
		t.Error("reset clock")
	}
	if status, _ := e.call("POST", "/api/clock", lead, map[string]string{"action": "jump"}); status != 400 {
		t.Error("unknown clock action")
	}
	e.exec(`UPDATE demo_clock SET offset_days = 60`)
	if status, _ := e.call("POST", "/api/clock", lead, map[string]string{"action": "next_day"}); status != 409 {
		t.Error("clock limit")
	}
}

func TestLateReminderStartsWithOverdue(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.exec(`UPDATE demo_clock SET offset_days = 5`)
	if e.reminders() != 1 {
		t.Fatal("one reminder")
	}
	if ev := e.progress(elena).Events; len(ev) != 1 || ev[0].Kind != "overdue" {
		t.Errorf("a stale «due soon» is not sent: %+v", ev)
	}
}

func TestRemindersRespectReviewMovedDeadlineAndLostAccess(t *testing.T) {
	e := newProgressEnv(t)
	elena, alexey := e.login("intern_02"), e.login("intern_01")
	e.pick(elena)
	e.pick(alexey)
	e.move(elena, "BACK-112", "in_progress")
	e.move(elena, "BACK-112", "review")
	e.exec(`UPDATE jira_assignments SET due_date = DATE '2026-11-25' WHERE ticket_id = 'BACK-118'`)
	e.exec(`UPDATE demo_clock SET offset_days = 3`) // 19th
	if e.reminders() != 0 {
		t.Fatal("review and moved deadline need no reminder")
	}
	e.exec(`UPDATE demo_clock SET offset_days = 8`) // 24th: day before the new deadline
	if e.reminders() != 1 {
		t.Fatal("new deadline is checked")
	}
	mustContain(t, e.progress(alexey).Events[0].Text, "25 ноября")
	if len(e.progress(elena).Events) != 2 {
		t.Error("only status confirmations for the reviewed task")
	}

	e.exec(`UPDATE jira_tickets SET classification = 'confidential' WHERE id = 'BACK-118'`)
	e.exec(`UPDATE demo_clock SET offset_days = 12`)
	if e.reminders() != 0 {
		t.Error("no reminder after access is lost")
	}
	if status, _ := e.move(alexey, "BACK-118", "in_progress"); status != 403 {
		t.Error("restricted task cannot move")
	}
}

func TestResetsHideOrDropProgressMessages(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.move(elena, "BACK-112", "in_progress")
	e.exec(`UPDATE demo_clock SET offset_days = 2`)
	e.reminders()
	if len(e.progress(elena).Events) != 2 {
		t.Fatal("confirmation and reminder")
	}
	if status, st := e.call("POST", "/api/chat/reset", elena, map[string]any{}); status != 200 || len(st.Events) != 0 ||
		st.Assignment.Status != "in_progress" {
		t.Fatal("chat reset hides messages and keeps the status")
	}
	if e.reminders() != 0 || len(e.progress(elena).Events) != 0 {
		t.Fatal("hidden reminders are not resent")
	}
	if status, st := e.call("POST", "/api/pick/reset", elena, map[string]any{}); status != 200 || st.Assignment != nil {
		t.Fatal("restart pick")
	}
	if e.reminders() != 0 {
		t.Error("no reminders without an assignment")
	}
	e.pick(elena)
	if st := e.progress(elena); st.Assignment.Status != "assigned" || st.Assignment.DueDate != "2026-11-21" {
		t.Errorf("new run starts fresh on the demo date: %+v", st.Assignment)
	}
}

func TestMigrationUpgradesStageOneAssignments(t *testing.T) {
	e := newProgressEnv(t)
	e.exec(`ALTER TABLE jira_assignments DROP COLUMN due_date, DROP COLUMN status_changed_at,
		DROP CONSTRAINT jira_assignments_status_check`)
	e.exec(`ALTER TABLE jira_assignments ADD CONSTRAINT jira_assignments_status_check CHECK (status IN ('assigned'))`)
	e.exec(`INSERT INTO jira_assignments (intern, ticket_id, rationale, first_step, mode, created_at)
		VALUES ('intern_02', 'BACK-112', 'r', 's', 'fixture', '2026-11-16T07:00:00Z')`)
	if err := e.db.Migrate(context.Background(), mustSeed(t)); err != nil {
		t.Fatal(err)
	}
	elena := e.login("intern_02")
	if st := e.progress(elena); st.Assignment.DueDate != "2026-11-19" {
		t.Fatalf("backfilled due date: %+v", st.Assignment)
	}
	if status, _ := e.move(elena, "BACK-112", "in_progress"); status != 200 {
		t.Error("new statuses are allowed after the upgrade")
	}
}

func TestPilotMetrics(t *testing.T) {
	e := newProgressEnv(t)
	elena, alexey := e.login("intern_02"), e.login("intern_01")
	e.pick(elena)
	e.pick(alexey)
	e.say(elena, "С чего начать?", "q1")
	e.move(elena, "BACK-112", "in_progress")
	e.say(elena, "Не могу запустить второй замер", "b1")
	e.say(elena, "Нет доступа к стенду, нужен наставник", "b2")
	e.say(alexey, "Тест падает", "b1")
	e.say(alexey, "make не запускается", "b2")
	e.move(elena, "BACK-112", "review")

	rows, err := e.db.Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].InternID != "intern_01" || rows[1].InternID != "intern_02" {
		t.Fatalf("rows = %+v", rows)
	}
	alexeyRow, elenaRow := rows[0], rows[1]
	if elenaRow.StartedAt == nil || elenaRow.ReviewAt == nil || elenaRow.StartedAt.Before(elenaRow.AssignedAt) {
		t.Errorf("elena timeline = %+v", elenaRow)
	}
	if elenaRow.Questions != 4 || elenaRow.Escalated != 1 || elenaRow.SelfSolved != 0 {
		t.Errorf("elena counts = %+v", elenaRow)
	}
	if alexeyRow.StartedAt != nil || alexeyRow.Questions != 3 || alexeyRow.Escalated != 0 || alexeyRow.SelfSolved != 1 {
		t.Errorf("alexey = %+v", alexeyRow)
	}
}
