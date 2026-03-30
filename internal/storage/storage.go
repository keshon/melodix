package storage

import (
	"context"
	"fmt"
	"time"

	st "github.com/keshon/melodix/internal/domain"

	"github.com/keshon/datastore"
)

const commandHistoryLimit int = 50

type Storage struct {
	ds                        *datastore.DataStore
	musicPlaybackHistoryLimit int
}

// New opens the JSON datastore. musicPlaybackHistoryLimit caps persisted playback rows per guild (≤0 means default 750).
func New(filePath string, musicPlaybackHistoryLimit int) (*Storage, error) {
	ds, err := datastore.New(context.Background(), filePath)
	if err != nil {
		return nil, err
	}
	if musicPlaybackHistoryLimit <= 0 {
		musicPlaybackHistoryLimit = 750
	}
	return &Storage{ds: ds, musicPlaybackHistoryLimit: musicPlaybackHistoryLimit}, nil
}

func (s *Storage) Close() error {
	return s.ds.Close()
}

func (s *Storage) getOrCreateGuildRecord(guildID string) (*st.Record, error) {
	var record st.Record
	exists, err := s.ds.Get(guildID, &record)
	if err != nil {
		return nil, fmt.Errorf("error getting guild record: %w", err)
	}
	if !exists {
		newRecord := &st.Record{}
		if err := s.ds.Set(guildID, newRecord); err != nil {
			return nil, err
		}
		return newRecord, nil
	}

	if len(record.CommandsHistory) > commandHistoryLimit {
		record.CommandsHistory = record.CommandsHistory[len(record.CommandsHistory)-commandHistoryLimit:]
	}

	return &record, nil
}

func (s *Storage) GetGuildRecord(guildID string) (*st.Record, error) {
	return s.getOrCreateGuildRecord(guildID)
}

func (s *Storage) GetRecordsList() (map[string]st.Record, error) {
	mapStringRecord := make(map[string]st.Record)
	for _, key := range s.ds.Keys() {
		var record st.Record
		exists, err := s.ds.Get(key, &record)
		if err != nil {
			return nil, fmt.Errorf("get record for key %q: %w", key, err)
		}
		if !exists {
			continue
		}
		mapStringRecord[key] = record
	}
	return mapStringRecord, nil
}

func (s *Storage) appendCommandToHistory(guildID string, command st.CommandHistory) error {

	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	record.CommandsHistory = append(record.CommandsHistory, command)
	return s.ds.Set(guildID, record)
}

func (s *Storage) SetCommand(
	guildID, channelID, channelName, guildName, userID, username, command string,
) error {
	record := st.CommandHistory{
		ChannelID:   channelID,
		ChannelName: channelName,
		GuildName:   guildName,
		UserID:      userID,
		Username:    username,
		Command:     command,
		Datetime:    time.Now(),
	}
	return s.appendCommandToHistory(guildID, record)
}
