package docs

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"

	"github.com/bwmarrin/discordgo"
)

// RecommendedBotPermissions is the bitmask for the minimal permissions the bot needs.
// Used in the OAuth2 invite URL so the generated README shows the correct link.
// Combines: View Channel, Send Messages, Embed Links, Read Message History, Manage Messages, Connect, Speak.
var RecommendedBotPermissions = discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionEmbedLinks |
	discordgo.PermissionReadMessageHistory |
	discordgo.PermissionManageMessages |
	discordgo.PermissionVoiceConnect |
	discordgo.PermissionVoiceSpeak

// RecommendedBotPermissionsList is a human-readable list of these permissions for the README.
var RecommendedBotPermissionsList = []string{
	"View Channel",
	"Send Messages",
	"Embed Links",
	"Read Message History",
	"Manage Messages",
	"Connect to Voice Channel",
	"Speak",
}

// UpdateReadme generates README.md from the command registry and category ordering.
// categoryWeights maps category name to sort order (lower first).
func UpdateReadme(registry *commandkit.Registry, categoryWeights map[string]int) error {
	commands := registry.GetAll()

	sort.Slice(commands, func(i, j int) bool {
		metaI, _ := commandkit.Root(commands[i]).(command.DiscordMeta)
		metaJ, _ := commandkit.Root(commands[j]).(command.DiscordMeta)

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
		root := commandkit.Root(c)

		meta, _ := root.(command.DiscordMeta)
		cat := ""
		if meta != nil {
			cat = meta.Category()
		}

		if cat != currentCategory {
			if currentCategory != "" {
				buf.WriteString("\n")
			}
			currentCategory = cat
			buf.WriteString(fmt.Sprintf("### %s\n\n", currentCategory))
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

	log.Println("[INFO] README.md updated with current commands")
	return nil
}

func renderDiscordCommand(buf *bytes.Buffer, c commandkit.Command) {
	name := c.Name()
	display := name
	if !(hasSpace(name) || startsWithUpper(name)) {
		display = "/" + display
	}

	buf.WriteString(fmt.Sprintf(
		"- **%s** — %s\n",
		display,
		c.Description(),
	))

	sp, ok := c.(command.SlashProvider)
	if !ok {
		return
	}

	def := sp.SlashDefinition()
	if def == nil {
		return
	}

	for _, opt := range def.Options {
		if opt.Type != discordgo.ApplicationCommandOptionSubCommand {
			continue
		}

		buf.WriteString(fmt.Sprintf(
			"  - **/%s %s** — %s\n",
			def.Name,
			opt.Name,
			opt.Description,
		))
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
