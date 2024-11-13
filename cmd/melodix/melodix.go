package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
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

const embedColor = 0x9f00d4

var commands = []Command{
	{"play", []string{"p", ">"}, "Play a song or add it to the queue", "Playback"},
	{"skip", []string{"next", "ff", ">>"}, "Skip the current song", "Playback"},
	{"stop", []string{"s", "x"}, "Stop the music and clear the queue", "Playback"},

	{"list", []string{"queue", "l", "q"}, "Show the current music queue", "Advanced Playback"},
	{"resume", nil, "Resume the current song", "Advanced Playback"},
	{"pause", nil, "Pause the current song", "Advanced Playback"},

	{"now", []string{"n"}, "Display the currently playing song", "Information"},
	{"stats", []string{"tracks"}, "Display recent playback stats", "Information"},
	{"log", []string{"history", "time", "t"}, "Display recent playback history", "Information"},

	{"ping", nil, "Check if the bot is responsive", "Utility"},
	{"set-prefix", nil, "Set a custom command prefix", "Utility"},
	{"melodix-reset-prefix", nil, "Reset the command prefix to the default `!`", "Utility"},

	{"about", nil, "About the bot", "General"},
	{"help", []string{"h", "?"}, "Show this help message", "General"},
}

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Category    string
}

type Bot struct {
	session       *discordgo.Session
	storage       *storage.Storage
	players       map[string]*player.Player
	prefixCache   map[string]string
	defaultPrefix string
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
		session:       dg,
		storage:       s,
		players:       make(map[string]*player.Player),
		prefixCache:   make(map[string]string),
		defaultPrefix: "!",
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
	for _, instance := range b.players {
		instance.ActionSignals <- player.ActionStop
	}
	if err := b.session.Close(); err != nil {
		log.Println("Error closing connection:", err)
	}
}

func (b *Bot) openConnection() error {
	return b.session.Open()
}

// Configuration Methods
func (b *Bot) configureIntents() {
	b.session.Identify.Intents = discordgo.IntentsAll
}

