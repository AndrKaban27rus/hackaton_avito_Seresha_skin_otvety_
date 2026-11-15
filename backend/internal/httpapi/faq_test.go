package httpapi

import (
	"slices"
	"testing"

	"github.com/hackandchange/ai-buddy/backend/internal/buddy"
	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/llm"
)

func TestFAQAnswersWithoutTheLead(t *testing.T) {
	e := newProgressEnv(t)
	daria, elena, lead, mentor := e.login("intern_04"), e.login("intern_02"), e.login("lead_01"), e.login("mentor_01")

	// Without a task and without grants the intern still gets answers from the base.
	answer := e.say(daria, "Когда у меня 1:1 с руководителем?", "faq-1")
	mustContain(t, answer, "Встреча 1:1 с руководителем — раз в неделю", buddy.FAQSource)
	if chat := e.state(daria).Chat; len(chat) != 1 || chat[0].Mode != buddy.ModeFAQ {
		t.Fatalf("chat = %+v", chat)
	}
	if status, st := e.send(daria, "Когда у меня 1:1 с руководителем?", "faq-1"); status != 200 || len(st.Chat) != 1 {
		t.Errorf("repeat with the same request ID: %d, %d turns", status, len(st.Chat))
	}
	if n := len(e.state(lead).Notifications); n != 0 {
		t.Errorf("the lead got %d reports for a frequent question", n)
	}

	// With a task, answers from the base leave the assignment and the lead alone.
	e.pick(elena)
	reports := len(e.state(lead).Notifications)
	mustContain(t, e.say(elena, "Как взять отгул?", "faq-2"), "минимум за 3 рабочих дня", buddy.FAQSource)
	mustContain(t, e.say(elena, "Как оформить pull request на код-ревью?", "faq-3"), "Первым ревьюером назначь наставника")
	if st := e.state(elena); st.Assignment == nil || st.Assignment.TicketID != "BACK-112" || st.Assignment.Status != "assigned" {
		t.Errorf("assignment = %+v", st.Assignment)
	}
	if n := len(e.state(lead).Notifications); n != reports {
		t.Errorf("lead reports changed from %d to %d", reports, n)
	}

	// Questions about the task and blockers keep their meaning.
	progress := e.say(elena, "Как отправить задачу на ревью?", "task-1")
	mustContain(t, progress, "статус «Назначена»", "кнопкой «Взять в работу»")
	mustNotContain(t, progress, buddy.FAQSource)
	blocker := e.say(elena, "Нет доступа к тестовому стенду, как запросить доступ?", "b1")
	mustContain(t, blocker, "Уточни, что именно мешает")
	mustNotContain(t, blocker, buddy.FAQSource)
	if len(e.bstate(mentor).HelpRequests) != 0 {
		t.Error("frequent questions and an unclarified blocker never reach the mentor")
	}
}

func TestFAQUnderstandsFreeWordingAndListsTopics(t *testing.T) {
	e := newEnv(t)
	alexey := e.login("intern_01")
	mustContain(t, e.say(alexey, "как оформить пул реквест", "w1"), "Первым ревьюером назначь наставника", buddy.FAQSource)
	mustContain(t, e.say(alexey, "можно поработать удалённо в пятницу?", "w2"), "гибридном формате", buddy.FAQSource)
	// A message the bot does not understand tells what it can answer.
	hello := e.say(alexey, "Привет!", "w3")
	mustContain(t, hello, "Могу подобрать стартовую задачу", "Ещё отвечаю на частые вопросы: встречи 1:1",
		"pull request и код-ревью", "удалённая работа.")
	e.pick(alexey)
	e.model.classify = func(string, bool, []llm.Turn) (llm.Intent, error) { return llm.IntentOther, nil }
	mustContain(t, e.say(alexey, "Спасибо", "w4"), "Могу помочь с текущей задачей", "Ещё отвечаю на частые вопросы: встречи 1:1")
}

