package discordreply

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/perm"
)

const EmbedColor = 0xb01e66

// responder implements command.Responder so commands can reply without importing
// the discord package directly (avoids import cycles).
type responder struct{}

func (responder) RespondEmbedEphemeral(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return RespondEmbedEphemeral(s, e, embed)
}
func (responder) RespondEmbed(s *discordgo.Session, e *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return RespondEmbed(s, e, embed)
}
func (responder) CheckBotPermissions(s *discordgo.Session, channelID string) bool {
	return perm.CheckBotPermissions(s, channelID)
}
func (responder) EmbedColor() int { return EmbedColor }

// DefaultResponder is injected into command contexts so commands never import discord directly.
var DefaultResponder cmdadapter.Responder = responder{}

// --- Interaction responses ---

// isAlreadyAcknowledged reports whether an interaction was already ACK'd/responded to.
// In that case, we must use InteractionResponseEdit or followups instead of InteractionRespond.
func isAlreadyAcknowledged(err error) bool {
	if err == nil {
		return false
	}
	// discordgo often wraps API errors as *discordgo.RESTError; string matching is
	// the most robust across forked versions and proxying layers.
	msg := err.Error()
	return strings.Contains(msg, "already been acknowledged") ||
		strings.Contains(msg, "Interaction has already been acknowledged")
}

// AckDeferred sends a fast public ACK for slash interactions and ignores duplicate ACK errors.
func AckDeferred(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if isAlreadyAcknowledged(err) {
		return nil
	}
	return err
}

// Respond sends a public message response to an interaction.
func Respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		return EditResponse(s, i, content)
	}
	return err
}

// RespondEphemeral sends an ephemeral message response to an interaction.
func RespondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		// Editing an existing (likely public) deferred response cannot turn it ephemeral,
		// so we fall back to an ephemeral followup.
		return FollowupEphemeral(s, i, content)
	}
	return err
}

// RespondEmbed sends a public embed response to an interaction.
func RespondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{embed}},
	})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		embeds := []*discordgo.MessageEmbed{embed}
		_, e := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &embeds})
		return e
	}
	return err
}

// RespondEmbedEphemeral sends an ephemeral embed response to an interaction.
func RespondEmbedEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		return FollowupEmbedEphemeral(s, i, embed)
	}
	return err
}

// RespondEmbedEphemeralWithFile sends an ephemeral embed with an attached file.
func RespondEmbedEphemeralWithFile(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	embed *discordgo.MessageEmbed,
	fileReader interface{ Read([]byte) (int, error) },
	fileName string,
) error {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{embed},
			Files:  []*discordgo.File{{Name: fileName, Reader: fileReader}},
		},
	})
	if err == nil {
		return nil
	}
	if isAlreadyAcknowledged(err) {
		_, ferr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Embeds: []*discordgo.MessageEmbed{embed},
			Files:  []*discordgo.File{{Name: fileName, Reader: fileReader}},
		})
		return ferr
	}
	return err
}

// RespondDeferredEphemeral acknowledges an interaction ephemerally without an immediate reply.
func RespondDeferredEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

// EditResponse edits an existing interaction response.
func EditResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
	return err
}

// EditResponseEmbed replaces embeds in the original interaction response.
func EditResponseEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	if embed == nil {
		return fmt.Errorf("nil embed")
	}
	embeds := []*discordgo.MessageEmbed{embed}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &embeds})
	return err
}

// --- Followup messages ---

// Followup sends a public followup message.
func Followup(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{Content: content})
	return err
}

// FollowupEphemeral sends an ephemeral followup message.
func FollowupEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: content})
	return err
}

// FollowupEmbed sends a public embed followup message.
func FollowupEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	return err
}

// FollowupEmbedEphemeral sends an ephemeral embed followup message.
func FollowupEmbedEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	return err
}

// --- Channel messages (non-interaction) ---

// Message sends a plain text message to a channel.
func Message(s *discordgo.Session, channelID, content string) error {
	_, err := s.ChannelMessageSend(channelID, content)
	return err
}

// MessageEmbed sends an embed to a channel.
func MessageEmbed(s *discordgo.Session, channelID string, embed *discordgo.MessageEmbed) error {
	_, err := s.ChannelMessageSendEmbed(channelID, embed)
	return err
}
