package main

import (
	"fmt"
	"os"
	"strings"
)

type config struct {
	ListenAddr string
	LiveKitURL string
	APIKey     string
	APISecret  string
	SourceRoom string
	TargetRoom string
	Identity   string
}

func configFromEnv() (config, error) {
	config := config{
		ListenAddr: envOrDefault("MEDIA_MIXER_LISTEN_ADDR", ":7981"),
		LiveKitURL: os.Getenv("LIVEKIT_URL"),
		APIKey:     os.Getenv("LIVEKIT_API_KEY"),
		APISecret:  os.Getenv("LIVEKIT_API_SECRET"),
		SourceRoom: envOrDefault("MEDIA_MIXER_SOURCE_ROOM", "studio"),
		TargetRoom: envOrDefault("MEDIA_MIXER_TARGET_ROOM", "broadcast"),
		Identity:   envOrDefault("MEDIA_MIXER_IDENTITY", "media-mixer"),
	}

	var missing []string
	if config.LiveKitURL == "" {
		missing = append(missing, "LIVEKIT_URL")
	}
	if config.APIKey == "" {
		missing = append(missing, "LIVEKIT_API_KEY")
	}
	if config.APISecret == "" {
		missing = append(missing, "LIVEKIT_API_SECRET")
	}
	if len(missing) > 0 {
		return config, fmt.Errorf("missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
