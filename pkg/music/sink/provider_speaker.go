package sink

import "github.com/rs/zerolog"

// SpeakerProvider is a Provider that always returns the same SpeakerSink (target ignored).
type SpeakerProvider struct {
	sink *SpeakerSink
}

// NewSpeakerProvider creates a provider that returns a single shared speaker sink.
func NewSpeakerProvider() *SpeakerProvider {
	return &SpeakerProvider{sink: NewSpeakerSink()}
}

// NewSpeakerProviderWithLogger creates a provider that returns a single shared speaker sink with logging.
func NewSpeakerProviderWithLogger(log zerolog.Logger) *SpeakerProvider {
	return &SpeakerProvider{sink: NewSpeakerSinkWithLogger(log)}
}

// Sink returns the shared speaker sink. target is ignored.
func (p *SpeakerProvider) Sink(target string) (AudioSink, error) {
	return p.sink, nil
}

// ReleaseSink is a no-op for speaker (no VC to leave).
func (p *SpeakerProvider) ReleaseSink(target string) {}

// InvalidateSink is a no-op for speaker.
func (p *SpeakerProvider) InvalidateSink() {}

// Close releases the oto context. Call when the CLI exits.
func (p *SpeakerProvider) Close() error {
	return p.sink.Close()
}
