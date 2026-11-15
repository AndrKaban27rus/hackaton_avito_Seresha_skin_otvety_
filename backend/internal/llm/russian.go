package llm

import (
	"regexp"
	"strings"
)

// russianOnly is added to every prompt whose answer the intern reads.
const russianOnly = " Пиши только по-русски. Латиницей пиши только то, что есть во входных данных: " +
	"ID задач, имена инструментов, команд, файлов и флагов."

var latinWord = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)

// knownLatin are tool and workflow names the bot may use even when the input does not mention them.
var knownLatin = map[string]bool{
	"jira": true, "grafana": true, "bitbucket": true, "confluence": true, "slack": true,
	"pull": true, "request": true, "merge": true, "review": true, "master": true, "main": true,
	"branch": true, "commit": true, "test": true, "tests": true, "api": true, "okr": true, "buddy": true,
}

// foreignWords returns Latin words of three or more letters that the model did not
// see in its input: free models sometimes slip a word of another language
// («serviert») into a Russian answer.
func foreignWords(text, input string) []string {
	input = strings.ToLower(input)
	var out []string
	seen := map[string]bool{}
	for _, w := range latinWord.FindAllString(text, -1) {
		lw := strings.ToLower(w)
		if len(lw) < 3 || knownLatin[lw] || seen[lw] || strings.Contains(input, lw) {
			continue
		}
		seen[lw] = true
		out = append(out, w)
	}
	return out
}

// inputText joins what the model saw, to tell input words from invented ones.
func inputText(messages []message) string {
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// russianRetry runs a model call once more when its text has foreign words.
// The second answer is kept even if it still has them: a reply is better than an error.
func (o *OpenRouter) russianRetry(input string, call func() (string, error)) error {
	for attempt := 1; ; attempt++ {
		text, err := call()
		if err != nil {
			return err
		}
		bad := foreignWords(text, input)
		if len(bad) == 0 {
			return nil
		}
		if attempt == 2 {
			o.warn("model answer still has foreign words", "words", bad)
			return nil
		}
		o.warn("model answer has foreign words, asking again", "words", bad)
	}
}
