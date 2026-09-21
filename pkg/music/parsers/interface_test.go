package parsers

import (
	"reflect"
	"testing"
)

// A parser is handed the track by value, so it has nothing to write through:
// what it learns comes back in Opened, and the stream decides what to do with
// it. Five parser packages used to write their findings into a *Track that
// belonged to whoever called them. This pins the shape; the compiler then
// holds every implementation to it.
func TestStreamerOpenTakesTheTrackByValue(t *testing.T) {
	open, ok := reflect.TypeOf((*Streamer)(nil)).Elem().MethodByName("Open")
	if !ok {
		t.Fatal("Streamer has no Open method")
	}
	for i := 0; i < open.Type.NumIn(); i++ {
		if in := open.Type.In(i); in.Kind() == reflect.Pointer && in.Elem() == reflect.TypeOf(Track{}) {
			t.Fatalf("Streamer.Open takes %s: a parser can write into the caller's track", in)
		}
	}
}
