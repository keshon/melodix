package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/keshon/melodix/pkg/music/cache"
	"github.com/rs/zerolog"
)

func TestCacheIndexRoundTripAndRecordsSkip(t *testing.T) {
	st, err := NewStorage(context.Background(), filepath.Join(t.TempDir(), "ds.json"), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	idx := st.CacheIndex()

	if m, err := idx.Load(); err != nil || len(m) != 0 {
		t.Fatalf("empty Load = (%v,%v), want (empty,nil)", m, err)
	}

	in := map[string]cache.Entry{"youtube:x": {Key: "youtube:x", File: "abc.mxo", Bytes: 10}}
	if err := idx.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := idx.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out["youtube:x"].File != "abc.mxo" || out["youtube:x"].Bytes != 10 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}

	// A guild record coexists; Records() returns it but skips the reserved key.
	if err := st.SetCommand("guild1", "c", "cn", "gn", "u", "un", "/play"); err != nil {
		t.Fatalf("SetCommand: %v", err)
	}
	recs := st.Records()
	if _, ok := recs[cacheIndexKey]; ok {
		t.Fatal("Records() must skip the reserved cache-index key")
	}
	if _, ok := recs["guild1"]; !ok {
		t.Fatal("Records() should include the guild record")
	}
}
