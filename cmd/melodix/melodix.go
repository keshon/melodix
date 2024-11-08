package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/keshon/melodix/player"
	songpkg "github.com/keshon/melodix/song"
	"github.com/keshon/melodix/storage"
	"github.com/keshon/melodix/youtube"
)

var commandAliases = [][]string{
	{"ping"},
	{"pong"},
	{"about"},
	{"pause", "resume"},
	{"play", "p"},
	{"stop", "s"},
	{"list", "queue", "l", "q"},
	{"add", "a", "+"},
	{"skip", "next", "ff", ">>"},
	{"history", "time", "t"},
	{"now", "n"},
}

var youtubeutil youtube.YouTube
var youtubeutilOnce sync.Once

var song songpkg.Song
var songOnce sync.Once

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

	command, param, err := b.extractCommand(m.Content, b.prefix)
	if err != nil {
		return
	}

	switch command {
	case "melodix-set-prefix":
		b.prefix = param
		s.ChannelMessageSend(m.ChannelID, "Prefix changed to "+param)
		return
	}

	canonical := b.getAliasedCommand(command, commandAliases)
	if len(canonical) == 0 {
		return
	}

	err = b.saveCommandHistory(m.GuildID, m.ChannelID, canonical, param)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error saving command info: %v", err))
		return
	}

	switch canonical {
	case "ping":
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	case "pong":
		s.ChannelMessageSend(m.ChannelID, "Ping!")
	case "about":
		s.ChannelMessageSend(m.ChannelID, "A new prototype of Melodix Player 2")
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

		b.Player.Queue = append(b.Player.Queue, songs...)
		b.Player.GuildID = m.GuildID
		b.Player.ChannelID = voiceState.ChannelID
		b.Player.Play(nil, 0)
	case "stop":
		b.Player.Signals <- player.ActionStop
	case "skip":
		b.Player.Signals <- player.ActionSkip
	case "pause", "!resume":
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

		s.ChannelMessageSend(m.ChannelID, songList.String())
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

func (b *Bot) getAliasedCommand(command string, aliases [][]string) string {
	lowerCommand := strings.ToLower(command)
	for _, aliasesPerCommand := range aliases {
		for _, alias := range aliasesPerCommand {
			if strings.ToLower(alias) == lowerCommand {
				return strings.ToLower(aliasesPerCommand[0])
			}
		}
	}
	return ""
}

func (b *Bot) saveCommandHistory(guildID, channelID, command, param string) error {
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

func (b *Bot) fetchSongs(param string) ([]*songpkg.Song, error) {
	param = strings.TrimSpace(param)
	if param == "" {
		return nil, fmt.Errorf("no song title or URL provided")
	}

	urls := strings.Fields(param)
	songs := make([]*songpkg.Song, 0, len(urls))

	youtubeutilOnce.Do(func() {
		youtubeutil = *youtube.New()
	})

	songOnce.Do(func() {
		song = *songpkg.New()
	})

	for _, url := range urls {
		var song *songpkg.Song
		var err error

		switch {
		case isYoutubeURL(url):
			song, err = song.GetYoutubeSong(url)
		case isInternetRadioURL(url):
			song, err = song.GetInternetRadioSong(url)
		case isMP3(url):
			song, err = song.GetLocalFileSong(url)
		default:
			var videoURL string
			videoURL, err = youtubeutil.GetVideoURLByTitle(url)
			if err == nil {
				song, err = song.GetYoutubeSong(videoURL)
			}
		}

		if err != nil {
			return nil, fmt.Errorf("error fetching song for %s: %w", url, err)
		}
		songs = append(songs, song)
	}

	if len(songs) == 0 {
		return nil, fmt.Errorf("no songs found")
	}
	return songs, nil
}

func isYoutubeURL(url string) bool {
	youtubeRegex := regexp.MustCompile(`^(https?://)?(www\.)?(m\.)?(music\.)?(youtube\.com|youtu\.be)(/|$)`)
	return youtubeRegex.MatchString(url)
}

func isInternetRadioURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") && !isYoutubeURL(url)
}

func isMP3(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".mp3")
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
	loadEnv("d:\\Projects\\dev\\Keshon\\melodix\\.env") // full path is needed for VStudio Debugging

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
