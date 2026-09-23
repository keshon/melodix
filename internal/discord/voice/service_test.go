package voice

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

// --- a connection that records what was sent where ---

type sentEmbed struct {
	channelID, messageID string
	embed                *adapter.Embed
}

type recordingAPI struct {
	mu     sync.Mutex
	edits  []sentEmbed
	posts  []sentEmbed
	nextID int
}

func (a *recordingAPI) EditChannelEmbed(channelID, messageID string, embed *adapter.Embed) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.edits = append(a.edits, sentEmbed{channelID, messageID, embed})
	return nil
}

func (a *recordingAPI) PostChannelEmbed(channelID string, embed *adapter.Embed) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := "posted-" + string(rune('0'+a.nextID))
	a.posts = append(a.posts, sentEmbed{channelID, id, embed})
	return id, nil
}

func (a *recordingAPI) editsOf(messageID string) []*adapter.Embed {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*adapter.Embed
	for _, e := range a.edits {
		if e.messageID == messageID {
			out = append(out, e.embed)
		}
	}
	return out
}

func (a *recordingAPI) MemberPermissions(string, string) (int64, error) { return 0, nil }
func (a *recordingAPI) CheckBotPermissions(string) bool                 { return true }
func (a *recordingAPI) CheckBotVoicePermissions(string) (bool, error)   { return true, nil }
func (a *recordingAPI) SendChannelMessage(string, string) error         { return nil }
func (a *recordingAPI) SendChannelEmbed(string, *adapter.Embed) error   { return nil }
func (a *recordingAPI) GuildInfo(string) (adapter.GuildInfo, error)     { return adapter.GuildInfo{}, nil }
func (a *recordingAPI) Latency() time.Duration                          { return 0 }
func (a *recordingAPI) EmbedColor() int                                 { return 0 }
func (a *recordingAPI) UserVoiceChannel(string, string) (string, error) {
	return "", errors.New("unused")
}

var _ adapter.BotAPI = (*recordingAPI)(nil)

// --- an interaction whose answer takes as long as a real one ---

type fakeInteraction struct {
	component bool
	answerID  string
	// latency stands in for the round trip of answering. The status watcher
	// runs while it lasts, which is the window the ordering bugs lived in.
	latency time.Duration

	mu       sync.Mutex
	answered []*adapter.Embed
}

func (f *fakeInteraction) GuildID() string                        { return "g1" }
func (f *fakeInteraction) ChannelID() string                      { return "text1" }
func (f *fakeInteraction) UserID() string                         { return "u1" }
func (f *fakeInteraction) Defer() error                           { return nil }
func (f *fakeInteraction) DeferEphemeral() error                  { return nil }
func (f *fakeInteraction) Respond(*adapter.Embed) error           { return nil }
func (f *fakeInteraction) RespondEphemeral(*adapter.Embed) error  { return nil }
func (f *fakeInteraction) Followup(*adapter.Embed) error          { return nil }
func (f *fakeInteraction) FollowupEphemeral(*adapter.Embed) error { return nil }
func (f *fakeInteraction) EditResponseText(string) error          { return nil }
func (f *fakeInteraction) CanJoinVoice(string) (bool, error)      { return true, nil }
func (f *fakeInteraction) Component() bool                        { return f.component }
func (f *fakeInteraction) AnswerEmbedMessage(e *adapter.Embed) (string, string, error) {
	time.Sleep(f.latency)
	f.mu.Lock()
	f.answered = append(f.answered, e)
	f.mu.Unlock()
	return "text1", f.answerID, nil
}

var _ adapter.Interaction = (*fakeInteraction)(nil)

// --- audio that plays until stopped ---

type silentReader struct{ closed chan struct{} }

func (r *silentReader) ReadPacket() ([]byte, error) { <-r.closed; return nil, errors.New("closed") }
func (r *silentReader) Close() error                { return nil }

type silentStreamer struct{}

func (silentStreamer) Open(track parsers.Track, _ float64) (parsers.Opened, error) {
	r := &silentReader{closed: make(chan struct{})}
	var once sync.Once
	return parsers.Opened{
		Reader:  r,
		Cleanup: func() { once.Do(func() { close(r.closed) }) },
		Title:   track.SourceInfo.Title,
	}, nil
}

type heldSink struct{}

func (heldSink) Stream(_ opus.Reader, stop <-chan struct{}) error {
	<-stop
	return stream.ErrPlaybackStopped
}

type heldProvider struct{}

func (heldProvider) Sink(string) (sink.AudioSink, error) { return heldSink{}, nil }
func (heldProvider) ReleaseSink(string)                  {}
func (heldProvider) InvalidateSink()                     {}

