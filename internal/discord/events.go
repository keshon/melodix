package discord

type SystemEventType int

const (
	SystemEventRefreshCommands SystemEventType = iota
)

type SystemEvent struct {
	Type    SystemEventType
	GuildID string
	Target  string // "all" or a specific command name
}

var systemEvents = make(chan SystemEvent, 32)

// PublishSystemEvent sends an event to the system event bus.
func PublishSystemEvent(ev SystemEvent) {
	select {
	case systemEvents <- ev:
	default:
		// channel full, drop event
	}
}

// SystemEvents returns the channel for system events.
func SystemEvents() <-chan SystemEvent {
	return systemEvents
}
