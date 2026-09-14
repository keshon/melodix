package disgosession

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/rs/zerolog"
)

// CheckResult is what a connection check learned.
type CheckResult struct {
	Username string
	Guilds   int
	Latency  time.Duration
	// Commands is how many application commands the first guild already has,
	// read rather than written. It proves REST works and that the
	// application id resolved, which are the two things a compile cannot.
	Commands int
	// CommandsGuild is the guild those commands were counted in.
	CommandsGuild string
}

// Check opens a disgo gateway with the real token, waits for READY, reads back
// what it can see, and disconnects. It registers nothing and sends nothing.
//
// This exists because everything else in the migration is a rewrite that only
// a live run can judge. A compile proves the disgo code type-checks against
// disgo; it proves nothing about the token, the intents, whether READY
// arrives, or whether REST authenticates -- and those are exactly the failures
// that would otherwise surface for the first time after the whole command
// layer had been ported onto them.
func Check(ctx context.Context, token string, log zerolog.Logger) (CheckResult, error) {
	var result CheckResult

	ready := make(chan *events.Ready, 1)
	s, err := New(Options{
		Token: token,
		Log:   log,
		Listeners: []bot.EventListener{
			bot.NewListenerFunc(func(e *events.Ready) {
				select {
				case ready <- e:
				default:
				}
			}),
		},
	})
	if err != nil {
		return result, err
	}

	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.Open(openCtx); err != nil {
		return result, err
	}
	defer func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelClose()
		s.Close(closeCtx)
	}()

	select {
	case <-openCtx.Done():
		return result, fmt.Errorf("gateway opened but READY did not arrive within 30s")
	case e := <-ready:
		result.Username = e.User.Username
		result.Guilds = len(e.Guilds)
	}

	client := s.Client()

	// Latency needs a heartbeat round trip, which has not necessarily
	// happened by READY. Give it a moment rather than reporting a zero that
	// looks like a failure.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if result.Latency = s.Latency(); result.Latency > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// A read-only REST call. It confirms the token authenticates for REST as
	// well as the gateway, and that ApplicationID resolved -- cmdsync needs
	// both, and under discordgo the id came from a User("@me") fallback that
	// disgo makes unnecessary.
	for guild := range client.Caches.Guilds() {
		cmds, err := client.Rest.GetGuildCommands(client.ApplicationID, guild.ID, false)
		if err != nil {
			return result, fmt.Errorf("reading guild commands: %w", err)
		}
		result.Commands = len(cmds)
		result.CommandsGuild = guild.ID.String()
		break
	}

	return result, nil
}
