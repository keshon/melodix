package ytdlp

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

// resetRuntimeDetection lets each case start from a clean slate, since the
// detection is deliberately done once per process.
func resetRuntimeDetection(t *testing.T, found map[string]bool) {
	t.Helper()
	// sync.Once carries a noCopy, so it is reset to a fresh zero value rather
	// than saved and restored.
	origLook, origOpts := lookPath, youtubeOpts
	t.Cleanup(func() {
		lookPath, youtubeOpts = origLook, origOpts
		runtimeOnce = sync.Once{}
	})

	runtimeOnce = sync.Once{}
	youtubeOpts = nil
	lookPath = func(name string) (string, error) {
		if found[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestYoutubeArgsPrefersDeno(t *testing.T) {
	// deno is the one runtime yt-dlp enables by itself, so it is preferred where
	// several are installed.
	resetRuntimeDetection(t, map[string]bool{"deno": true, "node": true, "bun": true})

	got := youtubeArgs()
	if i := slices.Index(got, "--js-runtimes"); i < 0 || got[i+1] != "deno" {
		t.Fatalf("args = %v, want deno", got)
	}
	if i := slices.Index(got, "--extractor-args"); i < 0 || got[i+1] != embeddedWebClient {
		t.Fatalf("args = %v, want the embedded web client", got)
	}
}

func TestYoutubeArgsFallsDownTheList(t *testing.T) {
	resetRuntimeDetection(t, map[string]bool{"node": true, "bun": true})

	got := youtubeArgs()
	if i := slices.Index(got, "--js-runtimes"); i < 0 || got[i+1] != "node" {
		t.Fatalf("args = %v, want node", got)
	}
}

// The important negative case: asking for the embedded web client without a
// runtime fails outright rather than degrading, so no runtime must mean no
// flags — a partial failure (live streams) beats a total one (nothing plays).
func TestYoutubeArgsEmptyWithoutARuntime(t *testing.T) {
	resetRuntimeDetection(t, nil)

	if got := youtubeArgs(); len(got) != 0 {
		t.Fatalf("args = %v, want none without a JS runtime", got)
	}
}

func TestYoutubeArgsDetectsOnce(t *testing.T) {
	resetRuntimeDetection(t, map[string]bool{"node": true})

	calls := 0
	inner := lookPath
	lookPath = func(name string) (string, error) {
		calls++
		return inner(name)
	}

	for i := 0; i < 5; i++ {
		youtubeArgs()
	}
	// deno misses, node hits: two lookups, once.
	if calls != 2 {
		t.Fatalf("lookPath called %d times, want the detection to happen once", calls)
	}
}

func TestArgsCopiesThePrefix(t *testing.T) {
	resetRuntimeDetection(t, map[string]bool{"node": true})

	first := args("-j", "https://a.test/1")
	second := args("-o", "-", "https://a.test/2")

	// Appending into the shared prefix would have let the second call rewrite
	// the first one's arguments.
	if slices.Contains(first, "https://a.test/2") {
		t.Fatalf("first invocation was scribbled on: %v", first)
	}
	if !slices.Contains(first, "https://a.test/1") || !slices.Contains(second, "https://a.test/2") {
		t.Fatalf("first=%v second=%v", first, second)
	}
	if !strings.HasPrefix(strings.Join(second, " "), "--js-runtimes node") {
		t.Fatalf("options must lead: %v", second)
	}
}