func TestFAQAnswerComesFromTheDatabase(t *testing.T) {
	e := newEnv(t)
	alexey, lead := e.login("intern_01"), e.login("lead_01")
	e.exec(`UPDATE faq SET answer = 'Стендап перенесли на 12:00.' WHERE id = 'standup'`)
	mustContain(t, e.say(alexey, "Во сколько стендап?", "s1"), "Стендап перенесли на 12:00.\n"+buddy.FAQSource)

	// Another team's entry never reaches the model, and an ID outside the list gets no answer.
	e.exec(`UPDATE faq SET team = 'Платежи' WHERE id = 'vacation'`)
	var seen []domain.FAQ
	e.model.classifyFAQ = func(_ string, faq []domain.FAQ) (llm.Classification, error) {
		seen = faq
		return llm.Classification{Intent: llm.IntentFAQ, FAQID: "vacation"}, nil
	}
	got := e.say(alexey, "Как взять отпуск?", "s2")
	mustContain(t, got, buddy.MsgNoFAQ, "В базе есть ответы на темы: встречи 1:1, цели стажировки")
	mustNotContain(t, got, "отгул и отпуск", buddy.FAQSource)
	if len(seen) != 11 || slices.ContainsFunc(seen, func(f domain.FAQ) bool { return f.ID == "vacation" }) {
		t.Errorf("model saw %d entries: %+v", len(seen), seen)
	}

	// The model found a general question with no entry: an honest reply, nothing sent to the lead.
	e.model.classifyFAQ = func(string, []domain.FAQ) (llm.Classification, error) {
		return llm.Classification{Intent: llm.IntentFAQ}, nil
	}
	mustContain(t, e.say(alexey, "Какой дресс-код в офисе?", "s3"), buddy.MsgNoFAQ)
	chat := e.state(alexey).Chat
	if chat[len(chat)-1].Mode != "system" || len(e.state(lead).Notifications) != 0 {
		t.Errorf("mode = %s, reports = %d", chat[len(chat)-1].Mode, len(e.state(lead).Notifications))
	}
}

func TestTaskProgressQuestionWinsOverModelFAQChoice(t *testing.T) {
	e := newEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	// The real model picked the pull request entry for a question about the task status.
	e.model.classifyFAQ = func(string, []domain.FAQ) (llm.Classification, error) {
		return llm.Classification{Intent: llm.IntentFAQ, FAQID: "code_review"}, nil
	}
	progress := e.say(elena, "Как отправить задачу на ревью?", "task-1")
	mustContain(t, progress, "статус «Назначена»", "кнопкой «Взять в работу»")
	mustNotContain(t, progress, buddy.FAQSource)
	// A question the FAQ rules recognise stays a frequent question, even with «ревью» in it.
	mustContain(t, e.say(elena, "Как оформить pull request на код-ревью?", "faq-1"), "Первым ревьюером назначь наставника", buddy.FAQSource)
	// The model may also call it «other»: the task answer still wins.
	e.model.classifyFAQ = func(string, []domain.FAQ) (llm.Classification, error) {
		return llm.Classification{Intent: llm.IntentOther}, nil
	}
	mustContain(t, e.say(elena, "Какой у задачи срок?", "task-2"), "срок —", "кнопкой «Взять в работу»")
	e.model.classifyFAQ = func(string, []domain.FAQ) (llm.Classification, error) {
		return llm.Classification{Intent: llm.IntentFAQ, FAQID: "code_review"}, nil
	}
	// Without a task there is no task answer to prefer.
	alexey := e.login("intern_01")
	mustContain(t, e.say(alexey, "Как отправить задачу на ревью?", "faq-2"), buddy.FAQSource)
}

func TestModelAnswerCannotClaimFAQSource(t *testing.T) {
	e := newEnv(t)
	elena := e.login("intern_02")
	e.pick(elena)
	e.model.answer = func(string, llm.Assignment, []llm.Turn) (string, error) {
		return "Создай ветку от master.\n" + buddy.FAQSource, nil
	}
	answer := e.say(elena, "С чего начать?", "q1")
	mustContain(t, answer, "Создай ветку от master.")
	mustNotContain(t, answer, buddy.FAQSource)
	// A status question never reaches the model: the answer comes from Jira data.
	progress := e.say(elena, "Как отправить задачу на ревью?", "q2")
	mustContain(t, progress, "статус «Назначена»", "кнопкой «Взять в работу»")
	mustNotContain(t, progress, "Создай ветку")
}
