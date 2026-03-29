package storage

import st "github.com/keshon/melodix/internal/domain"

func (s *Storage) DisableGroup(guildID, group string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for _, g := range record.CommandsDisabled {
		if g == group {
			return nil
		}
	}

	record.CommandsDisabled = append(record.CommandsDisabled, group)
	return s.ds.Set(guildID, record)
}

func (s *Storage) EnableGroup(guildID, group string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	updated := make([]string, 0, len(record.CommandsDisabled))
	for _, g := range record.CommandsDisabled {
		if g != group {
			updated = append(updated, g)
		}
	}
	record.CommandsDisabled = updated
	return s.ds.Set(guildID, record)
}

func (s *Storage) IsGroupDisabled(guildID, group string) (bool, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return false, err
	}
	for _, g := range record.CommandsDisabled {
		if g == group {
			return true, nil
		}
	}
	return false, nil
}

func (s *Storage) GetDisabledGroups(guildID string) ([]string, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}
	return record.CommandsDisabled, nil
}

func (s *Storage) GetCommandsHistory(guildID string) ([]st.CommandHistory, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}

	return record.CommandsHistory, nil
}

// GetCommandHashes returns the cached slash-command hashes for a guild (used to skip re-registration when unchanged).
func (s *Storage) GetCommandHashes(guildID string) (map[string]string, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}
	if record.CommandHashes == nil {
		return map[string]string{}, nil
	}
	return record.CommandHashes, nil
}

// SetCommandHashes persists the slash-command hashes for a guild.
func (s *Storage) SetCommandHashes(guildID string, hashes map[string]string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}
	record.CommandHashes = hashes
	return s.ds.Set(guildID, record)
}

// ClearCommandHashes clears the cached hashes for a guild (e.g. after a full command purge).
func (s *Storage) ClearCommandHashes(guildID string) error {
	return s.SetCommandHashes(guildID, map[string]string{})
}
