package env

import (
	"os"
	"strconv"
	"time"
)

func GetEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func GetEnvAsInt(key string, fallback int) int {
	valueStr := GetEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return fallback
}

func GetEnvAsFloat64(key string, fallback float64) float64 {
	valueStr := GetEnv(key, "")
	if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return value
	}
	return fallback
}

func GetEnvAsFloat32(key string, fallback float32) float32 {
	valueStr := GetEnv(key, "")
	if value, err := strconv.ParseFloat(valueStr, 32); err == nil {
		return float32(value)
	}
	return fallback
}

func GetEnvAsBool(key string, fallback bool) bool {
	valueStr := GetEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return fallback
}

func GetEnvAsDuration(key string, fallback time.Duration) time.Duration {
	valueStr := GetEnv(key, "")
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}
	return fallback
}
