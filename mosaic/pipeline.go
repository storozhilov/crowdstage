package main

import (
	"encoding/json"
	"net/http"
)

type PipelineResponse struct {
	Status string `json:"status"`
}

func startPipelineHandler(start func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start()
		writePipelineResponse(w, "started")
	}
}

func stopPipelineHandler(stop func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stop()
		writePipelineResponse(w, "stopped")
	}
}

func writePipelineResponse(w http.ResponseWriter, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(PipelineResponse{Status: status})
}
