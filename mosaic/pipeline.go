package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type PipelineResponse struct {
	Status string `json:"status"`
}

type PipelineErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

type startPipelineRequest struct {
	Columns *int `json:"columns"`
	Rows    *int `json:"rows"`
}

func startPipelineHandler(start func(columns, rows int) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		columns, rows, err := decodeStartPipelineRequest(r)
		if err != nil {
			writePipelineErrorStatus(w, http.StatusBadRequest, err)
			return
		}
		if err := start(columns, rows); err != nil {
			writePipelineError(w, err)
			return
		}
		writePipelineResponse(w, "started")
	}
}

func decodeStartPipelineRequest(r *http.Request) (columns, rows int, err error) {
	columns, rows = mosaicColumns, mosaicRows
	if r.Body == nil {
		return columns, rows, nil
	}

	var request startPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		if errors.Is(err, io.EOF) {
			return columns, rows, nil
		}
		return 0, 0, fmt.Errorf("decode start request: %w", err)
	}

	if request.Columns != nil {
		columns = *request.Columns
	}
	if request.Rows != nil {
		rows = *request.Rows
	}
	if columns <= 0 {
		return 0, 0, fmt.Errorf("grid columns must be positive")
	}
	if rows <= 0 {
		return 0, 0, fmt.Errorf("grid rows must be positive")
	}
	return columns, rows, nil
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
