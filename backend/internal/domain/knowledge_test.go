package domain

import "testing"

func TestCanRead(t *testing.T) {
	doc := Doc{ID: "d", Requires: []string{"search_dev"}}
	intern := Profile{Role: RoleIntern, Team: "Поиск", Grants: []string{"search_dev"}}
	if !CanRead(doc, intern, "Поиск") || CanRead(doc, intern, "Платежи") {
		t.Error("intern reads own team docs with grants")
	}
	if CanRead(doc, Profile{Role: RoleIntern, Team: "Поиск"}, "Поиск") {
		t.Error("missing grant")
	}
	if CanRead(Doc{ID: "x"}, intern, "Поиск") {
		t.Error("doc without requirements is closed for interns")
	}
	if !CanRead(doc, Profile{Role: RoleMentor, Team: "Поиск"}, "Поиск") || CanRead(doc, Profile{Role: RoleLead, Team: "Платежи"}, "Поиск") {
		t.Error("staff read their team docs")
	}
	if doc.URL() != "/demo/d" {
		t.Error("url")
	}
}

func TestInstructionAndRules(t *testing.T) {
	if _, ok := Instruction([]Doc{{Kind: "grafana"}}); ok {
		t.Error("no instruction")
	}
	if d, ok := Instruction([]Doc{{Kind: "grafana"}, {ID: "c", Kind: KindConfluence}}); !ok || d.ID != "c" {
		t.Error("instruction found")
	}
	for _, m := range []string{"Не могу запустить второй замер", "Тест падает", "Нет доступа к стенду", "Застряла",
		"Не получается", "Не открывается дашборд", "Ошибка в наборе", "Это блокер"} {
		if !ReportsBlocker(m) {
			t.Errorf("%q is a blocker", m)
		}
	}
	for _, m := range []string{"С чего начать?", "Где метрика?"} {
		if ReportsBlocker(m) {
			t.Errorf("%q is not a blocker", m)
		}
	}
	for _, m := range []string{"Нужен наставник", "Без помощи никак", "Нужна помощь", "Помоги", "Сама не справлюсь", "Не справлюсь"} {
		if !AsksForHuman(m) {
			t.Errorf("%q asks for a human", m)
		}
	}
	if AsksForHuman("Флаг выключен") {
		t.Error("asks for human")
	}
	fixes := []Fix{{Match: []string{"доступ"}, NeedsHuman: true}, {Match: []string{"Флаг"}, SelfStep: "включи"}}
	if f, ok := MatchFix(fixes, "флаг не включается"); !ok || f.SelfStep != "включи" {
		t.Error("case-insensitive match")
	}
	if _, ok := MatchFix(fixes, "что-то другое"); ok {
		t.Error("no match")
	}
}
