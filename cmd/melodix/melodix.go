package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/keshon/melodix/internal/version"
	"github.com/keshon/melodix/player"
	songpkg "github.com/keshon/melodix/song"
	"github.com/keshon/melodix/storage"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

type Command struct {
	Name        string   // Primary command name
	Aliases     []string // List of alias names
	Description string
	Category    string
}

var commands = []Command{
	{"play", []string{"p", ">"}, "Play a song or add it to the queue", "Playback"},
	{"skip", []string{"next", "ff", ">>"}, "Skip the current song", "Playback"},
	{"stop", []string{"s", "x"}, "Stop the music and clear the queue", "Playback"},

	{"list", []string{"queue", "l", "q"}, "Show the current music queue", "Advanced Playback"},
	{"resume", nil, "Resume the current song", "Advanced Playback"},
	{"pause", nil, "Pause the current song", "Advanced Playback"},

	{"now", []string{"n"}, "Display the currently playing song", "Information"},
	{"tracks", nil, "Display music tracks history", "Information"},
	{"log", []string{"history", "time", "t"}, "Display command history", "Information"},

	{"ping", []string{"pong"}, "Check if the bot is responsive", "Utility"},
	{"melodix-set-prefix", nil, "Set a custom command prefix", "Utility"},

	{"about", nil, "Information about the bot", "General"},
	{"help", []string{"h", "?"}, "Show this help message", "General"},
}

type Bot struct {
	Session *discordgo.Session
	Storage *storage.Storage
	Player  *player.Player
	prefix  string
}

func NewBot(token string) (*Bot, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	s, err := storage.New("datastore.json")
	if err != nil {
		return nil, fmt.Errorf("error creating DataStore: %w", err)
	}

	return &Bot{
		Session: dg,
		Storage: s,
		Player:  player.New(dg, s),
		prefix:  "!",
	}, nil
}

