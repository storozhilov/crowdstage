package main

import (
	"os"

	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
)

type config struct {
	ListenAddr     string
	LiveKitURL     string
	APIKey         string
	APISecret      string
	RoomName       string
	StudioRoomName string
	Identity       string
}

func configFromEnv() (config, error) {
	config := config{
		ListenAddr:     sharedconfig.EnvOrDefault("MOSAIC_LISTEN_ADDR", ":7980"),
		LiveKitURL:     os.Getenv("LIVEKIT_URL"),
		APIKey:         os.Getenv("LIVEKIT_API_KEY"),
		APISecret:      os.Getenv("LIVEKIT_API_SECRET"),
		RoomName:       sharedconfig.EnvOrDefault("MOSAIC_SOURCE_ROOM", "event-participants"),
		StudioRoomName: sharedconfig.EnvOrDefault("MOSAIC_TARGET_ROOM", "participants-mosaic"),
		Identity:       sharedconfig.EnvOrDefault("MOSAIC_IDENTITY", "mosaic-composer"),
	}

	if err := sharedconfig.Require("LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"); err != nil {
		return config, err
	}

	return config, nil
}
