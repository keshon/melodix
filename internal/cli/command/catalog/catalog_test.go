package catalog

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/rs/zerolog"
)

// Register panics on a taken name, so registering at all proves the names and
// aliases are unique. Every category must be one the bot orders by, or help
// and the README would put the command under a heading of its own.
func TestCatalogRegistersUnderKnownCategories(t *testing.T) {
	reg := cli.NewRegistry()
	Register(reg)
	if len(reg.All()) == 0 {
		t.Fatal("no commands registered")
	}
	for _, c := range reg.All() {
		if _, ok := config.CategoryWeights[c.Category()]; !ok {
			t.Errorf("%s: category %q is not in config.CategoryWeights", c.Name(), c.Category())
		}
	}
}

type noSink struct{}

func (noSink) Sink(string) (sink.AudioSink, error) { return nil, errors.New("no audio in tests") }
func (noSink) ReleaseSink(string)                  {}
func (noSink) InvalidateSink()                     {}

// The commands that need neither network nor audio, run the way a person
// would type them.
func TestCommandsAgainstHistory(t *testing.T) {
	store, err := storage.NewStorage(t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i, title := range []string{"first", "second", "first"} {
		track := parsers.Track{URL: "https://example.com/" + title, Title: title, CurrentParser: "ytnative-link"}
		if _, err := store.AppendMusicPlayback("cli", track, at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	svc := music.New(&config.Config{}, store, zerolog.Nop(), music.Hooks{
		NewSink: func(string) sink.Provider { return noSink{} },
	})
	t.Cleanup(svc.StopAll)
	reg := cli.NewRegistry()
	Register(reg)

	var out strings.Builder
	input := "help\nhistory\nhistory counts\nqueue\nnext\nplay 999\nstop\nquit\n"
	if err := cli.Run(reg, svc, "cli", strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, want := range []string{
		"🎵 Music",             // help groups by category
		"#3     first",        // timeline, newest first
		"×2",                  // counts view
		"The queue is empty.", // queue
		"No tracks left to skip.",
		"Unknown history id.",
		"Playback stopped. Queue cleared.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Index(out.String(), "#3") > strings.Index(out.String(), "#1 ") {
		t.Errorf("timeline is not newest first:\n%s", out.String())
	}
}
