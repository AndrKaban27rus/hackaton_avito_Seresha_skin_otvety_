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

func TestMatchFAQ(t *testing.T) {
	entries := []FAQ{{ID: "standup", Match: []string{"Стендап"}}, {ID: "schedule", Match: []string{"график", "стендап"}}}
	if f, ok := MatchFAQ(entries, "Во сколько СТЕНДАП?"); !ok || f.ID != "standup" {
		t.Errorf("case-insensitive, first entry wins: %+v", f)
	}
	if f, ok := MatchFAQ(entries, "Какой график?"); !ok || f.ID != "schedule" {
		t.Errorf("any keyword: %+v", f)
	}
	if _, ok := MatchFAQ(entries, "С чего начать?"); ok {
		t.Error("no match")
	}
	if _, ok := MatchFAQ(nil, "стендап"); ok {
		t.Error("empty base")
	}
}

func TestWordMatchesToleratesTyposButNotOtherWords(t *testing.T) {
	for _, c := range []struct {
		word, keyword string
		want          bool
	}{
		{"реквесты", "реквест", true},   // other ending
		{"рекуест", "реквест", true},    // wrong letter
		{"отугл", "отгул", true},        // swapped letters
		{"отпгул", "отгул", true},       // extra letter
		{"больнчный", "больничн", true}, // missing letter in a long keyword
		{"документацыя", "документаци", true},
		{"стендах", "стендап", false}, // another word, not a typo
		{"стенда", "стендап", false},  // too short to be a typo
		{"графин", "график", false},
		{"про", "пр", false}, // short keywords are whole words
		{"пр", "пр", true},
		{"цел", "цели", false},
		{"офисе", "офис", true},
		{"оффис", "офис", false}, // no typos in 4-letter keywords
	} {
		if got := wordMatches([]rune(c.word), []rune(c.keyword)); got != c.want {
			t.Errorf("%s ~ %s = %v", c.word, c.keyword, got)
		}
	}
	if !keywordMatches(splitWords("Встреча с моим РУКОВОДИТЕЛЕМ"), "встреч+руковод") {
		t.Error("parts in any place")
	}
	if keywordMatches(splitWords("встреча"), "встреч+руковод") || keywordMatches(splitWords("что угодно"), "+") {
		t.Error("every part is required")
	}
	if !keywordMatches(splitWords("пул реквест"), "пулреквест") || !keywordMatches(splitWords("пулреквест"), "пул реквест") {
		t.Error("glued and split words")
	}
	if typoDistance([]rune("ab"), []rune("ba")) != 1 || typoDistance([]rune(""), []rune("abc")) != 3 {
		t.Error("distance")
	}
}
