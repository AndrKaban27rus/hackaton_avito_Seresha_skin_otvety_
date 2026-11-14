package domain

import (
	"slices"
	"strings"
	"unicode"
)

// FAQ is a frequent intern question with the answer approved by the team lead.
type FAQ struct {
	ID string `json:"id"`
	// Team limits the entry to interns of this team.
	Team string `json:"team"`
	// Topic is a short label the bot lists when it did not understand the question.
	Topic    string `json:"topic"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Match holds keywords for the mode without a model. A keyword is a phrase whose
	// words follow each other in the message; «+» joins phrases that may stand anywhere
	// in any order, so «встреч+руковод» finds «встреча с моим руководителем».
	Match []string `json:"match"`
}

// MatchFAQ is the rule without a model: the first entry with a matching keyword.
// Case, «ё», punctuation, hyphens and spaces inside a phrase do not matter
// («пул-реквест», «пул реквест», «пулреквест»). A keyword is the start of a word,
// so other endings match. Keywords of 5+ letters also accept a typo.
func MatchFAQ(entries []FAQ, text string) (FAQ, bool) {
	words := splitWords(text)
	for _, f := range entries {
		for _, k := range f.Match {
			if keywordMatches(words, k) {
				return f, true
			}
		}
	}
	return FAQ{}, false
}

// splitWords lowercases the text, folds «ё» into «е» and keeps runs of letters and digits.
func splitWords(s string) [][]rune {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	var out [][]rune
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		out = append(out, []rune(w))
	}
	return out
}

func keywordMatches(words [][]rune, keyword string) bool {
	for _, part := range strings.Split(keyword, "+") {
		if !phraseMatches(words, splitWords(part)) {
			return false
		}
	}
	return true
}

// phraseMatches finds the phrase as consecutive words. The words may also be
// glued together or split apart: «пулреквест» for «пул реквест» and back.
func phraseMatches(words, phrase [][]rune) bool {
	if len(phrase) == 0 {
		return false
	}
	for i := 0; i+len(phrase) <= len(words); i++ {
		if consecutive(words[i:], phrase) {
			return true
		}
	}
	glued := slices.Concat(phrase...)
	if len(glued) < 6 {
		return false
	}
	for i, w := range words {
		if len(phrase) > 1 && wordMatches(w, glued) {
			return true
		}
		// «пул реквест» typed for the keyword «пулреквест»: exact letters, and the space must
		// fall inside the keyword, so «стендах делать» is not «стендап».
		if len(phrase) == 1 && i+1 < len(words) && len(w) < len(glued) && hasPrefix(slices.Concat(w, words[i+1]), glued) {
			return true
		}
	}
	return false
}

func hasPrefix(w, k []rune) bool {
	return len(w) >= len(k) && slices.Equal(w[:len(k)], k)
}

// consecutive reports whether words start with the phrase.
func consecutive(words, phrase [][]rune) bool {
	for j, p := range phrase {
		if !wordMatches(words[j], p) {
			return false
		}
	}
	return true
}

// wordMatches accepts the keyword as the start of the word. Keywords of 5+ letters
// also accept one typo (two for 9+ letters): a wrong, missing, extra or swapped letter.
// Keywords of up to 3 letters, such as «пр» or «1», must be the whole word.
func wordMatches(w, k []rune) bool {
	n := len(k)
	if n <= 3 {
		return slices.Equal(w, k)
	}
	if len(w) >= n && slices.Equal(w[:n], k) {
		return true
	}
	if n < 5 {
		return false
	}
	allowed := 1
	if n >= 9 {
		allowed = 2
	}
	for _, m := range []int{n - 1, n, n + 1} {
		// A missing letter is accepted only in long keywords: «стенда» is not a typo of «стендап».
		if m > len(w) || (m < n && n < 8) {
			continue
		}
		c := w[:m]
		// A whole word that differs only in the last letter is another word or form
		// («стендах», «графин»), not a typo.
		if m == n && m == len(w) && slices.Equal(c[:n-1], k[:n-1]) {
			continue
		}
		if typoDistance(c, k) <= allowed {
			return true
		}
	}
	return false
}

// typoDistance is the edit distance with swaps of neighbouring letters.
func typoDistance(a, b []rune) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}