func (b *Bot) registerEventHandlers() {
	b.session.AddHandler(b.onReady)
	b.session.AddHandler(b.onMessageCreate)
	b.session.AddHandler(b.onPlayback)
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

	prefix, err := b.getPrefixForGuild(m.GuildID)
	if err != nil {
		log.Printf("Error fetching prefix for guild %s: %v", m.GuildID, err)
		prefix = b.defaultPrefix
	}

	command, rawCommand, param, err := b.extractCommand(m.Content, prefix)
	if err != nil {
		return
	}

	switch rawCommand {
	case "melodix-reset-prefix":
		b.prefixCache[m.GuildID] = b.defaultPrefix
		b.storage.SavePrefix(m.GuildID, b.defaultPrefix)
		s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Prefix reset to `"+b.defaultPrefix+"`\nUse `"+b.defaultPrefix+"help` for a list of commands.").MessageEmbed)
		return
	}

	cmd := b.getAliasedCommand(command)
	if cmd == nil {
		return
	}

	err = b.saveCommandHistory(m.GuildID, m.ChannelID, m.Author.ID, m.Author.Username, cmd.Name, param)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error saving command info: %v", err))
		return
	}

	switch cmd.Name {
	case "ping":
		s.ChannelMessageSendEmbed(m.ChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("Pong!").MessageEmbed)
	case "about":
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
			fmt.Printf("Error opening image: %v", err)
		}
		emb := embed.NewEmbed().
			SetColor(embedColor).
			SetDescription(fmt.Sprintf("%v\n\n%v — %v", "ℹ️ About", version.AppName, version.AppDescription)).
			AddField("Made by Innokentiy Sokolov", "[Linkedin](https://www.linkedin.com/in/keshon), [GitHub](https://github.com/keshon), [Homepage](https://keshon.ru)").
			AddField("Repository", "https://github.com/keshon/melodix").
			AddField("Release:", buildDate+" (go version "+strings.TrimLeft(goVer, "go")+")").
			SetImage("attachment://" + filepath.Base(imagePath)).
			MessageEmbed
		s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{emb},
			Files: []*discordgo.File{
				{
					Name:   filepath.Base(imagePath),
					Reader: imageBytes,
				},
			},
		})
	case "play":
		voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
		emb := embed.NewEmbed().SetColor(embedColor)
		if err != nil || voiceState.ChannelID == "" {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("You must be in a voice channel to use this command.").MessageEmbed)
			return
		}
		songs, err := b.fetchSongs(param)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription(fmt.Sprintf("Error getting song: %v", err)).MessageEmbed)
			return
		}
		if len(songs) == 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("No song found.").MessageEmbed)
			return
		}
		for _, song := range songs {
			fmt.Printf("%v - %v (%v)\n", song.Title, song.PublicLink, song.SongID)
		}
		instance := b.getOrCreatePlayer(m.GuildID)
		instance.Queue = append(instance.Queue, songs...)
		instance.GuildID = m.GuildID
		instance.ChannelID = voiceState.ChannelID
		instance.Play()
	case "now":
		instance := b.getOrCreatePlayer(m.GuildID)
		if instance.Song != nil {
			emb := embed.NewEmbed().SetColor(embedColor)
			emb.SetDescription(fmt.Sprintf("%s Now playing\n\n**%s**\n[%s](%s)", player.StatusPlaying.StringEmoji(), instance.Song.Title, instance.Song.Source, instance.Song.PublicLink))
			if len(instance.Song.Thumbnail.URL) > 0 {
				emb.SetThumbnail(instance.Song.Thumbnail.URL)
			}
			emb.SetFooter(fmt.Sprintf("Use %shelp for a list of commands.", b.prefixCache[m.GuildID]))
			s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
			return
		}
		s.ChannelMessageSend(m.ChannelID, "No song is currently playing.")
	case "stop":
		instance := b.getOrCreatePlayer(m.GuildID)
		instance.ActionSignals <- player.ActionStop
	case "skip":
		instance := b.getOrCreatePlayer(m.GuildID)
		instance.ActionSignals <- player.ActionSkip
	case "pause", "resume":
		instance := b.getOrCreatePlayer(m.GuildID)
		voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
		if err != nil || voiceState.ChannelID == "" {
			emb := embed.NewEmbed().SetColor(embedColor)
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("You must be in a voice channel to use this command.").MessageEmbed)
			return
		}
		if instance.ChannelID != voiceState.ChannelID {
			instance.ChannelID = voiceState.ChannelID
			instance.ActionSignals <- player.ActionSwap
		} else {
			instance.ActionSignals <- player.ActionPauseResume
		}
	case "list":
		instance := b.getOrCreatePlayer(m.GuildID)
		songs := instance.Queue
		emb := embed.NewEmbed().SetColor(embedColor).SetDescription("Queue")
		if len(songs) == 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Queue is empty.").MessageEmbed)
			return
		}
		for index, song := range songs {
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("%d. %s", index+1, song.Title),
				Value:  fmt.Sprintf("[%s](%s)", song.Title, song.PublicLink),
				Inline: false,
			})
		}
		s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
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
				if cmd.Name != "melodix-reset-prefix" {
					cmd.Name = prefix + cmd.Name
				}

				helpMsg.WriteString(fmt.Sprintf("  _%s_ - %s\n", cmd.Name, cmd.Description))
			}
			helpMsg.WriteString("\n")
		}
		s.ChannelMessageSend(m.ChannelID, b.truncatListWithNewlines(helpMsg.String()))
	case "log":
		emb := embed.NewEmbed().SetColor(embedColor)
		records, err := b.storage.FetchCommandHistory(m.GuildID)
		if err != nil {
			emb.SetDescription(fmt.Sprintf("Error getting command history: %v", err))
			s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
			return
		}
		for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
			records[i], records[j] = records[j], records[i]
		}
		emb.SetDescription("Play History")
		for _, command := range records {
			if command.Command != "play" {
				continue
			}
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("%s - %s", command.Datetime.Format("2006.01.02 15:04:05"), command.Username),
				Value:  command.Param,
				Inline: false,
			})
		}
		s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
	case "stats":
		records, err := b.storage.FetchTrackHistory(m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting track history: %v", err))
			return
		}
		switch param {
		case "count":
			sort.Slice(records, func(i, j int) bool {
				return records[i].TotalCount > records[j].TotalCount
			})
		case "date":
			sort.Slice(records, func(i, j int) bool {
				return records[i].LastPlayed.After(records[j].LastPlayed)
			})
		default:
			sort.Slice(records, func(i, j int) bool {
				return records[i].TotalDuration > records[j].TotalDuration
			})
		}
		emb := embed.NewEmbed().SetColor(embedColor).SetDescription("Tracks Statistics").MessageEmbed
		for index, track := range records {
			totalDuration := time.Duration(track.TotalDuration * float64(time.Second))

			hours := int(totalDuration.Hours())
			minutes := int(totalDuration.Minutes()) % 60
			seconds := int(totalDuration.Seconds()) % 60

			durationFormatted := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
			emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("%d. %s", index+1, track.Title),
				Value:  fmt.Sprintf("`%s`\t`x%v`\t[%s](%s)", durationFormatted, track.TotalCount, track.SourceType, track.PublicLink),
				Inline: false,
			})
		}
		s.ChannelMessageSendEmbed(m.ChannelID, emb)
	case "set-prefix":
		emb := embed.NewEmbed().SetColor(embedColor)
		if len(param) == 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Please provide a prefix.").MessageEmbed)
			return
		}
		b.prefixCache[m.GuildID] = param
		err := b.storage.SavePrefix(m.GuildID, param)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Error saving new prefix.").MessageEmbed)
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Prefix changed to `"+param+"`\nUse `"+param+"help` for a list of commands.").MessageEmbed)
	}
}

