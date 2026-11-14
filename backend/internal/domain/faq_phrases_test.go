package domain_test

import (
	"testing"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/seed"
)

// Free wording of frequent questions must reach the right entry of the real base
// without a model, and questions about the task must not.
func TestFAQUnderstandsFreeWording(t *testing.T) {
	d, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	for msg, want := range map[string]string{
		"как оформить пул реквест":             "code_review",
		"Как оформить пул-реквест?":            "code_review",
		"как оформить пулреквест":              "code_review",
		"как оформить пул рекуест":             "code_review",
		"Как сделать PR?":                      "code_review",
		"как отправить merge request":          "code_review",
		"кто будет ревьюером моего кода":       "code_review",
		"когда у меня встреча с руководителем": "one_on_one",
		"будет ли 1 на 1 с тимлидом":           "one_on_one",
		"Когда 1:1?":        "one_on_one",
		"какие у меня цели": "goals",
		"что от меня ждут на испытательном сроке?": "goals",
		"как получить доступ к графане":            "access",
		"Во сколько дейлик?":                       "standup",
		"во сколько стэндап":                       "standup",
		"какой график работы":                      "schedule",
		"можно уйти пораньше сегодня?":             "schedule",
		"я заболела, что делать":                   "sick_leave",
		"мне плохо, температура":                   "sick_leave",
		"можно отпроситься в пятницу?":             "vacation",
		"хочу взять отгул":                         "vacation",
		"хочу в ОТПУСК":                            "vacation",
		"можно работать удалённо?":                 "remote",
		"можно работать из дома":                   "remote",
		"где документацыя команды":                 "docs",
		"где вики команды":                         "docs",
		"кому писать если есть вопрос":             "whom_to_ask",
		"как меня будут оценивать":                 "evaluation",
		"дадут ли оффер после стажировки":          "evaluation",
		"что от меня хотят на стажировке":          "goals",
		"есть ли регулярные встречи с лидом":       "one_on_one",
		"у меня нет доступа к jira":                "access",
		"во сколько общий созвон команды":          "standup",
		"сколько часов надо работать":              "schedule",
		"простыл, не смогу выйти":                  "sick_leave",
		"кто смотрит мой код":                      "code_review",
		"обязательно ли ходить в офис":             "remote",
	} {
		f, ok := domain.MatchFAQ(d.FAQ, msg)
		if !ok || f.ID != want {
			t.Errorf("%q -> %q, want %q", msg, f.ID, want)
		}
	}
	for _, msg := range []string{
		"С чего начать?", "Где посмотреть эту метрику?", "Как отправить задачу на ревью?",
		"Как перевести задачу в работу?", "На каких стендах делать замер?", "Какой срок у задачи?",
		"Какой набор запросов взять?", "Куда приложить результат в Jira?", "Где тестовый репозиторий?",
		"Подбери мне стартовую задачу", "Привет", "Не могу запустить второй замер", "Флаг кэша не включается",
	} {
		if f, ok := domain.MatchFAQ(d.FAQ, msg); ok {
			t.Errorf("%q must not match the FAQ, got %q", msg, f.ID)
		}
	}
}
