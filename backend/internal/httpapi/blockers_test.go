package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hackandchange/ai-buddy/backend/internal/llm"
)

type blockerState struct {
	Assignment *struct {
		TicketID  string `json:"ticket_id"`
		Resources []struct {
			ID, Kind, Title, URL string
		} `json:"resources"`
	} `json:"assignment"`
	Blocker *struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		HelperName string `json:"helper_name"`
	} `json:"blocker"`
	HelpRequests []struct {
		ID         int64  `json:"id"`
		InternName string `json:"intern_name"`
		Message    string `json:"message"`
	} `json:"help_requests"`
	Events []struct {
		Kind, Text string
	} `json:"events"`
	Chat []struct {
		Answer string `json:"answer"`
	} `json:"chat"`
	Error string `json:"error"`
}

func (e *env) blockers(method, path, cookie string, body any) (int, blockerState) {
	e.t.Helper()
	status, _, _ := e.do(method, path, cookie, body, nil)
	var st blockerState
	_ = json.Unmarshal(e.lastRaw, &st)
	return status, st
}

func (e *env) bstate(cookie string) blockerState {
	e.t.Helper()
	status, st := e.blockers("GET", "/api/state", cookie, nil)
	if status != 200 {
		e.t.Fatalf("state: %d %s", status, st.Error)
	}
	return st
}

func (e *env) say(cookie, message, id string) string {
	e.t.Helper()
	status, st := e.blockers("POST", "/api/chat", cookie, map[string]string{"message": message, "request_id": id})
	if status != 200 {
		e.t.Fatalf("%q: %d %s", message, status, st.Error)
	}
	return st.Chat[len(st.Chat)-1].Answer
}

