package cmdadapter

// GuildInfo is what a command can learn about a guild without naming the
// library that fetched it.
//
// The counts are whatever the session could see: a cached guild reports the
// members it has cached rather than the guild's true size, which is what the
// status command has always shown. Reporting it as a plain int keeps that
// honest — a command cannot mistake this for a figure it could not have.
type GuildInfo struct {
	ID       string
	Name     string
	Members  int
	Roles    int
	Channels int
}
