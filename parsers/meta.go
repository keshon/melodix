package parsers

type Meta struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	WebPageURL string  `json:"webpage_url"`
	Duration   float64 `json:"duration"`
	Parser     string
}
