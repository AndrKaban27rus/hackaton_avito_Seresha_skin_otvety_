// Package domain holds the access and selection rules for starter tasks.
// The rules run in Go before any model call and again before every write.
package domain

import (
	"regexp"
	"slices"
	"strings"
)

const (
	RoleIntern = "intern"
	RoleLead   = "lead"

	StatusOpen             = "open"
	ClassificationInternal = "internal"
	LabelStarter           = "starter"
)

// Profile is a synthetic employee from the demo directory.
type Profile struct {
	ID       string
	Role     string
	Name     string
	Team     string
	Grade    string
	Grants   []string
	LeadID   string
	MentorID string
}

// Ticket is a synthetic Jira issue. Nil Requires or Grades means the field is
// missing in Jira, and such a ticket is never offered.
type Ticket struct {
	ID             string
	Team           string
	Title          string
	Description    string
	Requires       []string
	Labels         []string
	Status         string
	Classification string
	Grades         []string
	OKRPriority    int
	// DueDays is the time to finish after assignment; 0 means the default.
	DueDays       int
	DemoRationale string
	DemoFirstStep string
}

// Permitted reports whether the profile may work on the ticket, ignoring
// whether somebody else already holds it.
func Permitted(t Ticket, p Profile) bool {
	if p.Role != RoleIntern || t.Status != StatusOpen || t.Classification != ClassificationInternal {
		return false
	}
	if t.Team != p.Team || !slices.Contains(t.Labels, LabelStarter) {
		return false
	}
	if t.Grades == nil || !slices.Contains(t.Grades, p.Grade) || t.Requires == nil {
		return false
	}
	for _, grant := range t.Requires {
		if !slices.Contains(p.Grants, grant) {
			return false
		}
	}
	return true
}

// Candidates returns permitted tickets that nobody holds, keeping input order.
func Candidates(tickets []Ticket, taken map[string]bool, p Profile) []Ticket {
	out := []Ticket{}
	for _, t := range tickets {
		if !taken[t.ID] && Permitted(t, p) {
			out = append(out, t)
		}
	}
	return out
}

// NoMatchReason explains an empty candidate list without naming excluded tickets.
type NoMatchReason string

const (
	// ReasonTaken means the intern has permitted tickets but all of them are held.
	ReasonTaken NoMatchReason = "taken"
	// ReasonNone means no ticket passes the grade, team, grant and classification checks.
	ReasonNone NoMatchReason = "none"
)

// WhyNoMatch must be called only when Candidates is empty.
func WhyNoMatch(tickets []Ticket, p Profile) NoMatchReason {
	for _, t := range tickets {
		if Permitted(t, p) {
			return ReasonTaken
		}
	}
	return ReasonNone
}

var (
	cyrillicWord = regexp.MustCompile(`[а-яё]+`)
	// RE2 \b is ASCII-only, so word boundaries are spaces around the joined words.
	takeToWork = regexp.MustCompile(` что (?:мне )?(?:можно )?взять в работу `)
	askVerbs   = []string{"подбер", "подбир", "выбер", "выбир", "найд", "назнач", "дай", "дайте", "предлож", "нуж", "хочу"}
	taskNouns  = []string{"задач", "задан", "тикет"}
)

// WantsTicket is the deterministic intent rule used without a model key.
func WantsTicket(message string) bool {
	words := cyrillicWord.FindAllString(strings.ToLower(message), -1)
	if takeToWork.MatchString(" " + strings.Join(words, " ") + " ") {
		return true
	}
	return anyPrefix(words, askVerbs) && anyPrefix(words, taskNouns)
}

func anyPrefix(words, prefixes []string) bool {
	for _, w := range words {
		for _, p := range prefixes {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
	}
	return false
}
