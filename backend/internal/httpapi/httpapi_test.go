package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hackandchange/ai-buddy/backend/internal/buddy"
	"github.com/hackandchange/ai-buddy/backend/internal/devdb"
	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/llm"
	"github.com/hackandchange/ai-buddy/backend/internal/seed"
	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

// Tests use their own PostgreSQL: TEST_DATABASE_URL, or a local instance in a temp folder.
var testDBURL string

func TestMain(m *testing.M) {
	testDBURL = os.Getenv("TEST_DATABASE_URL")
	var pg *devdb.DB
	if testDBURL == "" {
		dir, err := os.MkdirTemp("", "ai-buddy-test-pg")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if pg, err = devdb.Start(dir, 0, io.Discard); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		testDBURL = pg.URL
		defer os.RemoveAll(dir)
	}
	code := m.Run()
	if pg != nil {
		_ = pg.Stop()
	}
	os.Exit(code)
}

// scripted wraps the fixture model and lets a test override any step.
type scripted struct {
	llm.Fixture
	mode     string
	classify func(string, bool, []llm.Turn) (llm.Intent, error)
	sel      func(string, []domain.Ticket, domain.Profile) (llm.Selection, error)
	answer   func(string, llm.Assignment, []llm.Turn) (string, error)
	assess   func(llm.BlockerCase) (llm.Assessment, error)
	mu       sync.Mutex
	selects  int
}

func (s *scripted) Mode() string {
	if s.mode != "" {
		return s.mode
	}
	return llm.ModeFixture
}

func (s *scripted) Classify(ctx context.Context, m string, has bool, h []llm.Turn) (llm.Intent, error) {
	if s.classify != nil {
		return s.classify(m, has, h)
	}
	return s.Fixture.Classify(ctx, m, has, h)
}

func (s *scripted) Select(ctx context.Context, q string, c []domain.Ticket, p domain.Profile, g string) (llm.Selection, error) {
	s.mu.Lock()
	s.selects++
	s.mu.Unlock()
	if s.sel != nil {
		return s.sel(q, c, p)
	}
	return s.Fixture.Select(ctx, q, c, p, g)
}

func (s *scripted) Answer(ctx context.Context, q string, a llm.Assignment, h []llm.Turn) (string, error) {
	if s.answer != nil {
		return s.answer(q, a, h)
	}
	return s.Fixture.Answer(ctx, q, a, h)
}

func (s *scripted) AssessBlocker(ctx context.Context, c llm.BlockerCase) (llm.Assessment, error) {
	if s.assess != nil {
		return s.assess(c)
	}
	return s.Fixture.AssessBlocker(ctx, c)
}

type env struct {
	t     *testing.T
	db    *store.Store
	app   *buddy.Service
	model *scripted
	srv   *httptest.Server
	// lastRaw is the body of the latest response, for tests that decode extra fields.
	lastRaw []byte
}

func mustSeed(t *testing.T) seed.Data {
	t.Helper()
	data, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, testDBURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	data, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, data); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, model: &scripted{}}
	e.app = buddy.New(db, e.model, data)
	e.srv = httptest.NewServer(New(e.app, slog.New(slog.NewTextHandler(io.Discard, nil)), ""))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

