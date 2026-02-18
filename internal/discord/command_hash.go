package discord

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
)

// hashCommand creates a deterministic hash for an ApplicationCommand (including options)
func hashCommand(cmd *discordgo.ApplicationCommand) string {
	normalized := normalizeForHash(cmd)
	data, _ := json.Marshal(normalized)
	sum := sha1.Sum(data)
	return fmt.Sprintf("%x", sum)
}

// normalizeForHash strips runtime-only fields (IDs, versions, etc.) and sorts options
func normalizeForHash(cmd *discordgo.ApplicationCommand) map[string]interface{} {
	obj := map[string]interface{}{
		"name":        cmd.Name,
		"description": cmd.Description,
		"type":        cmd.Type,
	}

	if len(cmd.Options) > 0 {
		obj["options"] = normalizeOptions(cmd.Options)
	}
	return obj
}

// normalizeOptions sorts options
func normalizeOptions(opts []*discordgo.ApplicationCommandOption) []map[string]interface{} {
	normalized := make([]map[string]interface{}, len(opts))

	for i, o := range opts {
		entry := map[string]interface{}{
			"name":        o.Name,
			"description": o.Description,
			"type":        o.Type,
			"required":    o.Required,
		}
		if len(o.Choices) > 0 {
			choices := make([]map[string]interface{}, len(o.Choices))
			for j, c := range o.Choices {
				choices[j] = map[string]interface{}{
					"name":  c.Name,
					"value": c.Value,
				}
			}
			entry["choices"] = choices
		}
		if len(o.Options) > 0 {
			entry["options"] = normalizeOptions(o.Options)
		}
		normalized[i] = entry
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i]["name"].(string) < normalized[j]["name"].(string)
	})

	return normalized
}
