package perm

import (
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
