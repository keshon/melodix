package voicesink

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// A Provider lives as long as the player that holds it, which is the life of
// the process. A gateway session does not: its voice manager belongs to one
// bot.Client and closes over that client's gateway, and its DAVE registry is
// built fresh for each one.
//
// Storing either was the defect. After a single reconnect the guild joined
// through a manager whose gateway was shut, which fails instantly and forever,
// and no sink invalidation helped because what had gone stale was the thing
// that makes connections rather than a connection. The fix was to resolve them
// per acquisition; this is what keeps it fixed, because the tempting shape --
// "resolve once in the constructor and keep it" -- looks like a tidy-up and
// reads almost identically.
func TestAProviderStoresNothingThatDiesWithTheSession(t *testing.T) {
	sessionScoped := map[string]string{
		"voice.Manager":    "belongs to one bot.Client and closes over its gateway",
		"*DaveRegistry":    "built fresh per session in RunSession",
		"DaveRegistry":     "built fresh per session in RunSession",
		"*bot.Client":      "is the session",
		"*session.Session": "is the session",
	}

	fields := reflect.TypeOf(Provider{})
	for i := 0; i < fields.NumField(); i++ {
		f := fields.Field(i)
		name := f.Type.String()
		// reflect spells these with their package path; compare on the tail.
		short := name[strings.LastIndex(name, "/")+1:]
		if why, bad := sessionScoped[short]; bad {
			t.Errorf("Provider.%s is a %s, which %s -- resolve it per acquisition instead",
				f.Name, short, why)
		}
	}
}

// The one connection-shaped thing a Provider does keep is the connection it
// opened, and it keeps it to recognise it again rather than to read state off.
// Whether that connection is still the guild's is a question only the voice
// manager can answer -- disgo removes one on a voice websocket close it cannot
// resume from, and on closing the client, and reports neither.
//
// Both paths have to ask. Acquiring without asking hands the next track a sink
// over a dead socket; releasing without asking spends the close budget waiting
// on a gateway that is not there, on whichever worker ran /stop.
func TestEveryPathThatDecidesLivenessAsksTheManager(t *testing.T) {
	src, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("reading provider.go: %v", err)
	}
	text := string(src)

	for _, fn := range []string{"func (p *Provider) Sink(", "func (p *Provider) release("} {
		start := strings.Index(text, fn)
		if start < 0 {
			t.Fatalf("%s not found; this check is now guarding nothing", fn)
		}
		body := text[start:]
		if end := strings.Index(body, "\n}\n"); end > 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "manager.GetConn(p.guildID)") {
			t.Errorf("%s decides liveness without asking the manager", strings.TrimPrefix(fn, "func (p *Provider) "))
		}
	}
}