func (e *env) page(path, cookie string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestFirstStepUsesInstructionAndTestResources(t *testing.T) {
	e := newProgressEnv(t)
	elena, alexey, daria, lead, mentor := e.login("intern_02"), e.login("intern_01"), e.login("intern_04"),
		e.login("lead_01"), e.login("mentor_01")
	e.pick(elena)
	e.pick(alexey)

	res := e.bstate(elena).Assignment.Resources
	if len(res) != 3 || res[0].URL != "/demo/conf-search-p95" || res[1].Kind != "grafana" {
		t.Fatalf("resources = %+v", res)
	}
	start := e.say(elena, "С чего начать?", "start")
	mustContain(t, start, "Первый шаг:", "/demo/grafana-search-filters", "«p95 latency, ms»",
		"один и тот же набор запросов filters-smoke-200", "без кэша / с кэшем", "к BACK-112 в Jira")
	metric := e.say(elena, "Где посмотреть эту метрику?", "metric")
	mustContain(t, metric, "тестовый дашборд Grafana", "p95")
	if len(e.bstate(mentor).HelpRequests) != 0 || e.bstate(elena).Blocker != nil {
		t.Error("questions about the task never reach the mentor")
	}

	tests := e.say(alexey, "С чего начать?", "start")
	mustContain(t, tests, "/demo/bitbucket-search-filters", "tests/filters/", "empty_result.json", "make test-filters")
	mustNotContain(t, tests, "Grafana")

	for _, c := range []struct {
		path, cookie string
		want         int
		text         string
	}{
		{"/demo/grafana-search-filters", elena, 200, "Тестовый ресурс · синтетические данные"},
		{"/demo/conf-search-p95", lead, 200, "filters-smoke-200"},
		{"/demo/conf-search-p95", mentor, 200, "Проверка:"},
		{"/demo/grafana-search-filters", daria, 403, "нет допуска"},
		{"/demo/grafana-search-filters", "", 401, "Выберите тестовый профиль"},
		{"/demo/unknown", elena, 404, "не найден"},
	} {
		status, body := e.page(c.path, c.cookie)
		if status != c.want || !strings.Contains(body, c.text) {
			t.Errorf("%s: %d %.80s", c.path, status, body)
		}
	}
}

func TestMissingInstructionIsAdmitted(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.exec(`DELETE FROM kb_docs WHERE ticket_id = 'BACK-112'`)
	answer := e.say(elena, "С чего начать?", "start")
	mustContain(t, answer, "нет доступной тебе инструкции по BACK-112", "спроси наставника")
	mustNotContain(t, answer, "/demo/", "Grafana")
	if res := e.bstate(elena).Assignment.Resources; len(res) != 0 {
		t.Errorf("resources = %+v", res)
	}
	// Without an instruction the clarifying question still offers a safe self-check.
	mustContain(t, e.say(elena, "Не могу запустить второй замер", "b1"), "перечитай описание задачи в Jira")
}

func TestBlockerReachesMentorOnceAndCanBeClosed(t *testing.T) {
	e := newProgressEnv(t)
	elena, alexey, lead, mentor := e.login("intern_02"), e.login("intern_01"), e.login("lead_01"), e.login("mentor_01")
	e.pick(elena)
	e.move(elena, "BACK-112", "in_progress")

	clarify := e.say(elena, "Не могу запустить второй замер", "b1")
	mustContain(t, clarify, "Уточни, что именно мешает", "флаг search.filters.cache включён")
	if st := e.bstate(elena); st.Blocker == nil || st.Blocker.Status != "clarifying" {
		t.Fatalf("blocker = %+v", st.Blocker)
	}
	if len(e.bstate(mentor).HelpRequests) != 0 {
		t.Fatal("nothing is sent before the clarification")
	}

	opened := e.say(elena, "Для второго замера нужен доступ к тестовому стенду, сам не справлюсь", "b2")
	mustContain(t, opened, "Записал блокер по BACK-112 и передал его наставнику — Олег Павлович Смирнов",
		"Просьба к наставнику: Выдай доступ search_stand", "Что можно сделать сейчас: Пока ждёшь доступ", "«Блокер снят»")
	requests := e.bstate(mentor).HelpRequests
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	mustContain(t, requests[0].Message, "Стажёр: Елена Игоревна Орлова · стажёр E1 · команда «Поиск»",
		"Задача: BACK-112 «Проверить кэширование фильтров поиска» · статус «В работе» · срок 19 ноября, чт",
		"Препятствие: Не могу запустить второй замер. Для второго замера нужен доступ",
		"Просьба: Выдай доступ search_stand к тестовому стенду поиска")
	if len(e.bstate(lead).HelpRequests) != 0 {
		t.Error("the lead is not the addressee when a mentor exists")
	}
	if st := e.bstate(elena); st.Blocker == nil || st.Blocker.Status != "open" || st.Blocker.HelperName != "Олег Павлович Смирнов" {
		t.Fatalf("open blocker = %+v", st.Blocker)
	}

	again := e.say(elena, "Не могу запустить второй замер", "b3")
	mustContain(t, again, "уже передан", "Повторно писать не нужно")
	if len(e.bstate(mentor).HelpRequests) != 1 {
		t.Fatal("a repeat does not notify twice")
	}

	id := e.bstate(elena).Blocker.ID
	if status, _ := e.blockers("POST", "/api/chat", mentor, map[string]string{"message": "Привет", "request_id": "m"}); status != 403 {
		t.Error("mentor does not chat with the bot")
	}
	if status, _ := e.blockers("POST", "/api/blocker/close", alexey, map[string]any{"blocker_id": id}); status != 403 {
		t.Error("another intern cannot close it")
	}
	if status, _ := e.blockers("POST", "/api/blocker/close", alexey, map[string]any{"blocker_id": "x"}); status != 400 {
		t.Error("bad id")
	}
	if status, st := e.blockers("POST", "/api/blocker/close", mentor, map[string]any{"blocker_id": id}); status != 200 ||
		len(st.HelpRequests) != 0 {
		t.Fatalf("mentor closes: %d %+v", status, st.HelpRequests)
	}
	st := e.bstate(elena)
	if st.Blocker != nil || st.Events[len(st.Events)-1].Text == "" || st.Events[len(st.Events)-1].Kind != "blocker" {
		t.Fatalf("after close: %+v", st)
	}
	mustContain(t, st.Events[len(st.Events)-1].Text, "Блокер по BACK-112 снят")
	events := len(st.Events)
	if status, _ := e.blockers("POST", "/api/blocker/close", elena, map[string]any{"blocker_id": id}); status != 200 ||
		len(e.bstate(elena).Events) != events {
		t.Error("closing twice changes nothing")
	}
	// A new problem after closing starts a new clarification.
	mustContain(t, e.say(elena, "Опять не могу запустить замер", "b4"), "Уточни, что именно мешает")
}

func TestSelfSolvableProblemDoesNotCallMentor(t *testing.T) {
	e := newProgressEnv(t)
	elena, mentor := e.login("intern_02"), e.login("mentor_01")
	e.pick(elena)
	e.say(elena, "Не могу запустить второй замер", "b1")
	answer := e.say(elena, "Кажется, флаг кэша не включается", "b2")
	mustContain(t, answer, "наставника не зову", "/demo/stand-search-flags", "filters-smoke-200")
	if len(e.bstate(mentor).HelpRequests) != 0 || e.bstate(elena).Blocker != nil {
		t.Error("self help closes the clarification without a message")
	}
}

func TestBlockerGoesToLeadWithoutMentorAndRestartDropsIt(t *testing.T) {
	e := newProgressEnv(t)
	elena, lead, mentor := e.login("intern_02"), e.login("lead_01"), e.login("mentor_01")
	e.pick(elena)
	e.exec(`UPDATE profiles SET mentor_id = NULL WHERE id = 'intern_02'`)
	e.say(elena, "Не могу запустить второй замер", "b1")
	e.say(elena, "Нужен наставник, нет доступа к стенду", "b2")
	if len(e.bstate(lead).HelpRequests) != 1 || len(e.bstate(mentor).HelpRequests) != 0 {
		t.Fatal("the lead is the fallback addressee")
	}
	if status, _ := e.blockers("POST", "/api/pick/reset", elena, map[string]any{}); status != 200 {
		t.Fatal("restart pick")
	}
	if len(e.bstate(lead).HelpRequests) != 0 {
		t.Error("restart pick removes the run's blockers")
	}
	if status, _ := e.blockers("POST", "/api/pick/reset", mentor, map[string]any{}); status != 403 {
		t.Error("mentor cannot restart a pick")
	}
	if status, _ := e.blockers("POST", "/api/chat/reset", mentor, map[string]any{}); status != 200 {
		t.Error("mentor chat reset is harmless")
	}
}

func TestBlockerAssessmentFailureKeepsClarification(t *testing.T) {
	e := newProgressEnv(t)
	elena, mentor := e.login("intern_02"), e.login("mentor_01")
	e.pick(elena)
	e.say(elena, "Не могу запустить второй замер", "b1")
	e.model.assess = func(llm.BlockerCase) (llm.Assessment, error) { return llm.Assessment{}, llm.ErrModel }
	if status, _ := e.blockers("POST", "/api/chat", elena, map[string]string{"message": "Нет доступа", "request_id": "b2"}); status != 502 {
		t.Fatal("model failure is reported")
	}
	if st := e.bstate(elena); st.Blocker == nil || st.Blocker.Status != "clarifying" || len(e.bstate(mentor).HelpRequests) != 0 {
		t.Fatal("nothing is sent after a failure")
	}
	var seen llm.BlockerCase
	e.model.assess = func(c llm.BlockerCase) (llm.Assessment, error) {
		seen = c
		return llm.Assessment{NeedsHuman: true, SelfStep: "Подожди.", Request: "Помоги со стендом."}, nil
	}
	e.say(elena, "Нет доступа", "b2")
	if seen.Obstacle != "Не могу запустить второй замер" || seen.Check == "" || len(seen.Fixes) != 3 || seen.Assignment.TicketID != "BACK-112" {
		t.Errorf("model context = %+v", seen)
	}
	if len(e.bstate(mentor).HelpRequests) != 1 {
		t.Error("retry after failure works")
	}
	// The blocker changed between read and write: the late answer is rejected.
	e.say(elena, "С чего начать?", "q")
	e.app.AfterModel = func() { e.exec(`UPDATE blockers SET status = 'closed'`) }
	if status, _ := e.blockers("POST", "/api/chat", elena, map[string]string{"message": "Не могу запустить замер", "request_id": "b3"}); status != 409 {
		t.Error("stale blocker state is detected")
	}
}

func TestModelErrorsTellWhatToDo(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	cases := map[string]error{
		"OpenRouter не принял ключ":        fmt.Errorf("%w: %w", llm.ErrModel, llm.ErrModelAuth),
		"перегружена запросами":            fmt.Errorf("%w: %w", llm.ErrModel, llm.ErrModelBusy),
		"Модель не ответила вовремя":       fmt.Errorf("%w: %w", llm.ErrModel, llm.ErrModelTimeout),
		"Не удалось определить, что нужно": llm.ErrModel,
	}
	for want, err := range cases {
		e.model.classify = func(string, bool, []llm.Turn) (llm.Intent, error) { return "", err }
		status, st := e.blockers("POST", "/api/chat", elena, map[string]string{"message": "Подбери задачу", "request_id": "x"})
		if status != 502 || !strings.Contains(st.Error, want) {
			t.Errorf("%s: %d %q", want, status, st.Error)
		}
	}
	if len(e.bstate(elena).Chat) != 0 {
		t.Error("nothing is stored")
	}
}

func TestQuestionIsNotTakenForOpenBlocker(t *testing.T) {
	e := newProgressEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.say(elena, "Не могу запустить второй замер", "b1")
	e.say(elena, "Нет доступа к стенду", "b2")
	// The model reads the history and wrongly calls a plain question a blocker.
	e.model.classify = func(string, bool, []llm.Turn) (llm.Intent, error) { return llm.IntentBlocker, nil }
	answer := e.say(elena, "С чего начать?", "q1")
	mustContain(t, answer, "Первый шаг:", "/demo/grafana-search-filters")
	mustNotContain(t, answer, "уже передан")
	// A real repeat of the obstacle still gets the short «already sent» reply.
	mustContain(t, e.say(elena, "Не могу запустить второй замер", "b3"), "уже передан")
}
