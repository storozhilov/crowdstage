package main

import "testing"

func TestConfigFromEnvUsesDevelopmentDefaults(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", "secret")
	t.Setenv("MOSAIC_LISTEN_ADDR", "")
	t.Setenv("MOSAIC_ROOM", "")
	t.Setenv("MOSAIC_STUDIO_ROOM", "")
	t.Setenv("MOSAIC_IDENTITY", "")
	t.Setenv("MOSAIC_RECORDINGS_DIR", "")

	config, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv() error = %v", err)
	}

	if config.ListenAddr != ":7980" {
		t.Errorf("ListenAddr = %q, want %q", config.ListenAddr, ":7980")
	}
	if config.RoomName != "mosaic-composer" {
		t.Errorf("RoomName = %q, want %q", config.RoomName, "mosaic-composer")
	}
	if config.StudioRoomName != "studio" {
		t.Errorf("StudioRoomName = %q, want %q", config.StudioRoomName, "studio")
	}
	if config.Identity != "mosaic-composer" {
		t.Errorf("Identity = %q, want %q", config.Identity, "mosaic-composer")
	}
	if config.RecordingsDir != "/tmp/mosaic-recordings" {
		t.Errorf("RecordingsDir = %q, want %q", config.RecordingsDir, "/tmp/mosaic-recordings")
	}
}

func TestConfigFromEnvRequiresLiveKitSettings(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "")
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")

	if _, err := configFromEnv(); err == nil {
		t.Fatal("configFromEnv() error = nil, want an error")
	}
}
