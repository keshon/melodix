package cmdadapter

import "github.com/bwmarrin/discordgo"

// Embed is a rich message in the shape melodix actually writes them.
//
// Discord's own struct has around fifteen fields and the commands use six, so
// this is the six. It exists so a command can describe what it wants to say
// without naming the library that will put it on the wire — the translation is
// at the bottom of this file, in the one package that is allowed to know.
//
// Colour is deliberately a plain int rather than a named type: zero means
// "whatever the responder's default is", which is what almost every caller
// wants and none of them should have to say.
type Embed struct {
	Title       string
	Description string
	Color       int
	Footer      string
	ImageURL    string
	Fields      []EmbedField
}

// EmbedField is one name/value row. Inline packs rows side by side.
type EmbedField struct {
	Name   string
	Value  string
	Inline bool
}

// DiscordEmbed translates to the wire format. A nil Embed stays nil so the
// callers that pass one through can keep doing so.
//
// Exported because reply is where the translation happens: it owns every call
// that puts an embed on the wire, and cmdadapter cannot import it.
func DiscordEmbed(e *Embed) *discordgo.MessageEmbed {
	if e == nil {
		return nil
	}

	out := &discordgo.MessageEmbed{
		Title:       e.Title,
		Description: e.Description,
		Color:       e.Color,
	}
	if e.Footer != "" {
		out.Footer = &discordgo.MessageEmbedFooter{Text: e.Footer}
	}
	if e.ImageURL != "" {
		out.Image = &discordgo.MessageEmbedImage{URL: e.ImageURL}
	}
	for _, f := range e.Fields {
		out.Fields = append(out.Fields, &discordgo.MessageEmbedField{
			Name:   f.Name,
			Value:  f.Value,
			Inline: f.Inline,
		})
	}
	return out
}
