package kkdai

type KkdaiWrapper struct{}

func New() *KkdaiWrapper {
	return &KkdaiWrapper{}
}

func (y *KkdaiWrapper) GetStreamURL(url string) (string, error) {
	return "", nil
}

type Meta struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	WebPageURL string  `json:"webpage_url"`
	Duration   float64 `json:"duration"`
}

func (y *KkdaiWrapper) GetMetaInfo(url string) (Meta, error) {
	return Meta{}, nil
}

func (y *KkdaiWrapper) GetStream(url string) (string, error) {
	return "", nil
}
