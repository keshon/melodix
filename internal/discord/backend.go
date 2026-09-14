package discord

// Backend names the library carrying a connection. Two settings use it:
// DISCORD_BACKEND for the gateway, REST and interactions, and VOICE_BACKEND
// for the audio path. They are independent -- disgo's voice stack runs on
// discordgo's gateway, which is what voice-disgo-spike proved.
//
// Both are scaffolding. They go when pkg/discordgo-fork-dev does, the same way
// DAVE_BACKEND went when the hand-rolled MLS did.
type Backend string

const (
	// BackendDiscordgo is the vendored fork in pkg/discordgo-fork-dev.
	BackendDiscordgo Backend = "discordgo"
	// BackendDisgo is disgo.
	BackendDisgo Backend = "disgo"
)

// ParseBackend resolves a configured value, reporting whether it was
// recognised. An unrecognised one resolves to discordgo: this chooses which
// implementation carries a connection, and refusing to start a whole bot over
// a typo in it would cost more than the surprise of the default does. The
// caller logs the miss.
func ParseBackend(s string) (Backend, bool) {
	switch Backend(s) {
	case BackendDiscordgo, BackendDisgo:
		return Backend(s), true
	default:
		return BackendDiscordgo, false
	}
}
