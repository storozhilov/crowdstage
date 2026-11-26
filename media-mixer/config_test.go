package main

import "testing"

func TestConfigFromEnvRequiresLiveKitSettings(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "")
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")

	if _, err := configFromEnv(); err == nil {
		t.Fatal("configFromEnv succeeded without the required LiveKit settings")
	}
}

func TestConfigFromEnvProvidesDefaults(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", "secret")
	t.Setenv("MEDIA_MIXER_LISTEN_ADDR", "")
	t.Setenv("MEDIA_MIXER_SOURCE_ROOM", "")
	t.Setenv("MEDIA_MIXER_GUEST_ROOM", "")
	t.Setenv("MEDIA_MIXER_MOSAIC_ROOM", "")
	t.Setenv("MOSAIC_TARGET_ROOM", "")
	t.Setenv("MEDIA_MIXER_TARGET_ROOM", "")
	t.Setenv("MEDIA_MIXER_IDENTITY", "")

	config, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}

	if config.ListenAddr != ":7981" {
		t.Fatalf("listen address = %q, want %q", config.ListenAddr, ":7981")
	}
	if config.InputRoom != "studio" {
		t.Fatalf("input room = %q, want %q", config.InputRoom, "studio")
	}
	if config.GuestRoom != "guests" {
		t.Fatalf("guest room = %q, want %q", config.GuestRoom, "guests")
	}
	if config.MosaicRoom != "mosaic" {
		t.Fatalf("mosaic room = %q, want %q", config.MosaicRoom, "mosaic")
	}
	if config.OutputRoom != "broadcast" {
		t.Fatalf("output room = %q, want %q", config.OutputRoom, "broadcast")
	}
	if config.Identity != "media-mixer" {
		t.Fatalf("identity = %q, want %q", config.Identity, "media-mixer")
	}
}

func TestConfigFromEnvDefaultsTheMosaicRoomToTheMosaicTargetRoom(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", "secret")
	t.Setenv("MEDIA_MIXER_MOSAIC_ROOM", "")
	t.Setenv("MOSAIC_TARGET_ROOM", "program")

	config, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}

	if config.MosaicRoom != "program" {
		t.Fatalf("mosaic room = %q, want %q", config.MosaicRoom, "program")
	}
}

func TestConfigFromEnvReadsOverrides(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", "secret")
	t.Setenv("MEDIA_MIXER_LISTEN_ADDR", ":9000")
	t.Setenv("MEDIA_MIXER_SOURCE_ROOM", "source")
	t.Setenv("MEDIA_MIXER_GUEST_ROOM", "audience")
	t.Setenv("MEDIA_MIXER_MOSAIC_ROOM", "program")
	t.Setenv("MEDIA_MIXER_TARGET_ROOM", "target")
	t.Setenv("MEDIA_MIXER_IDENTITY", "mixer")

	config, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}

	if config.ListenAddr != ":9000" {
		t.Fatalf("listen address = %q, want %q", config.ListenAddr, ":9000")
	}
	if config.InputRoom != "source" {
		t.Fatalf("input room = %q, want %q", config.InputRoom, "source")
	}
	if config.GuestRoom != "audience" {
		t.Fatalf("guest room = %q, want %q", config.GuestRoom, "audience")
	}
	if config.MosaicRoom != "program" {
		t.Fatalf("mosaic room = %q, want %q", config.MosaicRoom, "program")
	}
	if config.OutputRoom != "target" {
		t.Fatalf("output room = %q, want %q", config.OutputRoom, "target")
	}
	if config.Identity != "mixer" {
		t.Fatalf("identity = %q, want %q", config.Identity, "mixer")
	}
}
