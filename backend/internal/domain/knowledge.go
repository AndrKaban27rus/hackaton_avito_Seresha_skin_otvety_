package domain

import (
	"regexp"
	"slices"
	"strings"
)

const RoleMentor = "mentor"

// Fix is a troubleshooting entry of an instruction.
type Fix struct {
	Match      []string `json:"match"`
	NeedsHuman bool     `json:"needs_human"`
	SelfStep   string   `json:"self_step"`
	Request    string   `json:"request"`
}

// Doc is a synthetic resource: a Confluence instruction, a Grafana dashboard,
// a Bitbucket repository or a test stand page.
type Doc struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	TicketID string   `json:"ticket"`
	Title    string   `json:"title"`
	Requires []string `json:"requires"`
	Steps    []string `json:"steps,omitempty"`
	Lines    []string `json:"lines,omitempty"`
	Check    string   `json:"check,omitempty"`
	Fixes    []Fix    `json:"fixes,omitempty"`
}

const KindConfluence = "confluence"

// URL is the demo page of the resource.
func (d Doc) URL() string { return "/demo/" + d.ID }

// CanRead checks access on the server: interns need every grant, staff of
// the same team may read team resources.
func CanRead(d Doc, p Profile, ticketTeam string) bool {
	if p.Role == RoleLead || p.Role == RoleMentor {
		return p.Team == ticketTeam
	}
	if p.Role != RoleIntern || d.Requires == nil {
		return false
	}
	for _, g := range d.Requires {
		if !slices.Contains(p.Grants, g) {
			return false
		}
	}
	return p.Team == ticketTeam
}

// Instruction returns the Confluence instruction among docs, if any.
func Instruction(docs []Doc) (Doc, bool) {
	for _, d := range docs {
		if d.Kind == KindConfluence {
			return d, true
		}
	}
	return Doc{}, false
}

var (
	blockerWords = regexp.MustCompile(`не (?:могу|получается|выходит|запускается|работает|открывается)|застрял|застряла|ошибк|падает|нет доступа|блокер`)
	humanWords   = regexp.MustCompile(`наставник|без помощи|нужна помощь|помоги|сам не|сама не|без тебя не|не справлюсь`)
)

// ReportsBlocker is the deterministic rule for «something stops me».
func ReportsBlocker(message string) bool {
	return blockerWords.MatchString(strings.ToLower(message))
}

// AsksForHuman reports an explicit request for a person.
func AsksForHuman(message string) bool {
	return humanWords.MatchString(strings.ToLower(message))
}

// MatchFix finds the first troubleshooting entry mentioned in the text.
func MatchFix(fixes []Fix, text string) (Fix, bool) {
	text = strings.ToLower(text)
	for _, f := range fixes {
		for _, m := range f.Match {
			if strings.Contains(text, strings.ToLower(m)) {
				return f, true
			}
		}
	}
	return Fix{}, false
}
