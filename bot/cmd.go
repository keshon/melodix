package bot

import (
	"github.com/bwmarrin/discordgo"
)

const embedColor = 0x9f00d4

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Category    string
}

var commands = []Command{
	{"play", []string{"p", ">"}, "Play a song or add it to the queue. Use `play fast ...` or `play slow ...` to manually select a parser", "Playback"},
	{"skip", []string{"next", "ff", ">>"}, "Skip the current song", "Playback"},
	{"stop", []string{"s", "x"}, "Stop the music and clear the queue", "Playback"},

	{"list", []string{"queue", "l", "q"}, "Show the current music queue", "Advanced Playback"},
	{"resume", nil, "Resume the current song", "Advanced Playback"},
	{"pause", nil, "Pause the current song", "Advanced Playback"},

	{"now", []string{"n"}, "Display the currently playing song", "Information"},
	{"stats", []string{"tracks"}, "Display recent playback stats", "Information"},
	{"log", []string{"history", "time", "t"}, "Display recent playback history", "Information"},

	{"ping", nil, "Check if the bot is responsive", "Utility"},
	{"cache", nil, "Enable/disable caching during playback (`cache on/off`)", "Utility"},
	{"set-prefix", nil, "Set a custom command prefix", "Utility"},
	{"melodix-reset-prefix", nil, "Reset the command prefix to the default `!`", "Utility"},

	{"about", nil, "About the bot", "General"},
	{"help", []string{"h", "?"}, "Show this help message", "General"},
}

type CommandHandler func(s *discordgo.Session, m *discordgo.MessageCreate, bot *Bot, command, param string)

var commandRegistry = map[string]CommandHandler{}

func registerCommand(name string, handler CommandHandler) {
	commandRegistry[name] = handler
}

func executeCommand(s *discordgo.Session, m *discordgo.MessageCreate, bot *Bot, name string, param string) {
	if handler, exists := commandRegistry[name]; exists {
		handler(s, m, bot, name, param)
	}
}
