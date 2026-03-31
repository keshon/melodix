package cmdmanager

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
)

// hashCommand produces a stable SHA-1 fingerprint of the fields that matter for
// command registration. Changing name, description, type, or options will produce
// a different hash and trigger an upsert.
func hashCommand(c *discordgo.ApplicationCommand) string {
	stable := map[string]interface{}{
		"name":        c.Name,
		"description": c.Description,
		"type":        c.Type,
	}
	if len(c.Options) > 0 {
		stable["options"] = normalizeOptions(c.Options)
	}

	data, _ := json.Marshal(stable)
	sum := sha1.Sum(data)
	return fmt.Sprintf("%x", sum)
}

// normalizeOptions recursively converts ApplicationCommandOptions into a stable,
// sorted structure suitable for deterministic JSON marshalling.
func normalizeOptions(opts []*discordgo.ApplicationCommandOption) []map[string]interface{} {
	out := make([]map[string]interface{}, len(opts))

	for i, o := range opts {
		entry := map[string]interface{}{
			"name":        o.Name,
			"description": o.Description,
			"type":        o.Type,
			"required":    o.Required,
		}

		if len(o.Choices) > 0 {
			choices := make([]map[string]interface{}, len(o.Choices))
			for j, ch := range o.Choices {
				choices[j] = map[string]interface{}{
					"name":  ch.Name,
					"value": ch.Value,
				}
			}
			entry["choices"] = choices
		}

		if len(o.Options) > 0 {
			entry["options"] = normalizeOptions(o.Options)
		}

		out[i] = entry
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})

	return out
}
