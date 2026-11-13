package httpapi

import (
	"encoding/json"
	"net/http"
)

const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

type HealthResponse[T any] struct {
	Status     string `json:"status"`
	Subsystems T      `json:"subsystems"`
}

func HealthHandler[T any](health func() HealthResponse[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := health()

		w.Header().Set("Content-Type", "application/json")
		statusCode := http.StatusOK
		if response.Status != StatusOK {
			statusCode = http.StatusServiceUnavailable
		}
		w.WriteHeader(statusCode)

		_ = json.NewEncoder(w).Encode(response)
	}
}

type PipelineResponse struct {
	Status string `json:"status"`
}

type PipelineErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func StartHandler(start func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := start(); err != nil {
			WriteError(w, http.StatusInternalServerError, err)
			return
		}
		WriteResponse(w, "started")
	}
}

func StopHandler(stop func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := stop(); err != nil {
			WriteError(w, http.StatusInternalServerError, err)
			return
		}
		WriteResponse(w, "stopped")
	}
}

func WriteResponse(w http.ResponseWriter, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(PipelineResponse{Status: status})
}

func WriteError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(PipelineErrorResponse{
		Status: "error",
		Error:  err.Error(),
	})
}