// Lifecycle Methods
func (b *Bot) Start() error {
	b.configureIntents()
	b.registerEventHandlers()

	if err := b.openConnection(); err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown() {
	b.Player.Signals <- player.ActionStop
	if err := b.Session.Close(); err != nil {
		log.Println("Error closing connection:", err)
	}
}

func (b *Bot) openConnection() error {
	return b.Session.Open()
}

// Configuration Methods
func (b *Bot) configureIntents() {
	b.Session.Identify.Intents = discordgo.IntentsAll
}

func (b *Bot) registerEventHandlers() {
	b.Session.AddHandler(b.onReady)
	b.Session.AddHandler(b.onMessageCreate)
}

// Event Handlers
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	botInfo, err := s.User("@me")
	if err != nil {
		log.Println("Warning: Error retrieving bot user:", err)
		return
	}
	fmt.Printf("Bot %v is up and running!\n", botInfo.Username)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	commandName, param, err := b.extractCommand(m.Content, b.prefix)
	if err != nil {
		return
	}

	switch commandName {
	case "melodix-set-prefix":
		b.prefix = param
		s.ChannelMessageSend(m.ChannelID, "Prefix changed to "+param)
		return
	}

	cmd := b.getAliasedCommand(commandName)
	if cmd == nil {
		return // Command not found
	}

	err = b.saveCommandHistory(m.GuildID, m.ChannelID, m.Author.ID, m.Author.Username, cmd.Name, param)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error saving command info: %v", err))
		return
	}

	switch cmd.Name {
	case "ping":
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	case "pong":
		s.ChannelMessageSend(m.ChannelID, "Ping!")
	case "about":
		title := "ℹ️ About Project"
		content := fmt.Sprintf("**%v** — %v", version.AppName, version.AppDescription)
		content = fmt.Sprintf("%v\n\nProject repository: https://github.com/keshon/melodix\n", content)

		buildDate := "unknown"
		if version.BuildDate != "" {
			t, err := time.Parse(time.RFC3339, version.BuildDate)
			if err == nil {
				buildDate = t.Format("2006-01-02")
			} else {
				buildDate = "invalid date"
			}
		}

		goVer := "unknown"
		if version.GoVersion != "" {
			goVer = version.GoVersion
		}

		imagePath := "./assets/about-banner.webp"
		imageBytes, err := os.Open(imagePath)
		if err != nil {
			slog.Error("Error opening image file:", err)
		}

		embedMsg := embed.NewEmbed().
			SetDescription(fmt.Sprintf("%v\n\n%v\n\n", title, content)).
			AddField("```"+buildDate+"```", "Build date").
			AddField("```"+goVer+"```", "Go version").
			AddField("```Innokentiy Sokolov```", "[Linkedin](https://www.linkedin.com/in/keshon), [GitHub](https://github.com/keshon), [Homepage](https://keshon.ru)").
			InlineAllFields().
			SetImage("attachment://" + filepath.Base(imagePath)).
			SetColor(0x9f00d4).MessageEmbed

		s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embedMsg},
			Files: []*discordgo.File{
				{
					Name:   filepath.Base(imagePath),
					Reader: imageBytes,
				},
			},
		})
	case "play":
		voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
		if err != nil || voiceState.ChannelID == "" {
			s.ChannelMessageSend(m.ChannelID, "You must be in a voice channel to use this command.")
			return
		}
		songs, err := b.fetchSongs(param)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting song: %v", err))
			return
		}
		if len(songs) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No song found.")
			return
		}
		for _, song := range songs {
			fmt.Printf("%v - %v (%v)\n", song.Title, song.PublicLink, song.SongID)
		}
		b.Player.Queue = append(b.Player.Queue, songs...)
		b.Player.GuildID = m.GuildID
		b.Player.ChannelID = voiceState.ChannelID
		b.Player.Play()
	case "stop":
		b.Player.Signals <- player.ActionStop
	case "skip":
		b.Player.Signals <- player.ActionSkip
	case "pause", "resume":
		b.Player.Signals <- player.ActionPauseResume
	case "list":
		songs := b.Player.Queue
		if len(songs) == 0 {
			s.ChannelMessageSend(m.ChannelID, "Queue is empty.")
			return
		}
		var songList strings.Builder
		for i, song := range songs {
			if song.PublicLink != "" {
				fmt.Fprintf(&songList, "%d. [%s](%s)\n", i+1, song.Title, song.PublicLink)
			} else {
				fmt.Fprintf(&songList, "%d. %s\n", i+1, song.Title)
			}
		}
		s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(songList.String()))
	case "now":
		if b.Player.Song != nil {
			title, source, url, err := b.Player.Song.GetInfo(b.Player.Song)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting song info: %v", err))
			}
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Now playing from %s:\n[%s](%s)", source, title, url))
			return
		}
		s.ChannelMessageSend(m.ChannelID, "No song is currently playing.")
	case "help":
		categoryOrder := []string{"Playback", "Advanced Playback", "Information", "Utility", "General"}
		categories := make(map[string][]Command)
		for _, cmd := range commands {
			categories[cmd.Category] = append(categories[cmd.Category], cmd)
		}
		var helpMsg strings.Builder
		for _, category := range categoryOrder {
			cmds, exists := categories[category]
			if !exists {
				continue
			}
			helpMsg.WriteString(fmt.Sprintf("**%s**\n", category))
			for _, cmd := range cmds {
				helpMsg.WriteString(fmt.Sprintf("  _%s_ - %s\n", b.prefix+cmd.Name, cmd.Description))
			}
			helpMsg.WriteString("\n")
		}
		s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(helpMsg.String()))

	case "log":
		list, err := b.Storage.FetchCommandHistory(m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting command history: %v", err))
			return
		}
		var logList strings.Builder
		for _, record := range list {
			if strings.HasPrefix(record.Command, "play") {
				username := fmt.Sprintf("@%s", record.Username)
				logList.WriteString(fmt.Sprintf("%s %s by %s\n", b.prefix+record.Command, record.Param, username))
			}
		}
		s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(logList.String()))
	case "tracks":
		list, err := b.Storage.FetchTrackHistory(m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting track history: %v", err))
			return
		}
		var tracksList strings.Builder
		for _, record := range list {
			totalDuration := time.Duration(record.TotalDuration * float64(time.Second))

			hours := int(totalDuration.Hours())
			minutes := int(totalDuration.Minutes()) % 60
			seconds := int(totalDuration.Seconds()) % 60

			durationFormatted := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
			tracksList.WriteString(fmt.Sprintf("%s - %s (%d plays, %s)\n", record.Title, record.PublicLink, record.TotalCount, durationFormatted))
		}
		s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(tracksList.String()))
	}
}

