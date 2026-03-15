package sink

import "github.com/keshon/melodix/pkg/music/sink/speaker"

// SpeakerProvider is a SinkProvider that always returns the same SpeakerSink (target ignored).
type SpeakerProvider struct {
	sink *speaker.SpeakerSink
}

// NewSpeakerProvider creates a provider that returns a single shared SpeakerSink.
func NewSpeakerProvider() *SpeakerProvider {
	return &SpeakerProvider{sink: speaker.NewSpeakerSink()}
}

// GetSink returns the shared speaker sink. target is ignored.
func (p *SpeakerProvider) GetSink(target string) (AudioSink, error) {
	return p.sink, nil
}

// ReleaseSink is a no-op for speaker (no VC to leave).
func (p *SpeakerProvider) ReleaseSink(target string) {}

// Close releases the oto context. Call when the CLI exits.
func (p *SpeakerProvider) Close() error {
	return p.sink.Close()
}
