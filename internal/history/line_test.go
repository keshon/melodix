package history

import (
	"strings"
	"testing"
)

func TestTruncateTitleMiddle(t *testing.T) {
	t.Parallel()
	short := "abc"
	if got := TruncateTitleMiddle(short, 10); got != short {
		t.Fatalf("short: %q", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := TruncateTitleMiddle(long, 12)
	if len([]rune(got)) != 12 {
		t.Fatalf("rune len: %q len=%d", got, len([]rune(got)))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected ellipsis: %q", got)
	}
}
