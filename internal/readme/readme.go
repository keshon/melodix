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

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/discord/perm"
	"github.com/rs/zerolog"
)

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

// entry is one command as the README lists it: enough to place it, and how
// to render its lines.
type entry struct {
	name     string
	category string
	render   func(buf *bytes.Buffer)
}

// Generate renders README.md.tmpl into README.md, in the working directory,
// with both frontends' command lists. Each list is grouped under the same
// category headings in the same order: categoryWeights, lower first.
func Generate(bot *command.Registry, terminal *cli.Registry, categoryWeights map[string]int, log zerolog.Logger) error {
	tmpl, err := template.ParseFiles(filepath.Join(".", "README.md.tmpl"))
	if err != nil {
		return err
	}

	data := struct {
		DiscordCommands    string
		CLICommands        string
		BotPermissions     int64
		BotPermissionsList string
	}{
		DiscordCommands:    section(discordEntries(bot), categoryWeights),
		CLICommands:        section(cliEntries(terminal), categoryWeights),
		BotPermissions:     perm.RecommendedBotMask(),
		BotPermissionsList: strings.Join(perm.RecommendedBotNames(), ", "),
	}

	f, err := os.Create(filepath.Join(".", "README.md"))
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

// section renders entries under category headings, categories by weight and
// commands by name within one.
func section(entries []entry, categoryWeights map[string]int) string {
	sort.SliceStable(entries, func(i, j int) bool {
		wi, wj := categoryWeights[entries[i].category], categoryWeights[entries[j].category]
		if wi == wj {
			return entries[i].name < entries[j].name
		}
		return wi < wj
	})

	var buf bytes.Buffer
	currentCategory := ""
	for i, e := range entries {
		if i == 0 || e.category != currentCategory {
			if i > 0 {
				buf.WriteString("\n")
			}
			currentCategory = e.category
			fmt.Fprintf(&buf, "#### %s\n\n", plainCategory(currentCategory))
		}
		e.render(&buf)
	}
	return strings.TrimRight(buf.String(), "\n")
}
