// Package envconfig reads environment variables using caller-supplied defaults.
package envconfig

import (
	"os"
	"strconv"
)

func String(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func Int(name string, fallback, min, max int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err == nil && value >= min && value <= max {
		return value
	}
	return fallback
}
