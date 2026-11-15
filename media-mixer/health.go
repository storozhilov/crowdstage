package main

import sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"

const (
	mixerStateIdle    = "idle"
	mixerStateRunning = "running"
	mixerStateFailed  = "failed"

	mixerOutputCount = 1
)

type HealthResponse = sharedhttp.HealthResponse[Subsystems]

type Subsystems struct {
	LiveKit LiveKitHealth `json:"livekit"`
	Mixer   MixerHealth   `json:"mixer"`
}

type LiveKitHealth struct {
	Status          string `json:"status"`
	Connected       bool   `json:"connected"`
	URL             string `json:"url"`
	Room            string `json:"room"`
	TargetRoom      string `json:"target_room"`
	Identity        string `json:"identity"`
	ConnectionState string `json:"connection_state"`
}

type MixerHealth struct {
	State                           string   `json:"state"`
	Inputs                          int      `json:"inputs"`
	Outputs                         int      `json:"outputs"`
	BroadcastedParticipantIdentity  string   `json:"broadcasted_participant_identity"`
	PublishingParticipantIdentities []string `json:"publishing_participant_identities"`
}
