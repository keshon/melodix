package common

import (
	"strings"
	"testing"
)

func TestPlaybackErrorDescription_truncates(t *testing.T) {
	long := strings.Repeat("x", 4000)
	err := errorString(long)
	got := PlaybackErrorDescription(err)
	if len([]rune(got)) > 3501 {
		t.Fatalf("expected truncation around 3500 runes, got %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestPlaybackErrorDescription_nil(t *testing.T) {
	if got := PlaybackErrorDescription(nil); got == "" {
		t.Fatal("expected non-empty fallback")
	}
}

func TestPlaybackErrorString_empty(t *testing.T) {
	if got := PlaybackErrorString(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestPlaybackErrorString_sameCapAsDescription(t *testing.T) {
	long := strings.Repeat("y", 4000)
	got := PlaybackErrorString(long)
	if len([]rune(got)) > 3501 {
		t.Fatalf("expected truncation around 3500 runes, got %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
