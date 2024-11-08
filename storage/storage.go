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
	GuildName   string    `json:"guild_name"`
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
	PrefPrefix          string                 `json:"pref_prefix"`
	ModUsersList        []string               `json:"mod_users"`
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

// Helper function to get or create a Record for a guild
func (s *Storage) getOrCreateGuildRecord(guildID string) (*Record, error) {
	data, exists := s.ds.Get(guildID)
	if !exists {
		newRecord := &Record{
			PrefPrefix:          "",
			ModUsersList:        []string{},
			CommandsHistoryList: []CommandHistoryRecord{},
			TracksHistoryList:   []TracksHistoryRecord{},
		}
		s.ds.Add(guildID, newRecord)
		return newRecord, nil
	}

	// Try to convert `data` (map[string]interface{}) into JSON format
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("error marshalling data: %w", err)
	}

	// Unmarshal JSON data into the Record struct
	var record Record
	err = json.Unmarshal(jsonData, &record)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling to *Record: %w", err)
	}

	return &record, nil
}

// AppendCommandToHistory appends a command history record for a guild
func (s *Storage) AppendCommandToHistory(guildID string, command CommandHistoryRecord) error {
	fmt.Println(guildID)
	fmt.Println(command)
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	fmt.Println(record)

	record.CommandsHistoryList = append(record.CommandsHistoryList, command)
	s.ds.Add(guildID, record)
	return nil
}

// AppendTrackToHistory appends a track to the track history if it doesn't already exist
func (s *Storage) AppendTrackToHistory(guildID string, track TracksHistoryRecord) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for _, existingTrack := range record.TracksHistoryList {
		if existingTrack.ID == track.ID {
			return fmt.Errorf("track with ID %s already exists in guild %s", track.ID, guildID)
		}
	}

	record.TracksHistoryList = append(record.TracksHistoryList, track)
	s.ds.Add(guildID, record)
	return nil
}

// AddTrackCountByOne increments the play count for a given track in a guild
func (s *Storage) AddTrackCountByOne(guildID, trackID string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == trackID {
			record.TracksHistoryList[i].TotalCount++
			s.ds.Add(guildID, record)
			return nil
		}
	}
	return fmt.Errorf("track with ID %s not found in guild %s", trackID, guildID)
}

// AddTrackDuration increments the play duration for a given track in a guild
func (s *Storage) AddTrackDuration(guildID, trackID string, duration time.Duration) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == trackID {
			record.TracksHistoryList[i].TotalDuration += duration
			s.ds.Add(guildID, record)
			return nil
		}
	}
	return fmt.Errorf("track with ID %s not found in guild %s", trackID, guildID)
}
