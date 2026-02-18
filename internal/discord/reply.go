package discord

import (
	"log"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"

	"github.com/bwmarrin/discordgo"
)

const EmbedColor = 0xb01e66

// responder implements command.Responder so commands can reply without importing discord (breaks import cycles).
type responder struct{}

func (responder) RespondEmbedEphemeral(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return RespondEmbedEphemeral(s, e, embed)
}
func (responder) RespondEmbed(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return RespondEmbed(s, e, embed)
}
func (responder) CheckBotPermissions(s *discordgo.Session, channelID string) bool {
	return CheckBotPermissions(s, channelID)
}
func (responder) EmbedColor() int { return EmbedColor }

// DefaultResponder is the Responder to set on command contexts (implements command.Responder).
var DefaultResponder command.Responder = responder{}

// cmdLogger implements command.CommandLogger so middleware can log without importing discord.
type cmdLogger struct{}

func (cmdLogger) LogCommand(s *discordgo.Session, store *storage.Storage, guildID, channelID, userID, username, commandName string) error {
	return LogCommand(s, store, guildID, channelID, userID, username, commandName)
}

// DefaultLogger is the CommandLogger to set on command contexts.
var DefaultLogger command.CommandLogger = cmdLogger{}

// Respond sends a public message response to an interaction.
func Respond(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) error {
	return session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
}

// RespondEphemeral sends an ephemeral message response to an interaction.
func RespondEphemeral(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) error {
	return session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// RespondEmbedEphemeral sends an ephemeral embed response to an interaction.
func RespondEmbedEphemeral(session *discordgo.Session, interaction *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// RespondEmbed sends a public embed response to an interaction.
func RespondEmbed(session *discordgo.Session, event *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// RespondEmbedEphemeralWithFile sends an ephemeral embed and an attached file.
func RespondEmbedEphemeralWithFile(
	session *discordgo.Session,
	interaction *discordgo.InteractionCreate,
	embed *discordgo.MessageEmbed,
	fileReader interface {
		Read(p []byte) (n int, err error)
	},
	fileName string,
) error {
	return session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{embed},
			Files: []*discordgo.File{
				{
					Name:   fileName,
					Reader: fileReader,
				},
			},
		},
	})
}

// RespondDeferredEphemeral sends an ephemeral deferred response to an interaction.
func RespondDeferredEphemeral(session *discordgo.Session, event *discordgo.InteractionCreate) error {
	return session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
}

// EditResponse edits an existing interaction response.
func EditResponse(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) error {
	_, err := session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	return err
}

// Message sends a simple message to a channel (non-interaction).
func Message(session *discordgo.Session, channelID string, content string) error {
	_, err := session.ChannelMessageSend(channelID, content)
	return err
}

// MessageEmbed sends an embed to a channel.
func MessageEmbed(session *discordgo.Session, channelID string, embed *discordgo.MessageEmbed) error {
	_, err := session.ChannelMessageSendEmbed(channelID, embed)
	return err
}

// Followup sends a public followup message.
func Followup(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) error {
	_, err := session.FollowupMessageCreate(interaction.Interaction, false, &discordgo.WebhookParams{
		Content: content,
	})
	return err
}

// FollowupEphemeral sends an ephemeral followup message.
func FollowupEphemeral(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) error {
	_, err := session.FollowupMessageCreate(interaction.Interaction, true, &discordgo.WebhookParams{
		Content: content,
	})
	return err
}

// FollowupEmbed sends a public embed followup message.
func FollowupEmbed(session *discordgo.Session, interaction *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	_, err := session.FollowupMessageCreate(interaction.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	return err
}

// FollowupEmbedEphemeral sends an ephemeral embed followup message.
func FollowupEmbedEphemeral(session *discordgo.Session, interaction *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	_, err := session.FollowupMessageCreate(interaction.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	return err
}

// LogCommand logs a command to the database.
func LogCommand(s *discordgo.Session, store *storage.Storage, guildID, channelID, userID, username, commandName string) error {
	channel, err := s.State.Channel(channelID)
	if err != nil {
		channel, err = s.Channel(channelID)
		if err != nil {
			log.Println("Failed to fetch channel:", err)
		}
	}
	channelName := ""
	if channel != nil {
		channelName = channel.Name
	}

	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			log.Println("Failed to fetch guild:", err)
		}
	}
	guildName := ""
	if guild != nil {
		guildName = guild.Name
	}

	return store.SetCommand(
		guildID,
		channelID,
		channelName,
		guildName,
		userID,
		username,
		commandName,
	)
}

// IsAdministrator checks if a member has admin permissions in a guild.
func IsAdministrator(s *discordgo.Session, member *discordgo.Member, cfg *config.Config) bool {
	if member == nil || member.User == nil {
		return false
	}

	guild, err := s.State.Guild(member.GuildID)
	if err != nil || guild == nil {
		guild, err = s.Guild(member.GuildID)
		if err != nil || guild == nil {
			return false
		}
	}

	if member.User.ID == guild.OwnerID {
		return true
	}

	for _, roleID := range member.Roles {
		if role, _ := s.State.Role(guild.ID, roleID); role != nil && role.Permissions&discordgo.PermissionAdministrator != 0 {
			return true
		}
	}

	return false
}

// CheckBotPermissions checks if the bot has manage messages permissions in a channel.
func CheckBotPermissions(s *discordgo.Session, channelID string) bool {
	botID := s.State.User.ID
	perms, err := s.UserChannelPermissions(botID, channelID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionManageMessages != 0
}
