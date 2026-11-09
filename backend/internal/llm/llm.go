package llm

import "context"

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
type Answer struct {
	Text    string   `json:"answer"`
	Sources []Source `json:"sources"`
}
type Assistant interface {
	Ask(context.Context, string) (Answer, error)
}
