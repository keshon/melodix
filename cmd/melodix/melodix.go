package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/keshon/melodix/datastore"
	"github.com/keshon/melodix/player"
	"github.com/keshon/melodix/song"
)

type Bot struct {
	Session   *discordgo.Session
	DataStore *datastore.DataStore
	Player    *player.Player
}

type Record struct {
	GuildName string `json:"guild_name"`
}

func NewBot(token string) (*Bot, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	ds, err := datastore.New("datastore.json")
	if err != nil {
		return nil, fmt.Errorf("error creating DataStore: %w", err)
	}
	return &Bot{Session: dg, DataStore: ds, Player: player.New(dg)}, nil
}

// Lifecycle Methods
func (b *Bot) Start() error {
	b.configureIntents()
	b.registerEventHandlers()

	if err := b.openConnection(); err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	if err := b.loadGuildInfo(); err != nil {
		return fmt.Errorf("error loading guild info: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown() {
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
		log.Fatalf("Error retrieving bot user: %v", err)
	}
	fmt.Printf("Bot %v is up and running!\n", botInfo.Username)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	command, param, err := b.parseCommand(m.Content)
	if err != nil {
		return
	}

	switch command {
	case "!ping":
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	case "!pong":
		s.ChannelMessageSend(m.ChannelID, "Ping!")
	case "!info":
		record, exists := b.DataStore.Get(m.GuildID)
		if exists {
			s.ChannelMessageSend(m.ChannelID, record.(*Record).GuildName)
		}
	case "!play":
		voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
		if err != nil || voiceState.ChannelID == "" {
			s.ChannelMessageSend(m.ChannelID, "You must be in a voice channel to use this command.")
			return
		}

		song := song.New()
		trackURL, err := url.Parse(param)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error parsing URL: %v", err))
			return
		}
		track, err := song.GetYoutubeSong(trackURL.String())
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting song: %v", err))
			return
		}
		b.Player.GuildID = m.GuildID
		b.Player.ChannelID = voiceState.ChannelID
		b.Player.Play(track, 0) // `param` has a link to YouTube
	}
}

// Utility Methods
func (b *Bot) parseCommand(content string) (string, string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", fmt.Errorf("no command found")
	}

	words := strings.Fields(content)
	command := words[0]
	parameter := strings.Join(words[1:], " ")

	return command, strings.TrimSpace(parameter), nil
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

func (b *Bot) loadGuildInfo() error {
	if len(b.Session.State.Guilds) == 0 {
		return fmt.Errorf("no guilds available for the bot")
	}

	guild := b.Session.State.Guilds[0]
	if guildInfo, err := b.Session.Guild(guild.ID); err == nil {
		b.DataStore.Add(guildInfo.ID, &Record{GuildName: guildInfo.Name})
	} else {
		return fmt.Errorf("error getting guild name: %w", err)
	}
	return nil
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
