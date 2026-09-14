package perm

import "github.com/keshon/melodix/internal/config"

// IsDeveloper reports whether a user ID matches the configured developer.
// Delegates to config for a single source of truth.
func IsDeveloper(cfg *config.Config, userID string) bool {
	return config.IsDeveloper(cfg, userID)
}
