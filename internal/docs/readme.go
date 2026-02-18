package docs

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/pkg/cmd"
)

// UpdateReadme generates README.md from the command registry and category ordering.
// categoryWeights maps category name to sort order (lower first).
func UpdateReadme(registry *cmd.Registry, categoryWeights map[string]int) error {
	commands := registry.GetAll()
	sort.Slice(commands, func(i, j int) bool {
		metaI, _ := cmd.Root(commands[i]).(command.DiscordMeta)
		metaJ, _ := cmd.Root(commands[j]).(command.DiscordMeta)
		catI, catJ := "", ""
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
		meta, _ := cmd.Root(c).(command.DiscordMeta)
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

		name := c.Name()
		display := name
		if !(hasSpace(name) || startsWithUpper(name)) {
			display = "/" + display
		}
		buf.WriteString(fmt.Sprintf("- **%s** — %s\n", display, c.Description()))
	}

	tmplPath := filepath.Join(".", "README.md.tmpl")
	outPath := filepath.Join(".", "README.md")

	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return err
	}

	data := struct {
		CommandSections string
	}{
		CommandSections: buf.String(),
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
