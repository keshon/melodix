package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/keshon/melodix/internal/domain"

	"github.com/keshon/datastore"
)

const commandHistoryLimit int = 50

type Storage struct {
	ds *datastore.DataStore
}

func New(ctx context.Context, filePath string) (*Storage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ds, err := datastore.New(ctx, filePath)
	if err != nil {
		return nil, err
	}
	return &Storage{ds: ds}, nil
}

func (s *Storage) Close(ctx context.Context) error {
	if s == nil || s.ds == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	done := make(chan error, 1)
	go func() {
		done <- s.ds.Close()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		log.Printf("[ERR] datastore close timed out: %v", ctx.Err())
		return ctx.Err()
	}
}

func (s *Storage) getOrCreateGuildRecord(guildID string) (*domain.Record, error) {
	var record domain.Record
	exists, err := s.ds.Get(guildID, &record)
	if err != nil {
		return nil, fmt.Errorf("error getting guild record: %w", err)
	}
	if !exists {
		newRecord := &domain.Record{}
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

func (s *Storage) GuildRecord(guildID string) (*domain.Record, error) {
	return s.getOrCreateGuildRecord(guildID)
}

func (s *Storage) Records() map[string]domain.Record {
	mapStringRecord := make(map[string]domain.Record)
	for _, key := range s.ds.Keys() {
		var record domain.Record
		exists, err := s.ds.Get(key, &record)
		if err != nil {
			log.Printf("error getting record for key %q: %v", key, err)
			continue
		}
		if !exists {
			continue
		}
		mapStringRecord[key] = record
	}
	return mapStringRecord
}

func (s *Storage) appendCommandToHistory(guildID string, command domain.CommandHistory) error {

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
	record := domain.CommandHistory{
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
