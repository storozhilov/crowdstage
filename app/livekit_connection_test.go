package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
)

func TestLiveKitAuthenticatedConnection(t *testing.T) {
	url := os.Getenv("LIVEKIT_URL")
	if url == "" {
		t.Skip("LIVEKIT_URL is not set; run tests against the dev stand with ci/run_tests.sh")
	}

	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required")
	}

	suffix := time.Now().UnixNano()
	roomName := fmt.Sprintf("connection-test-%d", suffix)
	identity := fmt.Sprintf("connection-test-%d", suffix)

	room, err := lksdk.ConnectToRoom(url, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: identity,
	}, &lksdk.RoomCallback{}, lksdk.WithConnectTimeout(30*time.Second))
	if err != nil {
		t.Fatalf("connect to LiveKit at %s: %v", url, err)
	}
	defer room.Disconnect()

	if got := room.ConnectionState(); got != lksdk.ConnectionStateConnected {
		t.Fatalf("connection state = %v, want %v", got, lksdk.ConnectionStateConnected)
	}
	if got := room.Name(); got != roomName {
		t.Fatalf("connected room name = %q, want %q", got, roomName)
	}
	if got := room.LocalParticipant.Identity(); got != identity {
		t.Fatalf("connected participant identity = %q, want %q", got, identity)
	}
}
