package slashsync

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/disgoorg/disgo/discord"

	"github.com/keshon/melodix/internal/discord/adapter"
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
func fingerprint(c *adapter.SlashCommand) string {
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
func normalizeOptions(opts []adapter.SlashOption) []map[string]any {
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

func normalizeChoices(choices []adapter.SlashChoice) []map[string]any {
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
func fromWire(c discord.ApplicationCommand) *adapter.SlashCommand {
	if c == nil {
		return nil
	}
	out := &adapter.SlashCommand{Name: c.Name()}
	switch c.Type() {
	case discord.ApplicationCommandTypeMessage:
		out.Type = adapter.MessageMenuCommand
	case discord.ApplicationCommandTypeUser:
		out.Type = adapter.UserMenuCommand
	default:
		out.Type = adapter.ChatInputCommand
	}
	if slash, ok := c.(discord.SlashCommand); ok {
		out.Description = slash.Description
		out.Options = optionsFromWire(slash.Options)
	}
	return out
}

func optionsFromWire(opts []discord.ApplicationCommandOption) []adapter.SlashOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]adapter.SlashOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, optionFromWire(o))
	}
	return out
}

func optionFromWire(o discord.ApplicationCommandOption) adapter.SlashOption {
	switch v := o.(type) {
	case discord.ApplicationCommandOptionSubCommand:
		return adapter.SlashOption{
			Type: adapter.OptionSubCommand, Name: v.Name,
			Description: v.Description, Options: optionsFromWire(v.Options),
		}
	case discord.ApplicationCommandOptionSubCommandGroup:
		subs := make([]adapter.SlashOption, 0, len(v.Options))
		for _, sub := range v.Options {
			subs = append(subs, adapter.SlashOption{
				Type: adapter.OptionSubCommand, Name: sub.Name,
				Description: sub.Description, Options: optionsFromWire(sub.Options),
			})
		}
		return adapter.SlashOption{
			Type: adapter.OptionSubCommandGroup, Name: v.Name,
			Description: v.Description, Options: subs,
		}
	case discord.ApplicationCommandOptionInt:
		out := adapter.SlashOption{
			Type: adapter.OptionInteger, Name: v.Name,
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
			out.Choices = append(out.Choices, adapter.SlashChoice{Name: c.Name, Value: c.Value})
		}
		return out
	case discord.ApplicationCommandOptionBool:
		return adapter.SlashOption{
			Type: adapter.OptionBoolean, Name: v.Name,
			Description: v.Description, Required: v.Required,
		}
	case discord.ApplicationCommandOptionString:
		out := adapter.SlashOption{
			Type: adapter.OptionString, Name: v.Name,
			Description: v.Description, Required: v.Required,
		}
		for _, c := range v.Choices {
			out.Choices = append(out.Choices, adapter.SlashChoice{Name: c.Name, Value: c.Value})
		}
		return out
	default:
		return adapter.SlashOption{
			Type: adapter.OptionString, Name: o.OptionName(),
		}
	}
}
