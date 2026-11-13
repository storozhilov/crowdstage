package main

import sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"

const (
	mosaicStateIdle    = "idle"
	mosaicStateRunning = "running"
)

type HealthResponse = sharedhttp.HealthResponse[Subsystems]

type Subsystems struct {
	LiveKit LiveKitHealth `json:"livekit"`
	Mosaic  MosaicHealth  `json:"mosaic"`
}

type LiveKitHealth struct {
	Status          string `json:"status"`
	Connected       bool   `json:"connected"`
	URL             string `json:"url"`
	Room            string `json:"room"`
	Identity        string `json:"identity"`
	ConnectionState string `json:"connection_state"`
}

type MosaicHealth struct {
	State                  string      `json:"state"`
	Guests                 int         `json:"guests"`
	RotationTimeoutSeconds int         `json:"rotation_timeout_seconds,omitempty"`
	Grid                   *GridHealth `json:"grid,omitempty"`
}

type GridHealth struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}
