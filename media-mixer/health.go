package main

import sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"

const (
	mixerStateIdle    = "idle"
	mixerStateRunning = "running"
	mixerStateFailed  = "failed"

	mixerOutputCount = 1
)

type HealthResponse = sharedhttp.HealthResponse[Subsystems]

type Subsystems struct {
	Mixer MixerHealth `json:"mixer"`
}

type MixerHealth struct {
	State   string `json:"state"`
	Inputs  int    `json:"inputs"`
	Outputs int    `json:"outputs"`
}
