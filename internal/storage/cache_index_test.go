package storage

import (
	"testing"

	"github.com/keshon/melodix/pkg/music/cache"
)

func TestCacheIndexRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	idx := s.CacheIndex()

	if m, err := idx.Load(); err != nil || len(m) != 0 {
		t.Fatalf("empty Load = (%v,%v), want (empty,nil)", m, err)
	}

	e := cache.Entry{ID: "youtube:x", File: "abc.mxo", Bytes: 10, Packets: 2, Title: "T"}
	if err := idx.Put(e); err != nil {
		t.Fatalf("Put: %v", err)
	}
	m, err := idx.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := m["youtube:x"]
	if !ok || got.File != "abc.mxo" || got.Bytes != 10 || got.Title != "T" {
		t.Fatalf("round-trip mismatch: %+v", m)
	}

	if err := idx.Delete("youtube:x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if m, _ := idx.Load(); len(m) != 0 {
		t.Fatalf("entry should be gone after Delete: %+v", m)
	}
}

// The cache index lives in its own collection, so it must not appear in a
// guild's export (it is global, not per-guild data).
func TestCacheIndexIsNotGuildData(t *testing.T) {
	s := newTestStorage(t)
	if err := s.CacheIndex().Put(cache.Entry{ID: "youtube:x", File: "a.mxo"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.SetCommand("g1", "c", "cn", "gn", "u", "un", "/play"); err != nil {
		t.Fatalf("SetCommand: %v", err)
	}

	export, err := s.ExportGuild("g1")
	if err != nil {
		t.Fatalf("ExportGuild: %v", err)
	}
	if len(export.CommandsHistory) != 1 || export.CommandsHistory[0].Command != "/play" {
		t.Fatalf("guild export should carry its command log: %+v", export)
	}
	if export.GuildID != "g1" {
		t.Fatalf("guild id: %q", export.GuildID)
	}
}

func TestCommandLogTrims(t *testing.T) {
	s := newTestStorage(t)
	for i := 0; i < commandHistoryLimit+5; i++ {
		if err := s.SetCommand("g", "c", "cn", "gn", "u", "un", "/play"); err != nil {
			t.Fatalf("SetCommand %d: %v", i, err)
		}
	}
	rows, err := s.CommandHistory("g")
	if err != nil {
		t.Fatalf("CommandHistory: %v", err)
	}
	if len(rows) != commandHistoryLimit {
		t.Fatalf("command log len = %d, want %d", len(rows), commandHistoryLimit)
	}
	// Oldest-first, and the oldest surviving id is the 6th ever written.
	if rows[0].ID != 6 {
		t.Fatalf("oldest surviving id = %d, want 6", rows[0].ID)
	}
}

func TestGuildSettingsToggle(t *testing.T) {
	s := newTestStorage(t)
	if disabled, _ := s.IsGroupDisabled("g", "music"); disabled {
		t.Fatal("group should start enabled")
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := s.DisableGroup("g", "music"); err != nil {
			t.Fatal(err)
		}
	}
	if groups, _ := s.DisabledGroups("g"); len(groups) != 1 {
		t.Fatalf("disabled groups = %v, want one entry", groups)
	}
	if err := s.EnableGroup("g", "music"); err != nil {
		t.Fatal(err)
	}
	if disabled, _ := s.IsGroupDisabled("g", "music"); disabled {
		t.Fatal("group should be enabled again")
	}
}
