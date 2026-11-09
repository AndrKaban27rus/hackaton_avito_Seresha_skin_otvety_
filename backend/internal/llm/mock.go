package llm

import (
	"context"
	"strings"
)

type Mock struct{}

func NewMock() *Mock { return &Mock{} }
func (m *Mock) Ask(_ context.Context, q string) (Answer, error) {
	q = strings.ToLower(q)
	if strings.Contains(q, "отгул") || strings.Contains(q, "отпуск") {
		return Answer{Text: "Я не вижу в демо подтверждённого порядка оформления отгула. Уточни процедуру у наставника; перед обращением можно подготовить даты и краткое описание причины.", Sources: []Source{{Title: "Тестовая инструкция: вопросы к наставнику", URL: "demo://handbook/mentor-questions"}}}, nil
	}
	if strings.Contains(q, "инструмент") || strings.Contains(q, "доступ") {
		return Answer{Text: "Начни со списка инструментов команды и проверь, какие из них нужны для первой задачи. В демо я не запрашиваю реальные доступы; если нужного сервиса нет, уточни у наставника.", Sources: []Source{{Title: "Чек-лист стажёра: рабочие инструменты", URL: "demo://handbook/tools"}}}, nil
	}
	return Answer{Text: "В доступных тестовых инструкциях я не нашёл подтверждённого ответа. Лучше уточнить это у наставника команды.", Sources: []Source{}}, nil
}
