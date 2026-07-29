package storage

import "slices"

// DisableGroup marks a command group disabled for the guild (idempotent).
func (s *Storage) DisableGroup(guildID, group string) error {
	g := s.guildSettings(guildID)
	if slices.Contains(g.CommandsDisabled, group) {
		return nil
	}
	g.CommandsDisabled = append(g.CommandsDisabled, group)
	return s.settings.Put(g)
}

// EnableGroup re-enables a command group for the guild (idempotent).
func (s *Storage) EnableGroup(guildID, group string) error {
	g := s.guildSettings(guildID)
	updated := make([]string, 0, len(g.CommandsDisabled))
	for _, existing := range g.CommandsDisabled {
		if existing != group {
			updated = append(updated, existing)
		}
	}
	if len(updated) == len(g.CommandsDisabled) {
		return nil
	}
	g.CommandsDisabled = updated
	return s.settings.Put(g)
}

// IsGroupDisabled reports whether a command group is disabled for the guild.
func (s *Storage) IsGroupDisabled(guildID, group string) (bool, error) {
	return slices.Contains(s.guildSettings(guildID).CommandsDisabled, group), nil
}

// DisabledGroups lists the guild's disabled command groups.
func (s *Storage) DisabledGroups(guildID string) ([]string, error) {
	return s.guildSettings(guildID).CommandsDisabled, nil
}
