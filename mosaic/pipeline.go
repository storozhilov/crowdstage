package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type PipelineResponse struct {
	Status string `json:"status"`
}

type PipelineErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

const maxRotationTimeoutSeconds = int64((1<<63 - 1) / int64(time.Second))

type startPipelineRequest struct {
	Columns                *int `json:"columns"`
	Rows                   *int `json:"rows"`
	RotationTimeoutSeconds *int `json:"rotation_timeout_seconds"`
}

func startPipelineHandler(start func(columns, rows int, rotationTimeout time.Duration) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		columns, rows, rotationTimeout, err := decodeStartPipelineRequest(r)
		if err != nil {
			writePipelineErrorStatus(w, http.StatusBadRequest, err)
			return
		}
		if err := start(columns, rows, rotationTimeout); err != nil {
			writePipelineError(w, err)
			return
		}
		writePipelineResponse(w, "started")
	}
}

func decodeStartPipelineRequest(r *http.Request) (columns, rows int, rotationTimeout time.Duration, err error) {
	columns, rows = mosaicColumns, mosaicRows
	rotationTimeout = defaultRotationTimeout
	if r.Body == nil {
		return columns, rows, rotationTimeout, nil
	}

	var request startPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		if errors.Is(err, io.EOF) {
			return columns, rows, rotationTimeout, nil
		}
		return 0, 0, 0, fmt.Errorf("decode start request: %w", err)
	}

	if request.Columns != nil {
		columns = *request.Columns
	}
	if request.Rows != nil {
		rows = *request.Rows
	}
	if request.RotationTimeoutSeconds != nil {
		seconds := int64(*request.RotationTimeoutSeconds)
		if seconds <= 0 {
			return 0, 0, 0, fmt.Errorf("rotation timeout must be a positive number of seconds")
		}
		if seconds > maxRotationTimeoutSeconds {
			return 0, 0, 0, fmt.Errorf("rotation timeout %d is too large; maximum is %d seconds", seconds, maxRotationTimeoutSeconds)
		}
		rotationTimeout = time.Duration(seconds) * time.Second
	}
	if err := validateMosaicGrid(columns, rows); err != nil {
		return 0, 0, 0, err
	}
	return columns, rows, rotationTimeout, nil
}

func validateMosaicGrid(columns, rows int) error {
	if columns <= 0 {
		return fmt.Errorf("grid columns must be positive")
	}
	if rows <= 0 {
		return fmt.Errorf("grid rows must be positive")
	}
	if rows > maxMosaicCapacity || columns > maxMosaicCapacity/rows {
		return fmt.Errorf("grid capacity must not exceed %d tiles", maxMosaicCapacity)
	}

	_, _, width, height := participantGridPositionForGrid(0, columns, rows)
	if width <= 0 || height <= 0 {
		return fmt.Errorf("grid %dx%d does not fit the %dx%d canvas", columns, rows, backgroundWidth, backgroundHeight)
	}
	return nil
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
	writePipelineErrorStatus(w, http.StatusInternalServerError, err)
}

func writePipelineErrorStatus(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(PipelineErrorResponse{
		Status: "error",
		Error:  err.Error(),
	})
}
