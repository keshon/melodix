// Package jobmgr provides simple synchronous and asynchronous job execution
// with cancellation, status callbacks, and in-memory tracking of running jobs.
//
// Typical usage:
//
//	jm := jobmgr.NewManager(func(msg string) {
//	    log.Println("JOB:", msg)
//	})
//
//	err := jm.StartAsync("sync-users", func(ctx context.Context) error {
//	    // do work until ctx is cancelled
//	    return nil
//	})
//
//	// later...
//	_ = jm.Stop("sync-users")
//
// The package is intentionally minimal: no retry logic, no workers, no persistence.
// Jobs run in separate goroutines and are automatically removed on completion.
package jobmgr

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DefaultManager is the global job manager.
var DefaultManager = NewManager(nil)

// Job represents a running unit of work.
// Jobs are added and removed by Manager automatically.
type Job struct {
	Name   string
	Cancel context.CancelFunc
}

// StatusReporter receives lifecycle events for jobs.
// Example messages:
//
//	running:get
//	error:get:failed to connect
//	done:get
type StatusReporter func(string)

// Manager orchestrates starting, stopping and tracking jobs.
// It is safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	Reporter StatusReporter
}

// NewManager creates a new Manager.
// The reporter callback may be nil.
func NewManager(reporter StatusReporter) *Manager {
	return &Manager{
		jobs:     make(map[string]*Job),
		Reporter: reporter,
	}
}

// StartSync runs a job in the current goroutine and blocks until completion.
// Use this for tasks that must run synchronously.
func (m *Manager) StartSync(name string, runner func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return runner(ctx)
}

// StartAsync runs a job in a separate goroutine and returns immediately.
// If a job with the same name is already running, an error is returned.
// Jobs are removed automatically after completion (success or failure).
func (m *Manager) StartAsync(name string, runner func(ctx context.Context) error) error {
	m.mu.Lock()
	if _, exists := m.jobs[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("job '%s' is already running", name)
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{Name: name, Cancel: cancel}

	m.mu.Lock()
	m.jobs[name] = job
	m.mu.Unlock()

	go func() {
		m.report("running:" + name)

		err := runner(ctx)
		if err != nil {
			m.report("error:" + name + ":" + err.Error())
		} else {
			m.report("done:" + name)
		}

		m.mu.Lock()
		delete(m.jobs, name)
		m.mu.Unlock()
	}()

	return nil
}

// Stop cancels a running job by name.
// If the job is not running, an error is returned.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name]
	if !ok {
		return fmt.Errorf("job '%s' not running", name)
	}

	job.Cancel()
	delete(m.jobs, name)
	return nil
}

// List returns the list of active job names.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, 0, len(m.jobs))
	for k := range m.jobs {
		out = append(out, k)
	}
	return out
}

// Status returns a human-readable summary of active jobs.
// Example:
//
//	"Running jobs: get, analyze"
//
// If none are running: "No jobs are running."
func (m *Manager) Status() string {
	active := m.List()
	if len(active) == 0 {
		return "No jobs are running."
	}
	return fmt.Sprintf("Running jobs: %s", strings.Join(active, ", "))
}

// report delivers lifecycle messages to the reporter if present.
func (m *Manager) report(s string) {
	if m.Reporter != nil {
		m.Reporter(s)
	}
}