type state struct {
	Profile struct {
		ID, Role, Name string
	} `json:"profile"`
	Profiles []map[string]any `json:"profiles"`
	Chat     []struct {
		Question, Answer, Mode string
	} `json:"chat"`
	Notifications []struct {
		Message string `json:"message"`
	} `json:"notifications"`
	Assignment *struct {
		TicketID  string `json:"ticket_id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		FirstStep string `json:"first_step"`
	} `json:"assignment"`
	Mode  string `json:"mode"`
	Error string `json:"error"`
}

func (e *env) do(method, path, cookie string, body any, headers map[string]string) (int, state, string) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			reader = strings.NewReader(s)
		} else {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	e.lastRaw = raw
	var st state
	_ = json.Unmarshal(raw, &st)
	return resp.StatusCode, st, strings.SplitN(resp.Header.Get("Set-Cookie"), ";", 2)[0]
}

func (e *env) login(profile string) string {
	e.t.Helper()
	status, st, cookie := e.do("POST", "/api/session", "", map[string]string{"profile": profile}, nil)
	if status != 200 || cookie == "" || st.Profile.ID != profile {
		e.t.Fatalf("login %s: %d %+v", profile, status, st)
	}
	return cookie
}

func (e *env) send(cookie, message, requestID string) (int, state) {
	e.t.Helper()
	status, st, _ := e.do("POST", "/api/chat", cookie, map[string]string{"message": message, "request_id": requestID}, nil)
	return status, st
}

func (e *env) pick(cookie string) (int, state) {
	return e.send(cookie, "Подбери мне стартовую задачу", "request-1")
}

func (e *env) state(cookie string) state {
	e.t.Helper()
	status, st, _ := e.do("GET", "/api/state", cookie, nil, nil)
	if status != 200 {
		e.t.Fatalf("state: %d %s", status, st.Error)
	}
	return st
}

func mustContain(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Errorf("%q does not contain %q", text, p)
		}
	}
}

func mustNotContain(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(text, p) {
			t.Errorf("%q must not contain %q", text, p)
		}
	}
}

func TestAssignmentFollowupAndChatReset(t *testing.T) {
	e := newEnv(t)
	intern, lead := e.login("intern_01"), e.login("lead_01")
	status, st := e.pick(intern)
	if status != 200 || st.Assignment == nil || st.Assignment.TicketID != "BACK-112" || st.Assignment.Status != "assigned" {
		t.Fatalf("%d %+v", status, st.Assignment)
	}
	mustContain(t, st.Chat[0].Answer, "Назначена BACK-112", "Первый шаг:")
	if st.Chat[0].Mode != llm.ModeFixture || st.Mode != llm.ModeFixture {
		t.Error("fixture mode is visible")
	}

	if _, again := e.pick(intern); len(again.Chat) != 1 {
		t.Error("same request ID is answered once")
	}
	_, retry := e.send(intern, "Подбери мне стартовую задачу", "retry")
	mustContain(t, retry.Chat[1].Answer, "уже есть задача BACK-112")
	_, follow := e.send(intern, "С чего начать?", "followup")
	mustContain(t, follow.Chat[2].Answer, "Первый шаг: "+st.Assignment.FirstStep,
		"Тестовая инструкция Confluence: замер p95", "Grafana", "p95 latency", "filters-smoke-200", "к BACK-112 в Jira")
	reports := e.state(lead).Notifications
	if len(reports) != 1 {
		t.Fatalf("reports = %+v", reports)
	}
	mustContain(t, reports[0].Message, "Алексей Сергеевич Ким · стажёр E1 · команда «Поиск»", "Назначена BACK-112",
		"Почему:", "Проверено бэкендом", "классификация internal", "допуски: search_dev")
	if len(e.state(lead).Chat) != 0 || len(e.state(intern).Notifications) != 0 {
		t.Error("roles do not see each other's data")
	}
	if len(e.state(e.login("intern_01")).Chat) != 3 {
		t.Error("history survives a new session")
	}

	status, reset, _ := e.do("POST", "/api/chat/reset", intern, map[string]any{}, nil)
	if status != 200 || len(reset.Chat) != 0 || reset.Assignment == nil || reset.Assignment.TicketID != "BACK-112" {
		t.Fatalf("reset: %d %+v", status, reset)
	}
	if len(e.state(lead).Notifications) != 1 {
		t.Error("intern reset keeps the lead report")
	}
}

func TestLeadResetHidesOldReportsAndShowsNewOnes(t *testing.T) {
	e := newEnv(t)
	first, second, lead := e.login("intern_01"), e.login("intern_02"), e.login("lead_01")
	e.pick(first)
	status, reset, _ := e.do("POST", "/api/chat/reset", lead, map[string]any{}, nil)
	if status != 200 || len(reset.Notifications) != 0 || len(e.state(e.login("lead_01")).Notifications) != 0 {
		t.Fatal("lead reset hides reports")
	}
	if e.state(first).Assignment.TicketID != "BACK-112" {
		t.Error("assignment stays")
	}
	e.send(first, "Подбери мне стартовую задачу", "repeat")
	if len(e.state(lead).Notifications) != 0 {
		t.Error("repeat does not bring the old report back")
	}
	if _, st := e.pick(second); st.Assignment.TicketID != "BACK-118" {
		t.Fatal("second intern gets BACK-118")
	}
	reports := e.state(lead).Notifications
	if len(reports) != 1 {
		t.Fatalf("reports = %+v", reports)
	}
	mustContain(t, reports[0].Message, "Елена Игоревна Орлова")
}

func TestRestartPickReleasesOnlyOwnAssignment(t *testing.T) {
	e := newEnv(t)
	intern, second, lead := e.login("intern_01"), e.login("intern_02"), e.login("lead_01")
	e.pick(second)
	e.pick(intern)
	if status, _, _ := e.do("POST", "/api/pick/reset", lead, map[string]any{}, nil); status != 403 {
		t.Error("lead cannot restart a pick")
	}
	status, st, _ := e.do("POST", "/api/pick/reset", intern, map[string]any{}, nil)
	if status != 200 || st.Assignment != nil || len(st.Chat) != 0 {
		t.Fatalf("%d %+v", status, st)
	}
	if e.state(second).Assignment.TicketID != "BACK-112" {
		t.Error("other intern keeps the task")
	}
	reports := e.state(lead).Notifications
	if len(reports) != 1 || !strings.Contains(reports[0].Message, "Елена") {
		t.Errorf("only own reports are removed: %+v", reports)
	}
	_, st = e.send(intern, "Что можно взять в работу?", "new-pick")
	if st.Assignment.TicketID != "BACK-118" {
		t.Fatal("new pick")
	}
	if len(e.state(lead).Notifications) != 2 {
		t.Error("a fresh report is sent")
	}
}

func TestAllTakenIsNotExplainedByMissingRights(t *testing.T) {
	e := newEnv(t)
	elena, alexey, ilya, lead := e.login("intern_02"), e.login("intern_01"), e.login("intern_03"), e.login("lead_01")
	if _, st := e.pick(elena); st.Assignment.TicketID != "BACK-112" {
		t.Fatal("Elena gets BACK-112")
	}
	if _, st := e.pick(alexey); st.Assignment.TicketID != "BACK-118" {
		t.Fatal("Alexey gets BACK-118")
	}
	selects := e.model.selects
	status, st := e.pick(ilya)
	if status != 200 || st.Assignment != nil || st.Chat[0].Answer != buddy.MsgAllTaken {
		t.Fatalf("%d %+v", status, st)
	}
	mustNotContain(t, st.Chat[0].Answer, "допуск", "грейд")
	e.send(ilya, "Подбери мне стартовую задачу", "repeat")
	if e.model.selects != selects {
		t.Error("the model is not asked without candidates")
	}
	reports := e.state(lead).Notifications
	if len(reports) != 3 {
		t.Fatalf("one no-match report: %+v", reports)
	}
	mustContain(t, reports[2].Message, "Илья Андреевич Соколов · стажёр E1 · команда «Поиск»", "уже назначены")
	all, _ := json.Marshal([]any{st, reports[2]})
	mustNotContain(t, string(all), "BACK-104", "BACK-121", "BACK-130", "profiles_ki")
}

func TestProfileWithoutGrantGetsNothingOnEmptyBase(t *testing.T) {
	e := newEnv(t)
	daria, lead := e.login("intern_04"), e.login("lead_01")
	status, st := e.pick(daria)
	if status != 200 || st.Assignment != nil || st.Chat[0].Answer != buddy.MsgNoneForProfile {
		t.Fatalf("%d %+v", status, st)
	}
	if e.model.selects != 0 {
		t.Error("the model never sees tickets the intern may not take")
	}
	report := e.state(lead).Notifications[0].Message
	mustContain(t, report, "Дарья Николаевна Белова", "нет стартовых")
	all, _ := json.Marshal([]any{st, report})
	mustNotContain(t, string(all), "BACK-112", "BACK-118", "BACK-104", "search_dev")
}

func TestModelRoutesNaturalRequestsBeforeSearchingJira(t *testing.T) {
	e := newEnv(t)
	e.model.mode = llm.ModeOpenRouter
	intern, lead := e.login("intern_01"), e.login("lead_01")
	routes := []llm.Intent{llm.IntentOther, llm.IntentPickTask, llm.IntentTaskQuestion}
	var histories [][]llm.Turn
	e.model.classify = func(_ string, _ bool, h []llm.Turn) (llm.Intent, error) {
		histories = append(histories, h)
		next := routes[0]
		routes = routes[1:]
		return next, nil
	}
	var offered []string
	e.model.sel = func(_ string, c []domain.Ticket, _ domain.Profile) (llm.Selection, error) {
		for _, t := range c {
			offered = append(offered, t.ID)
		}
		return llm.Selection{TicketID: "BACK-118", Rationale: "Подходит.", FirstStep: "Открой стенд."}, nil
	}
	e.model.answer = func(q string, a llm.Assignment, h []llm.Turn) (string, error) {
		if q != "С чего начать?" || a.TicketID != "BACK-118" || len(h) != 2 {
			t.Errorf("answer context: %q %+v %d", q, a, len(h))
		}
		return "Открой тестовый стенд.", nil
	}

	_, st := e.send(intern, "Привет", "hello")
	if st.Assignment != nil || e.model.selects != 0 {
		t.Fatal("greeting does not search Jira")
	}
	_, st = e.send(intern, "Есть что-нибудь для меня на сегодня?", "pick")
	if st.Assignment == nil || st.Assignment.TicketID != "BACK-118" || st.Chat[1].Mode != llm.ModeOpenRouter {
		t.Fatalf("pick: %+v", st)
	}
	if strings.Join(offered, ",") != "BACK-112,BACK-118" {
		t.Errorf("offered = %v", offered)
	}
	if len(histories[1]) != 1 || histories[1][0].Question != "Привет" {
		t.Errorf("classifier sees previous turns: %+v", histories[1])
	}
	_, st = e.send(intern, "С чего начать?", "followup")
	if st.Chat[2].Answer != "Открой тестовый стенд." || e.model.selects != 1 {
		t.Error("follow-up uses the current task")
	}
	if len(e.state(lead).Notifications) != 1 {
		t.Error("one report")
	}
}

func TestModelAndJiraErrorsLeaveNoResult(t *testing.T) {
	e := newEnv(t)
	intern, lead := e.login("intern_01"), e.login("lead_01")
	empty := func() {
		t.Helper()
		st := e.state(intern)
		if st.Assignment != nil || len(st.Chat) != 0 || len(e.state(lead).Notifications) != 0 {
			t.Fatalf("partial result: %+v", st)
		}
	}
	e.model.classify = func(string, bool, []llm.Turn) (llm.Intent, error) { return "", llm.ErrModel }
	if status, st := e.pick(intern); status != 502 || !strings.Contains(st.Error, "Повторите") {
		t.Fatalf("classify failure: %d", status)
	}
	empty()
	e.model.classify = nil
	e.model.sel = func(string, []domain.Ticket, domain.Profile) (llm.Selection, error) {
		return llm.Selection{}, context.DeadlineExceeded
	}
	if status, _ := e.pick(intern); status != 502 {
		t.Fatal("select timeout is a model error")
	}
	empty()
	e.model.sel = func(string, []domain.Ticket, domain.Profile) (llm.Selection, error) {
		return llm.Selection{TicketID: "BACK-104", Rationale: "x", FirstStep: "y"}, nil
	}
	if status, _ := e.pick(intern); status != 409 {
		t.Fatal("ID outside candidates is rejected")
	}
	empty()
	e.model.sel = nil

	e.exec(`CREATE FUNCTION jira_down() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'Jira unavailable'; END $$`)
	e.exec(`CREATE TRIGGER jira_down BEFORE INSERT ON jira_assignments FOR EACH ROW EXECUTE FUNCTION jira_down()`)
	status, st := e.pick(intern)
	if status != 503 || !strings.Contains(st.Error, "Повторите запрос") {
		t.Fatalf("Jira failure: %d %s", status, st.Error)
	}
	empty()
	e.exec(`DROP TRIGGER jira_down ON jira_assignments`)
	if status, _ := e.pick(intern); status != 200 {
		t.Fatal("retry after recovery works")
	}

	_, _ = e.send(intern, "С чего начать?", "q1")
	e.model.answer = func(string, llm.Assignment, []llm.Turn) (string, error) { return "", llm.ErrModel }
	if status, _ := e.send(intern, "А что дальше?", "q2"); status != 502 {
		t.Error("answer failure is a model error")
	}
	if len(e.state(intern).Chat) != 2 {
		t.Error("failed answer is not stored")
	}
}

func TestConcurrentRetryDoesNotDuplicate(t *testing.T) {
	e := newEnv(t)
	intern, lead := e.login("intern_01"), e.login("lead_01")
	var wg sync.WaitGroup
	gate := make(chan struct{})
	arrived := make(chan struct{}, 2)
	e.model.sel = func(q string, c []domain.Ticket, p domain.Profile) (llm.Selection, error) {
		arrived <- struct{}{}
		<-gate
		return llm.Fixture{}.Select(context.Background(), q, c, p, "")
	}
	codes := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = e.pick(intern)
		}()
	}
	<-arrived
	<-arrived
	close(gate)
	wg.Wait()
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("codes = %v", codes)
	}
	if len(e.state(intern).Chat) != 1 || len(e.state(lead).Notifications) != 1 {
		t.Error("one turn and one report")
	}
}

func TestConcurrentInternsCannotShareTicket(t *testing.T) {
	e := newEnv(t)
	first, second, lead := e.login("intern_01"), e.login("intern_02"), e.login("lead_01")
	gate := make(chan struct{})
	arrived := make(chan struct{}, 2)
	e.model.sel = func(string, []domain.Ticket, domain.Profile) (llm.Selection, error) {
		arrived <- struct{}{}
		<-gate
		return llm.Selection{TicketID: "BACK-112", Rationale: "r", FirstStep: "s"}, nil
	}
	var wg sync.WaitGroup
	codes := map[string]int{}
	var mu sync.Mutex
	for _, c := range []string{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := e.pick(c)
			mu.Lock()
			codes[c] = code
			mu.Unlock()
		}()
	}
	<-arrived
	<-arrived
	close(gate)
	wg.Wait()
	if !(codes[first] == 200 && codes[second] == 409 || codes[first] == 409 && codes[second] == 200) {
		t.Fatalf("codes = %v", codes)
	}
	loser := first
	if codes[first] == 200 {
		loser = second
	}
	e.model.sel = nil
	if _, st := e.send(loser, "Подбери мне стартовую задачу", "again"); st.Assignment.TicketID != "BACK-118" {
		t.Error("retry picks the remaining ticket")
	}
	if len(e.state(lead).Notifications) != 2 {
		t.Error("two reports")
	}
}

func TestRechecksAvailabilityAtAssignment(t *testing.T) {
	e := newEnv(t)
	intern, lead := e.login("intern_01"), e.login("lead_01")
	e.app.AfterModel = func() { e.exec(`UPDATE jira_tickets SET status = 'closed' WHERE id = 'BACK-112'`) }
	if status, _ := e.pick(intern); status != 409 {
		t.Fatal("closed ticket is not assigned")
	}
	if e.state(intern).Assignment != nil || len(e.state(lead).Notifications) != 0 {
		t.Error("no result")
	}
	// The intern now asks again: the list changed between read and write.
	e.app.AfterModel = func() { e.exec(`UPDATE jira_tickets SET status = 'open'`) }
	e.exec(`UPDATE jira_tickets SET status = 'closed'`)
	if status, _ := e.pick(intern); status != 409 {
		t.Fatal("no-match is re-checked before reporting")
	}
}

func TestResetDuringPendingRequestWins(t *testing.T) {
	e := newEnv(t)
	intern := e.login("intern_01")
	e.send(intern, "Привет", "hello")
	e.app.AfterModel = func() {
		if err := e.app.ResetChat(context.Background(), "intern_01", true); err != nil {
			t.Error(err)
		}
	}
	if status, _ := e.pick(intern); status != 409 {
		t.Fatal("late answer is rejected")
	}
	if st := e.state(intern); len(st.Chat) != 0 || st.Assignment != nil {
		t.Error("reset state is kept")
	}
	// A different turn written meanwhile is also detected.
	e.app.AfterModel = func() {
		e.exec(`INSERT INTO chat_turns (user_id, request_id, question, answer, mode) VALUES ('intern_01', 'x', 'q', 'a', 'system')`)
	}
	if status, _ := e.send(intern, "Привет", "hello-2"); status != 409 {
		t.Fatal("concurrent turn is detected")
	}
}

func TestRestrictedAssignmentHidesDetails(t *testing.T) {
	e := newEnv(t)
	intern := e.login("intern_01")
	e.pick(intern)
	e.exec(`UPDATE jira_tickets SET classification = 'confidential' WHERE id = 'BACK-112'`)
	st := e.state(intern)
	if st.Assignment == nil || st.Assignment.Status != buddy.StatusRestricted || st.Assignment.TicketID != "" {
		t.Fatalf("%+v", st.Assignment)
	}
	_, st = e.send(intern, "С чего начать?", "q")
	if st.Chat[1].Answer != buddy.MsgRestricted {
		t.Error("restricted reply")
	}
}

func TestMissingLeadIsAServiceError(t *testing.T) {
	e := newEnv(t)
	intern := e.login("intern_01")
	e.exec(`UPDATE profiles SET lead_id = 'intern_02' WHERE id = 'intern_01'`)
	if status, st := e.pick(intern); status != 503 || !strings.Contains(st.Error, "Руководитель") {
		t.Fatalf("%d %s", status, st.Error)
	}
}

func TestSessionAndRequestValidation(t *testing.T) {
	e := newEnv(t)
	if status, _, _ := e.do("GET", "/healthz", "", nil, nil); status != 200 {
		t.Error("healthz")
	}
	if status, _, _ := e.do("GET", "/api/state", "", nil, nil); status != 401 {
		t.Error("no session")
	}
	if status, _, _ := e.do("GET", "/api/state", "demo_session=forged", nil, nil); status != 401 {
		t.Error("unknown session")
	}
	if status, _, _ := e.do("POST", "/api/chat", "", map[string]string{"message": "x", "request_id": "1"}, nil); status != 401 {
		t.Error("chat without session")
	}
	if status, _, _ := e.do("POST", "/api/session", "", map[string]string{"profile": "ceo"}, nil); status != 400 {
		t.Error("unknown profile")
	}
	if status, _, _ := e.do("POST", "/api/session", "", map[string]string{}, nil); status != 400 {
		t.Error("missing profile")
	}
	lead := e.login("lead_01")
	if status, _ := e.pick(lead); status != 403 {
		t.Error("lead cannot ask for a task")
	}
	intern := e.login("intern_01")
	checks := []struct {
		name    string
		path    string
		body    any
		headers map[string]string
		want    int
	}{
		{"bad request id", "/api/chat", map[string]string{"message": "x", "request_id": "bad id"}, nil, 400},
		{"empty message", "/api/chat", map[string]string{"message": "  ", "request_id": "a"}, nil, 400},
		{"long message", "/api/chat", map[string]string{"message": strings.Repeat("я", 1201), "request_id": "a"}, nil, 400},
		{"foreign origin", "/api/chat", map[string]string{}, map[string]string{"Origin": "http://example.test"}, 403},
		{"content type", "/api/chat", map[string]string{}, map[string]string{"Content-Type": "text/plain"}, 415},
		{"not an object", "/api/chat", "[1]", nil, 400},
		{"broken json", "/api/chat", "{", nil, 400},
		{"too large", "/api/chat", `{"message":"` + strings.Repeat("a", 9000) + `"}`, nil, 400},
		{"unknown action", "/api/unknown", map[string]string{}, nil, 404},
	}
	for _, c := range checks {
		if status, _, _ := e.do("POST", c.path, intern, c.body, c.headers); status != c.want {
			t.Errorf("%s: %d, want %d", c.name, status, c.want)
		}
	}
	if status, _, _ := e.do("POST", "/api/chat", intern, map[string]string{"message": "Привет", "request_id": "same"},
		map[string]string{"Origin": e.srv.URL}); status != 200 {
		t.Error("same origin is allowed")
	}
	if status, _ := e.send(intern, "Другой текст", "same"); status != 409 {
		t.Error("reused request ID with new text")
	}
	// Logging in again from the same browser drops the old session.
	status, _, fresh := e.do("POST", "/api/session", intern, map[string]string{"profile": "intern_02"}, nil)
	if status != 200 || fresh == intern {
		t.Fatal("relogin")
	}
	if status, _, _ := e.do("GET", "/api/state", intern, nil, nil); status != 401 {
		t.Error("old session is removed")
	}
}

func TestStateSurvivesRestartAndProfilesAreListed(t *testing.T) {
	e := newEnv(t)
	intern := e.login("intern_01")
	e.pick(intern)
	before := e.state(intern)
	data, _ := seed.Load()
	if err := e.db.Migrate(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(New(buddy.New(e.db, llm.Fixture{}, data), slog.New(slog.NewTextHandler(io.Discard, nil)), ""))
	defer restarted.Close()
	e.srv = restarted
	after := e.state(intern)
	if after.Assignment == nil || after.Assignment.TicketID != before.Assignment.TicketID || len(after.Chat) != 1 {
		t.Fatalf("after restart: %+v", after)
	}
	if len(after.Profiles) != 6 {
		t.Errorf("profiles = %v", after.Profiles)
	}
}

func TestServesBuiltFrontend(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o600)
	_ = os.MkdirAll(filepath.Join(dir, "assets"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o600)
	srv := httptest.NewServer(New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), dir))
	defer srv.Close()
	for path, want := range map[string]string{"/": "app", "/some/route": "app", "/assets/app.js": "console"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(raw), want) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s -> %s", path, raw)
		}
	}
}

func TestStoreOpenFailsOnBadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := store.Open(ctx, "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1"); err == nil {
		t.Error("open must fail")
	}
	if _, err := store.Open(ctx, "::bad"); err == nil {
		t.Error("parse must fail")
	}
}