// newTestService wires the service into a music.Service the way the bot does,
// and returns guild g1's player.
func newTestService(t *testing.T) (*Service, *recordingAPI, *player.Player) {
	t.Helper()
	previous := stream.SetRegistry(map[string]parsers.Streamer{"silent": silentStreamer{}})
	t.Cleanup(func() { stream.SetRegistry(previous) })

	api := &recordingAPI{}
	s := NewVoiceService(func() adapter.BotAPI { return api }, zerolog.Nop())
	m := music.New(&config.Config{}, nil, zerolog.Nop(), music.Hooks{
		NewSink:  func(string) sink.Provider { return heldProvider{} },
		Watch:    s.WatchPlayerStatus,
		OnFailed: s.NotifyPlaybackFailed,
	})
	t.Cleanup(m.StopAll)
	return s, api, m.Player("g1")
}

func queue(t *testing.T, p *player.Player, titles ...string) {
	t.Helper()
	infos := make([]sources.TrackInfo, 0, len(titles))
	for _, title := range titles {
		infos = append(infos, sources.TrackInfo{
			URL: "https://example.com/" + title, Title: title,
			SourceName: sources.YouTube, AvailableParsers: []string{"silent"},
		})
	}
	if err := p.EnqueueTrackInfos(infos); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func registered(s *Service) guildMusicStatus {
	s.guildMusicStatusMu.RLock()
	defer s.guildMusicStatusMu.RUnlock()
	return s.guildMusicStatus["g1"]
}

func mentions(embeds []*adapter.Embed, text string) bool {
	for _, e := range embeds {
		if strings.Contains(e.Description, text) {
			return true
		}
	}
	return false
}

// Starting playback used to edit the previous status message into the new
// track. The player announces Playing while PlayNext is still running, before
// the command has posted and registered its own message, so the watcher found
// the old registration and wrote the new track into it. That message then
// stayed on that track for good, above the one that went on being updated.
func TestStartingPlaybackLeavesThePreviousStatusMessageAlone(t *testing.T) {
	s, api, p := newTestService(t)
	s.guildMusicStatus["g1"] = guildMusicStatus{ChannelID: "text1", MessageID: "old"}
	queue(t, p, "fresh")

	to := &fakeInteraction{answerID: "new", latency: 200 * time.Millisecond}
	if err := s.PlayNextAndAnnounce(to, p, "g1", "vc1", 1, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let the watcher act on anything left

	if mentions(api.editsOf("old"), "fresh") {
		t.Fatal("the previous status message was rewritten to the track that just started")
	}
	if got := registered(s).MessageID; got != "new" {
		t.Fatalf("status message is %q, want the one this start posted", got)
	}
}

// A /search pick is a component interaction, and its own answer is the
// ephemeral chooser: registering that made the Now Playing message visible to
// one person, and every later edit went to a message the channel endpoint
// cannot reach. The status message has to be public.
func TestAPickStartsAPublicStatusMessage(t *testing.T) {
	s, api, p := newTestService(t)
	queue(t, p, "picked")

	to := &fakeInteraction{component: true, answerID: "chooser"}
	if err := s.PlayNextAndAnnounce(to, p, "g1", "vc1", 1, nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	got := registered(s)
	if got.MessageID == "chooser" || got.MessageID == "" {
		t.Fatalf("status message is %q, want a public post", got.MessageID)
	}
	api.mu.Lock()
	posts := append([]sentEmbed(nil), api.posts...)
	api.mu.Unlock()
	if len(posts) != 1 || posts[0].messageID != got.MessageID || posts[0].channelID != "text1" {
		t.Fatalf("posts = %+v, want one public Now Playing in the channel the pick came from", posts)
	}
	to.mu.Lock()
	defer to.mu.Unlock()
	if len(to.answered) != 1 {
		t.Fatalf("the chooser was answered %d times; the person who picked still needs one answer", len(to.answered))
	}
}

// /next moves the playing track into the past. The status message it was on
// used to go on saying that track is playing, above the message for the one
// that replaced it.
func TestSkippingMarksTheOldStatusMessageSkipped(t *testing.T) {
	s, api, p := newTestService(t)
	queue(t, p, "first", "second")
	if err := s.PlayNextAndAnnounce(&fakeInteraction{answerID: "m1"}, p, "g1", "vc1", 2, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	current, _ := p.CurrentTrack()

	_ = p.Stop(false)
	if err := s.PlayNextAndAnnounce(&fakeInteraction{answerID: "m2"}, p, "g1", "vc1", 0, &current); err != nil {
		t.Fatalf("skip: %v", err)
	}

	var skipped bool
	for _, e := range api.editsOf("m1") {
		if strings.HasPrefix(e.Title, "⏭") && strings.Contains(e.Description, "first") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("the skipped track's status message was not marked skipped")
	}
	if got := registered(s).MessageID; got != "m2" {
		t.Fatalf("status message is %q, want the skip's own", got)
	}
}
