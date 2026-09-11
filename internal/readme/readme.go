package readme

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// RecommendedBotPermissions is the bitmask for the minimal permissions the bot
// needs. Used in the OAuth2 invite URL so the generated README shows the
// correct link. Combines: View Channel, Send Messages, Embed Links, Read
// Message History, Manage Messages.
var RecommendedBotPermissions = discordgo.PermissionManageRoles |
	discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionEmbedLinks |
	discordgo.PermissionAttachFiles |
	discordgo.PermissionReadMessageHistory |
	discordgo.PermissionManageMessages |
	discordgo.PermissionUseApplicationCommands

// RecommendedBotPermissionsList is a human-readable list of these permissions
// for the README.
var RecommendedBotPermissionsList = []string{
	"View Channel",
	"Send Messages",
	"Embed Links",
	"Read Message History",
	"Manage Messages",
}

// plainCategory drops the icon a category name carries, for the README.
//
// The icon stays in Category() because Discord renders these in embeds, where
// it earns its place and the client always has the glyph. A Markdown heading
// is neither: the emoji is decoration there, and it is the one part of the
// generated section that renders differently depending on the reader's fonts.
// Weighting and sorting still key off the original string — only the heading
// is plain.
//
// Leading icon runes are dropped up to the first rune that is text in its own
// right, so a category gains or loses an icon without anything here needing to
// know about it. A name that is nothing but an icon is left alone rather than
// rendered as an empty heading.
//
// "First non-letter" is not enough on its own: U+2139 INFORMATION SOURCE, the
// icon on the Information category, lives in Letterlike Symbols and Go reports
// unicode.IsLetter for it. What marks it as an icon is the variation selector
// that follows, which is the whole job of U+FE0F — so a rune trailed by one is
// treated as an icon whatever its category says.
const (
	variationSelector16 = '️'
	zeroWidthJoiner     = '‍'
)

func plainCategory(s string) string {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		emojiPresented := i+1 < len(runes) && runes[i+1] == variationSelector16

		switch {
		case r == variationSelector16 || r == zeroWidthJoiner || unicode.IsMark(r):
		case unicode.IsSpace(r):
		case unicode.IsSymbol(r):
		case emojiPresented:
		default:
			if plain := strings.TrimSpace(string(runes[i:])); plain != "" {
				return plain
			}
			return s
		}
	}
	return s
}

// UpdateReadme generates README.md from the command registry and category
// ordering. categoryWeights maps category name to sort order (lower first).
func UpdateReadme(registry *command.Registry, categoryWeights map[string]int, log zerolog.Logger) error {
	commands := registry.GetAll()

	sort.Slice(commands, func(i, j int) bool {
		metaI, _ := command.Root(commands[i]).(cmdadapter.Meta)
		metaJ, _ := command.Root(commands[j]).(cmdadapter.Meta)

		catI := ""
		catJ := ""

		if metaI != nil {
			catI = metaI.Category()
		}
		if metaJ != nil {
			catJ = metaJ.Category()
		}

		wi := categoryWeights[catI]
		wj := categoryWeights[catJ]

		if wi == wj {
			return commands[i].Name() < commands[j].Name()
		}
		return wi < wj
	})

	var buf bytes.Buffer
	currentCategory := ""

	for _, c := range commands {
		root := command.Root(c)

		meta, _ := root.(cmdadapter.Meta)
		cat := ""
		if meta != nil {
			cat = meta.Category()
		}

		if cat != currentCategory {
			if currentCategory != "" {
				buf.WriteString("\n")
			}
			currentCategory = cat
			fmt.Fprintf(&buf, "### %s\n\n", plainCategory(currentCategory))
		}

		renderDiscordCommand(&buf, root)
	}

	tmplPath := filepath.Join(".", "README.md.tmpl")
	outPath := filepath.Join(".", "README.md")

	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return err
	}

	permListBuf := new(bytes.Buffer)
	for i, name := range RecommendedBotPermissionsList {
		if i > 0 {
			permListBuf.WriteString(", ")
		}
		permListBuf.WriteString(name)
	}

	data := struct {
		CommandSections    string
		BotPermissions     int64
		BotPermissionsList string
	}{
		CommandSections:    buf.String(),
		BotPermissions:     int64(RecommendedBotPermissions),
		BotPermissionsList: permListBuf.String(),
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return err
	}

	log.Info().Msg("readme_updated")
	return nil
}

func renderDiscordCommand(buf *bytes.Buffer, c command.Command) {
	name := c.Name()
	display := name
	if !hasSpace(name) && !startsWithUpper(name) {
		display = "/" + display
	}

	fmt.Fprintf(buf, "- **%s** — %s\n", display, c.Description())

	sp, ok := c.(cmdadapter.SlashProvider)
	if !ok {
		return
	}

	def := sp.SlashDefinition()
	if def == nil {
		return
	}

	var sub strings.Builder
	cmdadapter.AppendSlashSubcommands(&sub, def.Name, def.Options, "")
	for _, line := range strings.Split(sub.String(), "\n") {
		// Lines look like:  `/help category` - description
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "`")
		parts := strings.SplitN(line, "` - ", 2)
		if len(parts) == 2 {
			fmt.Fprintf(buf, "  - **%s** — %s\n", parts[0], parts[1])
		}
	}
}

func hasSpace(s string) bool {
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return false
}

func startsWithUpper(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}
