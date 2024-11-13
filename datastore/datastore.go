package datastore

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type DataStore struct {
	data   map[string]interface{} // in-memory data storage
	file   string                 // file path for persistent storage
	mu     sync.RWMutex           // mutex for thread-safe access
	ticker *time.Ticker           // ticker for periodic saving
	done   chan bool              // channel for shutting down the auto-save
}

func New(filePath string) (*DataStore, error) {
	store := &DataStore{
		data:   make(map[string]interface{}),
		file:   filePath,
		ticker: time.NewTicker(60 * time.Minute),
		done:   make(chan bool),
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := os.WriteFile(filePath, []byte("{}"), 0644); err != nil {
			return nil, fmt.Errorf("failed to create empty JSON file: %v", err)
		}
	} else if err == nil {
		if err := store.loadFromFile(); err != nil {
			return nil, fmt.Errorf("failed to load data from file: %v", err)
		}
	} else {
		return nil, fmt.Errorf("failed to check file existence: %v", err)
	}

	go store.autoSave()    // start the auto-save routine
	store.handleShutdown() // setup graceful shutdown

	return store, nil
}

func (ds *DataStore) Add(key string, value interface{}) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.data[key] = value
}

func (ds *DataStore) Get(key string) (interface{}, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	value, exists := ds.data[key]
	return value, exists
}

func (ds *DataStore) Delete(key string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.data, key)
}

func (ds *DataStore) saveToFile() error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	data, err := json.MarshalIndent(ds.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %v", err)
	}

	return os.WriteFile(ds.file, data, 0644)
}

func (ds *DataStore) loadFromFile() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	data, err := os.ReadFile(ds.file)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	return json.Unmarshal(data, &ds.data)
}

func (ds *DataStore) autoSave() {
	for {
		select {
		case <-ds.done:
			return
		case <-ds.ticker.C:
			if err := ds.saveToFile(); err != nil {
				fmt.Println("Auto-save error:", err)
			}
		}
	}
}

func (ds *DataStore) handleShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		ds.ticker.Stop()    // Stop the ticker
		ds.done <- true     // Signal auto-save to stop
		_ = ds.saveToFile() // Save data before shutting down
		os.Exit(0)
	}()
}

func (ds *DataStore) Close() error {
	ds.ticker.Stop()
	ds.done <- true
	return ds.saveToFile()
}
