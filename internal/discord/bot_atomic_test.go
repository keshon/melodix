package discord

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestSessionCtxAtomicValue_HolderTypeIsStable(t *testing.T) {
	var v atomic.Value

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic on first Store: %v", r)
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		v.Store(&sessionCtxHolder{ctx: ctx})
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic on second Store: %v", r)
			}
		}()
		v.Store(&sessionCtxHolder{ctx: context.Background()})
	}()
}

