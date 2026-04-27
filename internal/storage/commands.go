package storage

import "github.com/keshon/melodix/internal/domain"

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

func (s *Storage) DisabledGroups(guildID string) ([]string, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}
	return record.CommandsDisabled, nil
}

func (s *Storage) CommandHistory(guildID string) ([]domain.CommandHistory, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}

	return record.CommandsHistory, nil
}
