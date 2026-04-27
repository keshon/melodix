package cmdmanager

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

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

		// Include "shape-affecting" option fields that Discord uses to validate/interpret input.
		// We intentionally only persist values that are set (or non-zero) to keep hashes stable
		// across discordgo versions where default values may differ.
		if o.Autocomplete {
			entry["autocomplete"] = true
		}
		// Note: discordgo uses plain numeric fields here (not pointers), so we include the values
		// directly for deterministic comparisons. Defaults (0) are stable across desired/existing.
		entry["min_value"] = o.MinValue
		entry["max_value"] = o.MaxValue
		entry["min_length"] = o.MinLength
		entry["max_length"] = o.MaxLength
		if len(o.ChannelTypes) > 0 {
			cts := make([]int, 0, len(o.ChannelTypes))
			for _, ct := range o.ChannelTypes {
				cts = append(cts, int(ct))
			}
			sort.Ints(cts)
			entry["channel_types"] = cts
		}

		if len(o.Choices) > 0 {
			choices := make([]map[string]interface{}, len(o.Choices))
			for j, ch := range o.Choices {
				choices[j] = map[string]interface{}{
					"name":  ch.Name,
					"value": ch.Value,
				}
			}
			// Discord treats choices as a set; order should not affect comparison.
			sort.Slice(choices, func(i, j int) bool {
				ni, _ := choices[i]["name"].(string)
				nj, _ := choices[j]["name"].(string)
				if ni != nj {
					return ni < nj
				}
				return valueKey(choices[i]["value"]) < valueKey(choices[j]["value"])
			})
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

func valueKey(v interface{}) string {
	switch t := v.(type) {
	case string:
		return "s:" + t
	case bool:
		if t {
			return "b:1"
		}
		return "b:0"
	case float64:
		// json.Unmarshal numbers become float64; encode deterministically.
		return "n:" + strconv.FormatFloat(t, 'g', -1, 64)
	case int:
		return "i:" + strconv.Itoa(t)
	case int64:
		return "i64:" + strconv.FormatInt(t, 10)
	case uint64:
		return "u64:" + strconv.FormatUint(t, 10)
	default:
		b, _ := json.Marshal(t)
		return "j:" + string(b)
	}
}
