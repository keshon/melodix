package storage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/keshon/melodix/datastore"
)

const (
	commandHistoryLimit int = 20
	tracksHistoryLimit  int = 12
)

type Storage struct {
	ds *datastore.DataStore
}

type CommandHistoryRecord struct {
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	GuildName   string    `json:"guild_name"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Command     string    `json:"command"`
	Param       string    `json:"param"`
	Datetime    time.Time `json:"datetime"`
}

type TracksHistoryRecord struct {
	ID            string    `json:"id"`
	Title         string    `json:"name"`
	SourceType    string    `json:"source_type"`
	PublicLink    string    `json:"public_link"`
	TotalCount    int       `json:"total_count"`
	TotalDuration float64   `json:"total_duration"`
	LastPlayed    time.Time `json:"last_played"`
}

type Record struct {
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

// Helper function to get or create a Record for a guild
func (s *Storage) getOrCreateGuildRecord(guildID string) (*Record, error) {
	data, exists := s.ds.Get(guildID)
	if !exists {
		newRecord := &Record{
			PrefPrefix:          "",
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

	if len(record.CommandsHistoryList) > commandHistoryLimit {
		record.CommandsHistoryList = record.CommandsHistoryList[len(record.CommandsHistoryList)-20:]
	}

	if len(record.TracksHistoryList) > tracksHistoryLimit {
		record.TracksHistoryList = record.TracksHistoryList[len(record.TracksHistoryList)-20:]
	}

	return &record, nil
}

// AppendCommandToHistory appends a command history record for a guild
func (s *Storage) AppendCommandToHistory(guildID string, command CommandHistoryRecord) error {

	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	record.CommandsHistoryList = append(record.CommandsHistoryList, command)
	s.ds.Add(guildID, record)
	return nil
}

// AppendTrackToHistory appends a track to the track history or updates it if it already exists
func (s *Storage) AppendTrackToHistory(guildID string, track TracksHistoryRecord) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	track.Title = sanitizeString(track.Title)

	for i, existingTrack := range record.TracksHistoryList {
		if existingTrack.ID == track.ID {
			// Update LastPlayed instead of blocking on duplicates
			record.TracksHistoryList[i].LastPlayed = time.Now()
			s.ds.Add(guildID, record)
			return nil
		}
	}

	// If track is not found, create a new entry
	track.LastPlayed = time.Now()
	record.TracksHistoryList = append(record.TracksHistoryList, track)
	s.ds.Add(guildID, record)
	return nil
}

// AddTrackCountByOne increments the play count for a track in a guild
func (s *Storage) AddTrackCountByOne(guildID, ID, Title, sourceType, publicLink string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == ID {
			record.TracksHistoryList[i].TotalCount++
			record.TracksHistoryList[i].LastPlayed = time.Now()

			if record.TracksHistoryList[i].Title != Title {
				record.TracksHistoryList[i].Title = Title
			}
			if record.TracksHistoryList[i].PublicLink != publicLink {
				record.TracksHistoryList[i].PublicLink = publicLink
			}
			s.ds.Add(guildID, record)
			return nil
		}
	}

	// If track is not found, create a new entry
	newTrack := TracksHistoryRecord{
		ID:         ID,
		TotalCount: 1,
		LastPlayed: time.Now(),
		Title:      Title,
		SourceType: sourceType,
		PublicLink: publicLink,
	}
	record.TracksHistoryList = append(record.TracksHistoryList, newTrack)
	s.ds.Add(guildID, record)
	return nil
}

// AddTrackDuration increments the play duration for a track in a guild
func (s *Storage) AddTrackDuration(guildID, ID, Title, sourceType, publicLink string, duration time.Duration) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	for i, track := range record.TracksHistoryList {
		if track.ID == ID {
			record.TracksHistoryList[i].TotalDuration += duration.Seconds()
			record.TracksHistoryList[i].LastPlayed = time.Now()

			if record.TracksHistoryList[i].Title != Title {
				record.TracksHistoryList[i].Title = Title
			}
			if record.TracksHistoryList[i].PublicLink != publicLink {
				record.TracksHistoryList[i].PublicLink = publicLink
			}
			s.ds.Add(guildID, record)
			return nil
		}
	}

	// If track is not found, create a new entry
	newTrack := TracksHistoryRecord{
		ID:         ID,
		TotalCount: 1,
		LastPlayed: time.Now(),
		Title:      Title,
		SourceType: sourceType,
		PublicLink: publicLink,
	}
	record.TracksHistoryList = append(record.TracksHistoryList, newTrack)
	s.ds.Add(guildID, record)
	return nil
}

func (s *Storage) FetchCommandHistory(guildID string) ([]CommandHistoryRecord, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}

	return record.CommandsHistoryList, nil
}

func (s *Storage) FetchTrackHistory(guildID string) ([]TracksHistoryRecord, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return nil, err
	}

	return record.TracksHistoryList, nil
}

// SavePrefix saves or updates the prefix for a specific guild in storage.
func (s *Storage) SavePrefix(guildID, prefix string) error {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return err
	}

	record.PrefPrefix = prefix
	s.ds.Add(guildID, record)
	return nil
}

// FetchPrefix retrieves the prefix for a specific guild from storage.
func (s *Storage) FetchPrefix(guildID string) (string, error) {
	record, err := s.getOrCreateGuildRecord(guildID)
	if err != nil {
		return "", err
	}

	return record.PrefPrefix, nil
}

func sanitizeString(input string) string {
	// keep only ASCII characters
	re := regexp.MustCompile(`[^\x20-\x7E]`)
	return re.ReplaceAllString(input, "")
}
