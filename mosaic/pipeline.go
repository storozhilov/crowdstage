package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

type PipelineResponse = sharedhttp.PipelineResponse

type PipelineErrorResponse = sharedhttp.PipelineErrorResponse

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
			sharedhttp.WriteError(w, http.StatusBadRequest, err)
			return
		}
		if err := start(columns, rows, rotationTimeout); err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, err)
			return
		}
		sharedhttp.WriteResponse(w, "started")
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
