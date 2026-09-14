package cmdsync

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/disgoorg/disgo/discord"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// fingerprint is a stable digest of the fields that matter for registration.
//
// It is computed over the neutral declaration rather than over either
// library's types, which is what lets the two sides of the comparison come
// from different places: the desired command is what a command declared, and
// the existing one is what Discord reported, converted back. Hashing the wire
// form instead would compare two shapes that differ in defaults nobody set.
//
// The digest is never persisted -- both sides are computed fresh on every
// sync -- so the algorithm can change without a migration.
func fingerprint(c *cmdadapter.SlashCommand) string {
	if c == nil {
		return ""
	}
	stable := map[string]any{
		"name":        c.Name,
		"description": c.Description,
		"type":        int(c.Type),
	}
	if len(c.Options) > 0 {
		stable["options"] = normalizeOptions(c.Options)
	}

	data, _ := json.Marshal(stable)
	return fmt.Sprintf("%x", sha1.Sum(data))
}

// normalizeOptions renders options into a sorted, deterministic structure.
// Discord does not promise an order and neither library preserves one, so a
// declaration reordered in source must not read as a change.
func normalizeOptions(opts []cmdadapter.SlashOption) []map[string]any {
	out := make([]map[string]any, 0, len(opts))
	for _, o := range opts {
		entry := map[string]any{
			"name":        o.Name,
			"description": o.Description,
			"type":        int(o.Type),
			"required":    o.Required,
		}
		if o.MinValue != nil {
			entry["min_value"] = *o.MinValue
		}
		if o.MaxValue != 0 {
			entry["max_value"] = o.MaxValue
		}
		if len(o.Choices) > 0 {
			entry["choices"] = normalizeChoices(o.Choices)
		}
		if len(o.Options) > 0 {
			entry["options"] = normalizeOptions(o.Options)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}

func normalizeChoices(choices []cmdadapter.SlashChoice) []map[string]any {
	out := make([]map[string]any, 0, len(choices))
	for _, c := range choices {
		out = append(out, map[string]any{
			"name":  c.Name,
			"value": fmt.Sprint(c.Value),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["name"].(string) != out[j]["name"].(string) {
			return out[i]["name"].(string) < out[j]["name"].(string)
		}
		return out[i]["value"].(string) < out[j]["value"].(string)
	})
	return out
}

// fromWire converts a command Discord reported back into the neutral
// declaration, so it can be fingerprinted against what the registry declares.
func fromWire(c discord.ApplicationCommand) *cmdadapter.SlashCommand {
	if c == nil {
		return nil
	}
	out := &cmdadapter.SlashCommand{Name: c.Name()}
	switch c.Type() {
	case discord.ApplicationCommandTypeMessage:
		out.Type = cmdadapter.MessageMenuCommand
	case discord.ApplicationCommandTypeUser:
		out.Type = cmdadapter.UserMenuCommand
	default:
		out.Type = cmdadapter.ChatInputCommand
	}
	if slash, ok := c.(discord.SlashCommand); ok {
		out.Description = slash.Description
		out.Options = optionsFromWire(slash.Options)
	}
	return out
}

func optionsFromWire(opts []discord.ApplicationCommandOption) []cmdadapter.SlashOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]cmdadapter.SlashOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, optionFromWire(o))
	}
	return out
}

func optionFromWire(o discord.ApplicationCommandOption) cmdadapter.SlashOption {
	switch v := o.(type) {
	case discord.ApplicationCommandOptionSubCommand:
		return cmdadapter.SlashOption{
			Type: cmdadapter.OptionSubCommand, Name: v.Name,
			Description: v.Description, Options: optionsFromWire(v.Options),
		}
	case discord.ApplicationCommandOptionSubCommandGroup:
		subs := make([]cmdadapter.SlashOption, 0, len(v.Options))
		for _, sub := range v.Options {
			subs = append(subs, cmdadapter.SlashOption{
				Type: cmdadapter.OptionSubCommand, Name: sub.Name,
				Description: sub.Description, Options: optionsFromWire(sub.Options),
			})
		}
		return cmdadapter.SlashOption{
			Type: cmdadapter.OptionSubCommandGroup, Name: v.Name,
			Description: v.Description, Options: subs,
		}
	case discord.ApplicationCommandOptionInt:
		out := cmdadapter.SlashOption{
			Type: cmdadapter.OptionInteger, Name: v.Name,
			Description: v.Description, Required: v.Required,
		}
		if v.MinValue != nil {
			min := float64(*v.MinValue)
			out.MinValue = &min
		}
		if v.MaxValue != nil {
			out.MaxValue = float64(*v.MaxValue)
		}
		for _, c := range v.Choices {
			out.Choices = append(out.Choices, cmdadapter.SlashChoice{Name: c.Name, Value: c.Value})
		}
		return out
	case discord.ApplicationCommandOptionBool:
		return cmdadapter.SlashOption{
			Type: cmdadapter.OptionBoolean, Name: v.Name,
			Description: v.Description, Required: v.Required,
		}
	case discord.ApplicationCommandOptionString:
		out := cmdadapter.SlashOption{
			Type: cmdadapter.OptionString, Name: v.Name,
			Description: v.Description, Required: v.Required,
		}
		for _, c := range v.Choices {
			out.Choices = append(out.Choices, cmdadapter.SlashChoice{Name: c.Name, Value: c.Value})
		}
		return out
	default:
		return cmdadapter.SlashOption{
			Type: cmdadapter.OptionString, Name: o.OptionName(),
		}
	}
}