func (b *Bot) onPlayback(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	go func() {
		instance := b.getOrCreatePlayer(m.GuildID)
		signal := <-instance.StatusSignals
		if signal == player.StatusPlaying {
			if instance.Song != nil {
				emb := embed.NewEmbed().SetColor(embedColor)
				emb.SetDescription(fmt.Sprintf("%s Now playing\n\n**%s**\n[%s](%s)", player.StatusPlaying.StringEmoji(), instance.Song.Title, instance.Song.Source, instance.Song.PublicLink))
				if len(instance.Song.Thumbnail.URL) > 0 {
					emb.SetThumbnail(instance.Song.Thumbnail.URL)
				}
				emb.SetFooter(fmt.Sprintf("Use %shelp for a list of commands.", b.prefixCache[m.GuildID]))
				s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
				return
			}
			s.ChannelMessageSend(m.ChannelID, "No song is currently playing.")
		}
	}()
}

// Utility Methods
func (b *Bot) getOrCreatePlayer(guildID string) *player.Player {
	if player, exists := b.players[guildID]; exists {
		return player
	}

	newPlayer := player.New(b.session, b.storage)
	b.players[guildID] = newPlayer
	return newPlayer
}

func (b *Bot) getPrefixForGuild(guildID string) (string, error) {
	if prefix, exists := b.prefixCache[guildID]; exists {
		return prefix, nil
	}

	prefix, err := b.storage.FetchPrefix(guildID)
	if err != nil || prefix == "" {
		prefix = b.defaultPrefix // Use default if there's an error or no prefix is set
	}

	b.prefixCache[guildID] = prefix
	return prefix, nil
}

func (b *Bot) extractCommand(content, prefix string) (string, string, string, error) {
	lowerContent := strings.ToLower(content)
	if strings.HasPrefix(lowerContent, strings.ToLower(prefix)) {
		content = content[len(prefix):]
	} else if !strings.HasPrefix(lowerContent, "melodix-set-prefix") {
		return "", content, "", nil
	}

	words := strings.Fields(content)
	if len(words) == 0 {
		return "", "", "", fmt.Errorf("no command found")
	}

	command := strings.ToLower(words[0])
	parameter := ""
	if len(words) > 1 {
		parameter = strings.Join(words[1:], " ")
		parameter = strings.TrimSpace(parameter)
	}

	return command, "", parameter, nil
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
	channel, err := b.session.Channel(channelID)
	if err != nil {
		fmt.Println("Error retrieving channel:", err)
		return err
	}

	guild, err := b.session.Guild(guildID)
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

	b.storage.AppendCommandToHistory(guildID, record)

	return nil
}

func (b *Bot) findUserVoiceState(guildID, userID string) (*discordgo.VoiceState, error) {
	guild, err := b.session.State.Guild(guildID)
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
