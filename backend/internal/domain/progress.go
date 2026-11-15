package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Assignment statuses in the first-weeks path.
const (
	StatusAssigned   = "assigned"
	StatusInProgress = "in_progress"
	StatusReview     = "review"

	DefaultDueDays = 3
)

var statusNames = map[string]string{
	StatusAssigned:   "Назначена",
	StatusInProgress: "В работе",
	StatusReview:     "На ревью",
}

// StatusName returns the Russian label used in chat and UI.
func StatusName(status string) string {
	if name, ok := statusNames[status]; ok {
		return name
	}
	return status
}

// KnownStatus reports whether the status exists in the workflow.
func KnownStatus(status string) bool {
	_, ok := statusNames[status]
	return ok
}

// NextStatus is the only allowed forward step; review is final in this iteration.
func NextStatus(status string) (string, bool) {
	switch status {
	case StatusAssigned:
		return StatusInProgress, true
	case StatusInProgress:
		return StatusReview, true
	}
	return "", false
}

// Zone is the demo time zone: Moscow has no daylight saving time.
var Zone = time.FixedZone("MSK", 3*60*60)

// Day is a calendar date stored as midnight UTC, like PostgreSQL DATE.
type Day struct{ t time.Time }

func DayOf(y int, m time.Month, d int) Day { return Day{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)} }

// DayFromTime keeps the calendar date of a DATE value.
func DayFromTime(t time.Time) Day { return DayOf(t.Year(), t.Month(), t.Day()) }

// Today is the demo date: the Moscow date of now shifted by offsetDays.
func Today(now time.Time, offsetDays int) Day {
	m := now.In(Zone)
	return DayOf(m.Year(), m.Month(), m.Day()).Add(offsetDays)
}

func (d Day) Add(days int) Day    { return Day{d.t.AddDate(0, 0, days)} }
func (d Day) Time() time.Time     { return d.t }
func (d Day) Before(o Day) bool   { return d.t.Before(o.t) }
func (d Day) Equal(o Day) bool    { return d.t.Equal(o.t) }
func (d Day) String() string      { return d.t.Format("2006-01-02") }
func (d Day) DaysUntil(o Day) int { return int(o.t.Sub(d.t).Hours() / 24) }
func DueDate(assigned Day, n int) Day {
	if n <= 0 {
		n = DefaultDueDays
	}
	return assigned.Add(n)
}

var (
	months   = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	weekdays = []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
)

// Label formats a date as «17 ноября, вт».
func (d Day) Label() string {
	return fmt.Sprintf("%d %s, %s", d.t.Day(), months[d.t.Month()-1], weekdays[d.t.Weekday()])
}

// Reminder kinds sent to the intern.
const (
	ReminderDueSoon = "due_soon"
	ReminderOverdue = "overdue"
)

// ReminderFor decides which reminder is relevant today. Work on review needs
// none; a missed «due soon» is not sent after the deadline.
func ReminderFor(status string, due, today Day) (string, bool) {
	switch {
	case status == StatusReview:
		return "", false
	case due.Before(today):
		return ReminderOverdue, true
	case today.Add(1).Equal(due) || today.Equal(due):
		return ReminderDueSoon, true
	}
	return "", false
}

var statusQuestion = regexp.MustCompile(`статус|перев[её]|перевод|ревью|в работу|срок|дедлайн`)

// taskWords mark a message about the current task: its steps, tools and result.
var taskWords = regexp.MustCompile(`начат|начин|начн|шаг|дальше|делать|сделат|задач|зачем|объясн|метрик|ссылк|` +
	`инструкц|замер|тест|дашборд|репозитор|стенд|кэш|кеш|фильтр|p95|grafana|bitbucket|jira|confluence|` +
	`результат|прилож|где посмотр|где найт|где леж|где взят`)

// AsksAboutTask is the rule without a model: does the message ask about the current task?
// Anything else gets the list of topics instead of the task instruction.
func AsksAboutTask(message string) bool {
	return taskWords.MatchString(strings.ToLower(message))
}

// AsksAboutProgress is the deterministic rule for questions about status or deadline.
func AsksAboutProgress(message string) bool {
	return statusQuestion.MatchString(strings.ToLower(message))
}
