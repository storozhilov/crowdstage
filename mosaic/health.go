package main

import (
	"encoding/json"
	"net/http"
)

const (
	statusOK       = "ok"
	statusDegraded = "degraded"

	mosaicStateIdle    = "idle"
	mosaicStateRunning = "running"
)

type HealthResponse struct {
	Status     string     `json:"status"`
	Subsystems Subsystems `json:"subsystems"`
}

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
	State        string      `json:"state"`
	Participants int         `json:"participants"`
	Grid         *GridHealth `json:"grid,omitempty"`
}

type GridHealth struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

func healthHandler(health func() HealthResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := health()

		w.Header().Set("Content-Type", "application/json")
		statusCode := http.StatusOK
		if response.Status != statusOK {
			statusCode = http.StatusServiceUnavailable
		}
		w.WriteHeader(statusCode)

		_ = json.NewEncoder(w).Encode(response)
	}
}
