package cmd

import "sort"

// DefaultRegistry is the global registry used by adapters (Discord, CLI, etc.).
var DefaultRegistry = NewRegistry()

// Registry stores commands by name. It does not perform dispatch; each adapter
// (CLI, Discord, HTTP) looks up commands and invokes them with its own context.
type Registry struct {
	commands map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds a command. Usually called from init() or adapter setup.
func (r *Registry) Register(c Command) {
	r.commands[c.Name()] = c
}

// Get returns the command with the given name, or nil.
func (r *Registry) Get(name string) Command {
	return r.commands[name]
}

// GetAll returns all registered commands, sorted by name.
func (r *Registry) GetAll() []Command {
	list := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name() < list[j].Name()
	})
	return list
}
