package buddy

import "testing"

func TestSentenceEndsStepWithPeriod(t *testing.T) {
	for in, want := range map[string]string{
		"Открой тесты":   "Открой тесты.",
		"Открой тесты. ": "Открой тесты.",
		"Готово!":        "Готово!",
		"Подумай…":       "Подумай…",
		"":               "",
	} {
		if got := sentence(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
