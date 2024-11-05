package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/keshon/melodix/datastore"
	"github.com/keshon/melodix/player"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

type Bot struct {
	Session   *discordgo.Session
	DataStore *datastore.DataStore
	Player    *player.Player
}

type Record struct {
	GuildName string
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

func (b *Bot) Start() {
	b.setIntents()
	b.registerHandlers()
	b.openConnection()

	guildInfo, err := b.Session.Guild(b.Session.State.Guilds[0].ID)
	if err != nil {
		log.Fatal("Error getting guild name:", err)
	}
	b.DataStore.Add(guildInfo.ID, &Record{GuildName: guildInfo.Name})
}

func (b *Bot) Shutdown() {
	if err := b.Session.Close(); err != nil {
		log.Fatal("Error closing connection:", err)
	}
}

func (b *Bot) setIntents() {
	b.Session.Identify.Intents = discordgo.IntentsGuildMessages
}

func (b *Bot) registerHandlers() {
	b.Session.AddHandler(b.onReady)
	b.Session.AddHandler(b.onMessageCreate)
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {

	botInfo, err := s.User("@me")
	if err != nil {
		log.Fatalf("Error retrieving bot user: %v", err)
	}
	fmt.Printf("Bot %v is up!\n", botInfo.Username)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	command, param, err := b.splitCommandFromParameter(m.Content)
	if err != nil {
		return
	}

	switch command {
	case "!ping":
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	case "!pong":
		s.ChannelMessageSend(m.ChannelID, "Ping!")
	case "!info":
		record, _ := b.DataStore.Get(m.GuildID)
		s.ChannelMessageSend(m.ChannelID, record.(*Record).GuildName)
	case "!play":
		channel, err := s.State.Channel(m.ChannelID)
		if err != nil {
			slog.Error("Error getting channel: %v", err)
		}
		guild, err := s.State.Guild(channel.GuildID)
		if err != nil {
			slog.Error("Error getting guild: %v", err)
		}

		b.Player.Play(channel.ID, guild.ID, param)
	}
}

func (b *Bot) openConnection() {
	if err := b.Session.Open(); err != nil {
		log.Fatal("Error opening connection: ", err)
	}
}

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	if os.Getenv("DISCORD_TOKEN") == "" {
		log.Fatal("DISCORD_TOKEN is missing in environment variables")
	}
}

func (b *Bot) splitCommandFromParameter(content string) (string, string, error) {
	content = strings.ToLower(content)

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

	command = strings.ToLower(command)
	return command, parameter, nil
}

func main() {
	loadEnv()

	token := os.Getenv("DISCORD_TOKEN")

	bot, err := NewBot(token)
	if err != nil {
		log.Fatal(err)
	}

	bot.Start()
	defer bot.Shutdown()

	fmt.Println("Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}
