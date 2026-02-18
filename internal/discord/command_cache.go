package discord

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// guildCachePath returns the path to the guild command cache
func guildCachePath(guildID string) string {
	return filepath.Join("data", "commands", guildID+".json")
}

// loadGuildCommandHashes loads the guild command cache
func loadGuildCommandHashes(guildID string) map[string]string {
	data := make(map[string]string)
	path := guildCachePath(guildID)

	file, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(file, &data)
	}
	return data
}

// saveGuildCommandHashes saves the guild command cache
func saveGuildCommandHashes(guildID string, hashes map[string]string) {
	path := guildCachePath(guildID)
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(hashes, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}
