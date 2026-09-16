package perm

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The mask and the names are two readings of one list, and they used to be two
// lists: the invite URL asked for eight permissions while the README promised
// five. This is the assertion that keeps them one.
func TestRecommendedMaskAndNamesDescribeTheSameSet(t *testing.T) {
	mask := RecommendedBotMask()
	names := RecommendedBotNames()

	if len(names) != len(recommendedBot) {
		t.Fatalf("named %d permissions, mask covers %d", len(names), len(recommendedBot))
	}
	for _, bit := range recommendedBot {
		if mask&bit == 0 {
			t.Errorf("permission %#x is named but missing from the mask", bit)
		}
	}
}

// A bit nobody has a name for would reach the README as hex, which tells a
// server admin nothing about what they are granting.
func TestEveryRecommendedPermissionHasAName(t *testing.T) {
	for i, name := range RecommendedBotNames() {
		if name == "" || strings.HasPrefix(name, "0x") {
			t.Errorf("permission %#x renders as %q; add it to permissionNames", recommendedBot[i], name)
		}
	}
}

// Name falls back rather than returning nothing, so an unrecognised bit still
// says something specific.
func TestNameFallsBackToHex(t *testing.T) {
	const unknownBit int64 = 1 << 62

	if got := Name(unknownBit); got != "0x4000000000000000" {
		t.Errorf("Name(unknown) = %q, want the hex form", got)
	}
}

// The invite has to grant what playback checks for. The recommended set asked
// for eight permissions and not Connect or Speak, which is the pair
// CheckBotVoicePermissions refuses /play without -- so a bot invited exactly
// as recommended could not play anything.
func TestTheInviteGrantsWhatPlaybackChecks(t *testing.T) {
	if RecommendedBotMask()&int64(VoicePlayback) != int64(VoicePlayback) {
		t.Fatalf("recommended mask %#x lacks the voice permissions playback requires (%#x)",
			RecommendedBotMask(), int64(VoicePlayback))
	}
}

// running.md hands people an invite URL with the mask written out, and that
// number is a third copy of this list unless something holds it to the first.
// It had drifted: it asked for Manage Messages and not Attach Files, while the
// list here asked for Manage Roles and not Connect.
func TestRunningDocInvitesWithTheRecommendedMask(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "running.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`oauth2/authorize\?[^\s)]*permissions=(\d+)`).FindSubmatch(doc)
	if m == nil {
		t.Fatal("running.md has no invite URL with a permissions= mask")
	}
	if got, want := string(m[1]), strconv.FormatInt(RecommendedBotMask(), 10); got != want {
		t.Fatalf("running.md invites with permissions=%s; the recommended mask is %s (%s)",
			got, want, strings.Join(RecommendedBotNames(), ", "))
	}
}
