package reply

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/discord/adapter"
)

// The placeholder is only removed when the caller saw nothing in its place.
// Both mistakes here are visible to users: too eager and an ephemeral reply
// blinks out with the response that carried it, too shy and a "thinking"
// message sits in the channel forever.
func TestOnlyAnUnansweredDeferIsPending(t *testing.T) {
	cases := []struct {
		name string
		do   func(*responseState)
		want bool
	}{
		{
			name: "deferred and answered by a followup",
			do:   func(s *responseState) { s.deferredNow(false); s.answeredNow() },
			want: false,
		},
		{
			name: "deferred and nothing answered",
			do:   func(s *responseState) { s.deferredNow(false) },
			want: true,
		},
		{
			name: "answered without ever deferring",
			do:   func(s *responseState) { s.answeredNow() },
			want: false,
		},
		{
			name: "neither: an interaction nobody touched has no placeholder",
			do:   func(s *responseState) {},
			want: false,
		},
		{
			name: "answered before the defer is recorded",
			do:   func(s *responseState) { s.answeredNow(); s.deferredNow(false) },
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s responseState
			tc.do(&s)
			if got := s.takePending(); got != tc.want {
				t.Errorf("takePending() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The delete must be issued once. A second call would target an interaction
// response that is already gone, which is an error the log would carry on
// every command that answers this way.
func TestPendingIsClaimedOnce(t *testing.T) {
	var s responseState
	s.deferredNow(false)

	if !s.takePending() {
		t.Fatal("first call did not claim the placeholder")
	}
	for i := 0; i < 3; i++ {
		if s.takePending() {
			t.Fatalf("claimed the placeholder again on call %d", i+2)
		}
	}
}

// Only one goroutine should ever get the claim, whatever order the marks
// arrive in.
func TestPendingIsClaimedOnceUnderConcurrency(t *testing.T) {
	var s responseState
	s.deferredNow(false)

	const goroutines = 16
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		claims int
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if s.takePending() {
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if claims != 1 {
		t.Errorf("placeholder claimed %d times, want exactly 1", claims)
	}
}

// Every response fallback in the responder turns on recognising this one
// error, so it is worth pinning to the code rather than to the sentence
// Discord happens to send with it.
func TestAlreadyAcknowledgedIsMatchedByCode(t *testing.T) {
	acked := &rest.Error{Code: rest.JSONErrorCodeInteractionAlreadyAcknowledged}
	if !alreadyAcknowledged(acked) {
		t.Fatal("did not recognise 40060")
	}
	if !alreadyAcknowledged(fmt.Errorf("responding: %w", acked)) {
		t.Fatal("did not recognise 40060 through a wrap")
	}

	for name, err := range map[string]error{
		"nil":               nil,
		"unrelated rest":    &rest.Error{Code: rest.JSONErrorCodeUnknownInteraction},
		"plain error":       errors.New("already been acknowledged"),
		"unrelated wrapped": fmt.Errorf("x: %w", errors.New("boom")),
	} {
		if alreadyAcknowledged(err) {
			t.Errorf("%s: treated as an acknowledged interaction", name)
		}
	}
}

// One create for every shape of reply, so Respond and Followup cannot disagree
// about what a field means -- which is the failure a method per combination
// invites, and the reason there is no longer one.
func TestAReplyBecomesTheMessageItDescribes(t *testing.T) {
	embed := &adapter.Embed{Description: "body"}

	for name, tc := range map[string]struct {
		in   adapter.Reply
		want func(discord.MessageCreate) error
	}{
		"embed": {
			adapter.Reply{Embed: embed},
			func(m discord.MessageCreate) error {
				if len(m.Embeds) != 1 || m.Flags != 0 {
					return fmt.Errorf("embeds=%d flags=%d", len(m.Embeds), m.Flags)
				}
				return nil
			},
		},
		"ephemeral text": {
			adapter.Reply{Text: "hello", Ephemeral: true},
			func(m discord.MessageCreate) error {
				if m.Content != "hello" || m.Flags != discord.MessageFlagEphemeral {
					return fmt.Errorf("content=%q flags=%d", m.Content, m.Flags)
				}
				if len(m.Embeds) != 0 {
					return fmt.Errorf("an embed appeared from nowhere")
				}
				return nil
			},
		},
		"attachment": {
			adapter.Reply{Embed: embed, File: strings.NewReader("x"), FileName: "a.txt"},
			func(m discord.MessageCreate) error {
				if len(m.Files) != 1 || m.Files[0].Name != "a.txt" {
					return fmt.Errorf("files=%d", len(m.Files))
				}
				return nil
			},
		},
		"buttons": {
			adapter.Reply{Embed: embed, Buttons: []adapter.ActionRow{{
				Buttons: []adapter.Button{{Label: "go", CustomID: "search:yt:1"}},
			}}},
			func(m discord.MessageCreate) error {
				if len(m.Components) != 1 {
					return fmt.Errorf("components=%d", len(m.Components))
				}
				return nil
			},
		},
	} {
		if err := tc.want(create(tc.in)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// --- the order of calls a Responder makes ---

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	l.mu.Lock()
	l.calls = append(l.calls, call)
	l.mu.Unlock()
}

func (l *callLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.calls, ", ")
}

type loggedEvent struct{ log *callLog }

func (e loggedEvent) CreateMessage(discord.MessageCreate, ...rest.RequestOpt) error {
	e.log.add("respond")
	return nil
}

func (e loggedEvent) DeferCreateMessage(ephemeral bool, _ ...rest.RequestOpt) error {
	if ephemeral {
		e.log.add("defer ephemeral")
	} else {
		e.log.add("defer public")
	}
	return nil
}

func (e loggedEvent) Client() *bot.Client { return nil }

type loggedREST struct{ log *callLog }

func (r loggedREST) CreateFollowupMessage(_ snowflake.ID, _ string, m discord.MessageCreate, _ ...rest.RequestOpt) (*discord.Message, error) {
	if m.Flags.Has(discord.MessageFlagEphemeral) {
		r.log.add("followup ephemeral")
	} else {
		r.log.add("followup public")
	}
	return &discord.Message{}, nil
}

func (r loggedREST) UpdateInteractionResponse(snowflake.ID, string, discord.MessageUpdate, ...rest.RequestOpt) (*discord.Message, error) {
	r.log.add("edit original")
	return &discord.Message{}, nil
}

func (r loggedREST) DeleteInteractionResponse(snowflake.ID, string, ...rest.RequestOpt) error {
	r.log.add("delete original")
	return nil
}

func loggedResponder() (*Responder, *callLog) {
	log := &callLog{}
	return &Responder{event: loggedEvent{log}, rest: loggedREST{log}}, log
}

// A reply asked for as ephemeral has to be ephemeral. After a public defer it
// was not: the first followup replaces the "thinking" placeholder and takes
// the deferral's visibility, so /play's voice errors, /next's, /stop's and the
// dispatcher's own error replies were all posted to the whole channel. The
// placeholder has to go first; the followup then stands on its own flags.
func TestAnEphemeralReplyAfterAPublicDeferStaysPrivate(t *testing.T) {
	r, log := loggedResponder()
	_ = r.AckDeferred(false)

	_ = r.Followup(adapter.Reply{Embed: &adapter.Embed{Description: "x"}, Ephemeral: true})

	if got, want := log.String(), "defer public, delete original, followup ephemeral"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// Nothing extra where nothing is wrong: the placeholder is already private, or
// the reply is public, or something has already answered.
func TestOnlyAPublicPlaceholderIsRemovedForAPrivateReply(t *testing.T) {
	ephemeral := adapter.Reply{Embed: &adapter.Embed{Description: "x"}, Ephemeral: true}
	public := adapter.Reply{Embed: &adapter.Embed{Description: "x"}}
	cases := []struct {
		name string
		do   func(*Responder)
		want string
	}{
		{"private defer", func(r *Responder) { _ = r.AckDeferred(true); _ = r.Followup(ephemeral) },
			"defer ephemeral, followup ephemeral"},
		{"public reply", func(r *Responder) { _ = r.AckDeferred(false); _ = r.Followup(public) },
			"defer public, followup public"},
		{"second private reply", func(r *Responder) {
			_ = r.AckDeferred(false)
			_ = r.Followup(ephemeral)
			_ = r.Followup(ephemeral)
		}, "defer public, delete original, followup ephemeral, followup ephemeral"},
		{"after the placeholder was answered", func(r *Responder) {
			_ = r.AckDeferred(false)
			_, _, _ = r.AnswerEmbedMessage(&adapter.Embed{Description: "x"})
			_ = r.Followup(ephemeral)
		}, "defer public, edit original, followup ephemeral"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, log := loggedResponder()
			tc.do(r)
			if got := log.String(); got != tc.want {
				t.Fatalf("calls = %q, want %q", got, tc.want)
			}
		})
	}
}
