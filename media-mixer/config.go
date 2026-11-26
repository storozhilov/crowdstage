package main

import (
	"os"

	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
)

type config struct {
	ListenAddr string
	LiveKitURL string
	APIKey     string
	APISecret  string
	InputRoom  string
	GuestRoom  string
	MosaicRoom string
	OutputRoom string
	Identity   string
}

func configFromEnv() (config, error) {
	defaultMosaicRoom := sharedconfig.EnvOrDefault("MOSAIC_TARGET_ROOM", "mosaic")
	config := config{
		ListenAddr: sharedconfig.EnvOrDefault("MEDIA_MIXER_LISTEN_ADDR", ":7981"),
		LiveKitURL: os.Getenv("LIVEKIT_URL"),
		APIKey:     os.Getenv("LIVEKIT_API_KEY"),
		APISecret:  os.Getenv("LIVEKIT_API_SECRET"),
		InputRoom:  sharedconfig.EnvOrDefault("MEDIA_MIXER_SOURCE_ROOM", "studio"),
		GuestRoom:  sharedconfig.EnvOrDefault("MEDIA_MIXER_GUEST_ROOM", "guests"),
		MosaicRoom: sharedconfig.EnvOrDefault("MEDIA_MIXER_MOSAIC_ROOM", defaultMosaicRoom),
		OutputRoom: sharedconfig.EnvOrDefault("MEDIA_MIXER_TARGET_ROOM", "broadcast"),
		Identity:   sharedconfig.EnvOrDefault("MEDIA_MIXER_IDENTITY", "media-mixer"),
	}

	if err := sharedconfig.Require("LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"); err != nil {
		return config, err
	}

	return config, nil
}
