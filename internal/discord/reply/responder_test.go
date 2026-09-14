package reply

import (
	"sync"
	"testing"
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
