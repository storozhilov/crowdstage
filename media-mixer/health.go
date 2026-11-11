package main

import (
	"encoding/json"
	"net/http"
)

const (
	statusOK       = "ok"
	statusDegraded = "degraded"

	mixerStateIdle    = "idle"
	mixerStateRunning = "running"
	mixerStateFailed  = "failed"
)

type HealthResponse struct {
	Status     string     `json:"status"`
	Subsystems Subsystems `json:"subsystems"`
}

type Subsystems struct {
	Mixer MixerHealth `json:"mixer"`
}

type MixerHealth struct {
	State           string `json:"state"`
	SourceRoom      string `json:"source_room"`
	TargetRoom      string `json:"target_room"`
	ForwardedTracks int    `json:"forwarded_tracks"`
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
