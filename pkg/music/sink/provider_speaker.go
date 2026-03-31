package sink

import "github.com/keshon/melodix/pkg/music/sink/speaker"

// SpeakerProvider is a Provider that always returns the same speaker.Sink (target ignored).
type SpeakerProvider struct {
	sink *speaker.Sink
}

// NewSpeakerProvider creates a provider that returns a single shared speaker sink.
func NewSpeakerProvider() *SpeakerProvider {
	return &SpeakerProvider{sink: speaker.New()}
}

// Sink returns the shared speaker sink. target is ignored.
func (p *SpeakerProvider) Sink(target string) (AudioSink, error) {
	return p.sink, nil
}

// ReleaseSink is a no-op for speaker (no VC to leave).
func (p *SpeakerProvider) ReleaseSink(target string) {}

// Close releases the oto context. Call when the CLI exits.
func (p *SpeakerProvider) Close() error {
	return p.sink.Close()
}
