package main

import (
	"encoding/json"
	"net/http"
)

type PipelineResponse struct {
	Status string `json:"status"`
}

type PipelineErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func startPipelineHandler(start func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := start(); err != nil {
			writePipelineError(w, err)
			return
		}
		writePipelineResponse(w, "started")
	}
}

func stopPipelineHandler(stop func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := stop(); err != nil {
			writePipelineError(w, err)
			return
		}
		writePipelineResponse(w, "stopped")
	}
}

func writePipelineResponse(w http.ResponseWriter, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(PipelineResponse{Status: status})
}

func writePipelineError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(PipelineErrorResponse{
		Status: "error",
		Error:  err.Error(),
	})
}
