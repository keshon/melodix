package reply

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
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
			do:   func(s *responseState) { s.deferredNow(); s.answeredNow() },
			want: false,
		},
		{
			name: "deferred and nothing answered",
			do:   func(s *responseState) { s.deferredNow() },
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
			do:   func(s *responseState) { s.answeredNow(); s.deferredNow() },
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
	s.deferredNow()

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
	s.deferredNow()

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
	embed := &cmdadapter.Embed{Description: "body"}

	for name, tc := range map[string]struct {
		in   cmdadapter.Reply
		want func(discord.MessageCreate) error
	}{
		"embed": {
			cmdadapter.Reply{Embed: embed},
			func(m discord.MessageCreate) error {
				if len(m.Embeds) != 1 || m.Flags != 0 {
					return fmt.Errorf("embeds=%d flags=%d", len(m.Embeds), m.Flags)
				}
				return nil
			},
		},
		"ephemeral text": {
			cmdadapter.Reply{Text: "hello", Ephemeral: true},
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
			cmdadapter.Reply{Embed: embed, File: strings.NewReader("x"), FileName: "a.txt"},
			func(m discord.MessageCreate) error {
				if len(m.Files) != 1 || m.Files[0].Name != "a.txt" {
					return fmt.Errorf("files=%d", len(m.Files))
				}
				return nil
			},
		},
		"buttons": {
			cmdadapter.Reply{Embed: embed, Buttons: []cmdadapter.ActionRow{{
				Buttons: []cmdadapter.Button{{Label: "go", CustomID: "search:yt:1"}},
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
