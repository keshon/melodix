package bot

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/player"
	songpkg "github.com/keshon/melodix/song"
	"github.com/keshon/melodix/storage"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

type Bot struct {
	session       *discordgo.Session
	storage       *storage.Storage
	players       map[string]*player.Player
	prefixCache   map[string]string
	playMessage   map[string]*discordgo.Message
	playChannelID map[string]string
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
		playMessage:   make(map[string]*discordgo.Message),
		playChannelID: make(map[string]string),
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

	cmd := getAliasedCommand(command)
	if cmd == nil {
		return
	}

	executeCommand(s, m, b, cmd.Name, param)
}

func (b *Bot) onPlayback(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	var currentChannelID string
	if b.playChannelID[m.GuildID] != "" {
		currentChannelID = b.playChannelID[m.GuildID]
	} else {
		currentChannelID = m.ChannelID
	}

	instance := b.getOrCreatePlayer(m.GuildID)
	signal := <-instance.StatusSignals
	switch signal {
	case player.StatusPlaying:
		if instance.Song == nil {
			s.ChannelMessageSendEmbed(currentChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription("No song is currently playing.").MessageEmbed)
			return
		}

		emb := embed.NewEmbed().SetColor(embedColor)
		title, source, publicLink, parser, err := instance.Song.GetSongInfo(instance.Song)
		if err != nil {
			s.ChannelMessageEditEmbed(currentChannelID, b.playMessage[m.GuildID].ID, emb.SetDescription(fmt.Sprintf("Error getting this song(s)\n\n%v", err)).MessageEmbed)
		}
		hostname, err := extractHostname(instance.Song.PublicLink)
		if err != nil {
			hostname = source
		}
		ffmpeg := "`ffmpeg`"
		if parser != "" {
			if parser == songpkg.ParserKkdai.String() {
				parser = fmt.Sprintf("`%s`", "kkdai")
			} else if parser == songpkg.ParserYtdlp.String() {
				parser = fmt.Sprintf("`%s`", "ytdlp")
			}
		}
		emb.SetDescription(fmt.Sprintf("%s Now playing\n\n**%s**\n[%s](%s)\n\n%s %s", player.StatusPlaying.StringEmoji(), title, hostname, publicLink, ffmpeg, parser))
		if len(instance.Song.Thumbnail.URL) > 0 {
			emb.SetThumbnail(instance.Song.Thumbnail.URL)
		}
		emb.SetFooter(fmt.Sprintf("Use %shelp for a list of commands.", b.prefixCache[m.GuildID]))
		if b.playMessage[m.GuildID] != nil {
			s.ChannelMessageEditEmbed(currentChannelID, b.playMessage[m.GuildID].ID, emb.MessageEmbed)
		} else {
			s.ChannelMessageSendEmbed(currentChannelID, emb.MessageEmbed)
		}
	case player.StatusResuming:
		fmt.Println("Interuption detected, resuming...")
	case player.StatusError:
		fmt.Println("Error:", signal)
	case player.StatusAdded:
		desc := fmt.Sprintf("Song(s) added to queue\n\nUse `%slist` to see the current queue.", b.prefixCache[m.GuildID])
		if b.playMessage[m.GuildID] != nil {
			s.ChannelMessageEditEmbed(currentChannelID, b.playMessage[m.GuildID].ID, embed.NewEmbed().SetColor(embedColor).SetDescription(desc).MessageEmbed)
		} else {
			s.ChannelMessageSendEmbed(currentChannelID, embed.NewEmbed().SetColor(embedColor).SetDescription(desc).MessageEmbed)
		}

	}

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

func getAliasedCommand(input string) *Command {
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

func (b *Bot) fetchSongs(input string, parser songpkg.Parser) ([]*songpkg.Song, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("no song title or URL provided")
	}

	var songs []*songpkg.Song
	songFetcher := songpkg.New()

	song, err := songFetcher.FetchSongs(input, parser)
	if err != nil {
		return nil, err
	}
	songs = append(songs, song...)

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

func extractHostname(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	hostname := parsedURL.Hostname()

	// Remove 'www.' prefix if present
	hostname = strings.TrimPrefix(hostname, "www.")

	return hostname, nil
}