// Utility Methods
func (b *Bot) extractCommand(content, prefix string) (string, string, error) {
	lowerContent := strings.ToLower(content)
	if strings.HasPrefix(lowerContent, strings.ToLower(prefix)) {
		content = content[len(prefix):]
	} else if !strings.HasPrefix(lowerContent, "melodix-set-prefix") {
		return "", "", nil
	}

	words := strings.Fields(content)
	if len(words) == 0 {
		return "", "", fmt.Errorf("no command found")
	}

	command := strings.ToLower(words[0])
	parameter := ""
	if len(words) > 1 {
		parameter = strings.Join(words[1:], " ")
		parameter = strings.TrimSpace(parameter)
	}

	return command, parameter, nil
}

func (b *Bot) getAliasedCommand(input string) *Command {
	input = strings.ToLower(input)
	for _, cmd := range commands {
		if input == cmd.Name {
			return &cmd
		}
		for _, alias := range cmd.Aliases {
			if input == alias {
				return &cmd
			}
		}
	}
	return nil
}

func (b *Bot) saveCommandHistory(guildID, channelID, userID, username, command, param string) error {
	channel, err := b.Session.Channel(channelID)
	if err != nil {
		fmt.Println("Error retrieving channel:", err)
		return err
	}

	guild, err := b.Session.Guild(guildID)
	if err != nil {
		fmt.Println("Error retrieving guild:", err)
		return err
	}

	record := storage.CommandHistoryRecord{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		GuildName:   guild.Name,
		UserID:      userID,
		Username:    username,
		Command:     command,
		Param:       param,
		Datetime:    time.Now(),
	}

	b.Storage.AppendCommandToHistory(guildID, record)

	return nil
}

func (b *Bot) findUserVoiceState(guildID, userID string) (*discordgo.VoiceState, error) {
	guild, err := b.Session.State.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving guild: %w", err)
	}

	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return vs, nil
		}
	}
	return nil, fmt.Errorf("user not in any voice channel")
}

func (b *Bot) fetchSongs(input string) ([]*songpkg.Song, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("no song title or URL provided")
	}

	var songs []*songpkg.Song
	songFetcher := songpkg.New()

	if strings.Contains(input, "http://") || strings.Contains(input, "https://") {
		urls := strings.Fields(input)
		for _, url := range urls {
			song, err := songFetcher.FetchSongs(url)
			if err != nil {
				return nil, fmt.Errorf("error fetching songs from URL %s: %w", url, err)
			}
			songs = append(songs, song...)
		}
	} else {
		song, err := songFetcher.FetchSongs(input)
		if err != nil {
			return nil, fmt.Errorf("error fetching songs for title %q: %w", input, err)
		}
		songs = append(songs, song...)
	}

	if len(songs) == 0 {
		return nil, fmt.Errorf("no songs found for the provided input")
	}
	return songs, nil
}

func (b *Bot) truncatListWithNewlines(content string) string {
	if len(content) > 2000 {
		lines := strings.Split(content, "\n")
		var truncatedList strings.Builder
		for _, line := range lines {
			if truncatedList.Len()+len(line)+1 > 2000 { // +1 for newline character
				break
			}
			truncatedList.WriteString(line + "\n")
		}
		return truncatedList.String()
	}
	return content
}

// Global Functions
func loadEnv(path string) {
	if err := godotenv.Load(path); err != nil {
		log.Fatal("Error loading .env file")
	}

	if os.Getenv("DISCORD_TOKEN") == "" {
		log.Fatal("DISCORD_TOKEN is missing in environment variables")
	}
}

func main() {
	loadEnv("./.env") // full path is needed for VStudio Debugging

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Discord token not found in environment variables")
	}

	bot, err := NewBot(token)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}
	defer bot.Shutdown()

	if err := bot.Start(); err != nil {
		log.Fatal("Failed to start bot:", err)
	}

	fmt.Println("Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	close(sc)
}
