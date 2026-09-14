package cmdadapter

// Invoker is who ran a command and where, resolved once when the context is
// built rather than read back off a wire event by every accessor.
//
// Holding it as data is what lets the contexts carry no library value at all.
// The five accessors below used to reach into an interaction to answer four
// questions whose answers never change during an invocation, which meant every
// context had to keep the event that could answer them -- and a command that
// took the event instead of asking the question was one line away at all
// times. That is how phase 1 reported zero references while two dozen call
// sites still held a *discordgo.Session.
type Invoker struct {
	// GuildID is empty in a direct message.
	GuildID   string
	ChannelID string
	// UserID is UnknownUserID when the invocation carried no caller.
	UserID string
	// Username is unknownUsername, or the user ID for a reaction, when the
	// invocation carried no member.
	Username string
}
