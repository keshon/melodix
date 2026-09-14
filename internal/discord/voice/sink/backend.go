package sink

// Backend names the library that carries the audio path. Everything above it
// — commands, REST, interactions, gateway — is discordgo whichever is chosen.
type Backend string

const (
	// BackendDiscordgo is the vendored discordgo fork and its own DAVE
	// implementation, in pkg/discordgo-fork-dev.
	BackendDiscordgo Backend = "discordgo"
	// BackendDisgo is disgo's voice stack with dave-go's E2EE, bridged onto
	// the discordgo gateway by DisgoVoice.
	BackendDisgo Backend = "disgo"
)

// ParseBackend resolves a configured value, reporting whether it was
// recognised. An unrecognised one resolves to discordgo: this setting only
// chooses how the audio is carried, and refusing to start a whole bot over a
// typo in it would cost more than the surprise of the default does. The caller
// logs the miss.
func ParseBackend(s string) (Backend, bool) {
	switch Backend(s) {
	case BackendDiscordgo, BackendDisgo:
		return Backend(s), true
	default:
		return BackendDiscordgo, false
	}
}
