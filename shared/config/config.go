package config

import (
	"fmt"
	"os"
	"strings"
)

func EnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func Require(names ...string) error {
	var missing []string
	for _, name := range names {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variable(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
