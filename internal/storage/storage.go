package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	st "github.com/keshon/melodix/internal/domain"

	"github.com/keshon/datastore"
)

const commandHistoryLimit int = 50

type Storage struct {
	ds *datastore.DataStore
}

func New(filePath string) (*Storage, error) {
	ds, err := datastore.New(filePath)
	if err != nil {
		return nil, err
	}
	return &Storage{ds: ds}, nil
}

func (s *Storage) Close() error {
	return s.ds.Close()
}

func (s *Storage) getOrCreateGuildRecord(guildID string) (*st.Record, error) {
	data, exists := s.ds.Get(guildID)
	if !exists {
		newRecord := &st.Record{}
		s.ds.Add(guildID, newRecord)
		return newRecord, nil
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("error marshalling data: %w", err)
	}

	var record st.Record
	err = json.Unmarshal(jsonData, &record)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling to *Record: %w", err)
	}

	if len(record.CommandsHistory) > commandHistoryLimit {
		record.CommandsHistory = record.CommandsHistory[len(record.CommandsHistory)-commandHistoryLimit:]
	}

	return &record, nil
}

func (s *Storage) GetGuildRecord(guildID string) (*st.Record, error) {
	return s.getOrCreateGuildRecord(guildID)
}

func (s *Storage) GetRecordsList() map[string]st.Record {
	mapStringAny := s.ds.GetAll()

	mapStringRecord := make(map[string]st.Record)
	for key, value := range mapStringAny {
		jsonData, err := json.Marshal(value)
		if err != nil {
			log.Printf("error marshalling data: %v", err)
			continue
		}

		var record st.Record
		err = json.Unmarshal(jsonData, &record)
		if err != nil {
			log.Printf("error unmarshalling to *Record: %v", err)
			continue
		}

		mapStringRecord[key] = record
	}
	return mapStringRecord
}

func (s *Storage) appendCommandToHistory(guildID string, command st.CommandHistory) error {

	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	record.CommandsHistory = append(record.CommandsHistory, command)
	s.ds.Add(guildID, record)
	return nil
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
