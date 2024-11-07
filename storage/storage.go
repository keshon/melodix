package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/melodix/datastore"
)

type Storage struct {
	ds *datastore.DataStore
}

type CommandHistoryRecord struct {
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	Command     string    `json:"command"`
	Param       string    `json:"param"`
	Datetime    time.Time `json:"datetime"`
}

type TracksHistoryRecord struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	SourceType    string        `json:"source_type"`
	PublicLink    string        `json:"public_link"`
	TotalCount    int           `json:"total_count"`
	TotalDuration time.Duration `json:"total_duration"`
	LastPlayed    time.Time     `json:"last_played"`
}

type Record struct {
	GuildID             string                 `json:"guild_name"`
	ModsList            []string               `json:"mods"`
	PrefPrefix          string                 `json:"pref_prefix"`
	CommandsHistoryList []CommandHistoryRecord `json:"commands_history"`
	TracksHistoryList   []TracksHistoryRecord  `json:"tracks_history"`
}

func New(filePath string) (*Storage, error) {
	ds, err := datastore.New(filePath)
	if err != nil {
		return nil, err
	}
	return &Storage{ds: ds}, nil
}

func (s *Storage) CreateGuild(guildID, prefix string) error {
	if _, exists := s.ds.Get(guildID); exists {
		return fmt.Errorf("guild already exists")
	}

	newRecord := Record{
		GuildID:             guildID,
		ModsList:            []string{},
		PrefPrefix:          prefix,
		CommandsHistoryList: make([]CommandHistoryRecord, 0),
		TracksHistoryList:   make([]TracksHistoryRecord, 0),
	}
	s.ds.Add(guildID, newRecord)
	return nil
}

func (s *Storage) ReadGuild(guildID string) (*Record, error) {
	data, exists := s.ds.Get(guildID)
	if !exists {
		return nil, fmt.Errorf("guild not found")
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid guild data: expected map[string]interface{}")
	}

	var record Record
	dataBytes, err := json.Marshal(dataMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data for unmarshaling: %v", err)
	}

	err = json.Unmarshal(dataBytes, &record)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data into Record: %v", err)
	}

	return &record, nil
}

func (s *Storage) UpdateGuild(guildID string, newRecord *Record) error {
	_, err := s.ReadGuild(guildID)
	if err != nil {
		return fmt.Errorf("guild not found")
	}
	s.ds.Add(guildID, newRecord)
	return nil
}

func (s *Storage) DeleteGuild(guildID string) error {
	if _, exists := s.ds.Get(guildID); !exists {
		return fmt.Errorf("guild not found")
	}

	s.ds.Delete(guildID)
	return nil
}

func (s *Storage) CreateCommandHistory(guildID, channelID, channelName, command, param string) error {
	record, err := s.ReadGuild(guildID)
	if err != nil {
		return err
	}

	newRecord := CommandHistoryRecord{
		ChannelID:   channelID,
		ChannelName: channelName,
		Command:     command,
		Param:       param,
		Datetime:    time.Now(),
	}

	record.CommandsHistoryList = append(record.CommandsHistoryList, newRecord)

	return s.UpdateGuild(guildID, record)
}

func (s *Storage) CreateTracksHistory(guildID, trackID, name, sourceType, publicLink string, totalCount int, totalDuration time.Duration, lastPlayed time.Time) error {
	record, err := s.ReadGuild(guildID)
	if err != nil {
		return err
	}

	newRecord := TracksHistoryRecord{
		ID:            trackID,
		Name:          name,
		SourceType:    sourceType,
		PublicLink:    publicLink,
		TotalCount:    totalCount,
		TotalDuration: totalDuration,
		LastPlayed:    lastPlayed,
	}

	record.TracksHistoryList = append(record.TracksHistoryList, newRecord)

	return s.UpdateGuild(guildID, record)
}

func (s *Storage) FindTrackByID(guildID, trackID string) (*TracksHistoryRecord, error) {
	record, err := s.ReadGuild(guildID)
	if err != nil {
		return nil, err
	}

	for _, track := range record.TracksHistoryList {
		if track.ID == trackID {
			return &track, nil
		}
	}
	return nil, fmt.Errorf("track not found")
}

func (s *Storage) UpdateTrackDuration(guildID, trackID string, duration time.Duration) error {
	record, err := s.ReadGuild(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == trackID {
			record.TracksHistoryList[i].TotalDuration += duration
			return s.UpdateGuild(guildID, record)
		}
	}
	return fmt.Errorf("track not found")
}

func (s *Storage) UpdateTrackCountByOne(guildID, trackID string) error {
	record, err := s.ReadGuild(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == trackID {
			record.TracksHistoryList[i].TotalCount++
			return s.UpdateGuild(guildID, record)
		}
	}
	return fmt.Errorf("track not found")
}
