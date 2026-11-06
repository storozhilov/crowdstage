package main

import (
	"encoding/json"
	"net/http"
)

const (
	statusOK       = "ok"
	statusDegraded = "degraded"
)

type HealthResponse struct {
	Status     string     `json:"status"`
	Subsystems Subsystems `json:"subsystems"`
}

type Subsystems struct {
	LiveKit LiveKitHealth `json:"livekit"`
}

type LiveKitHealth struct {
	Status          string `json:"status"`
	Connected       bool   `json:"connected"`
	URL             string `json:"url"`
	Room            string `json:"room"`
	Identity        string `json:"identity"`
	ConnectionState string `json:"connection_state"`
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
