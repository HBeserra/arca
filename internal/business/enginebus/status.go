package enginebus

import (
	"context"
	"fmt"
)

type (
	Status struct {
		LoadedModels bool         `json:"loadedModels"`
		Memory       MemoryStatus `json:"memory"`
		Cpu          CpuStatus    `json:"cpu"`
		Gpu          GpuStatus    `json:"gpu"`
	}

	MemoryStatus struct {
		Total     uint64  `json:"total"`
		Used      uint64  `json:"used"`
		Available uint64  `json:"available"`
		UsedPct   float64 `json:"usedPct"`
	}

	CpuStatus struct {
		ProcessUsagePct float64 `json:"processUsagePct"`
		SystemUsagePct  float64 `json:"systemUsagePct"`
	}

	GpuStatus struct {
		TotalMemory    uint64  `json:"totalMemory"`
		UsedMemory     uint64  `json:"usedMemory"`
		FreeMemory     uint64  `json:"freeMemory"`
		UsedPct        float64 `json:"usedPct"`
		SystemUsagePct float64 `json:"systemUsagePct"`
	}
)

func (e *Engine) Status(ctx context.Context) (*Status, error) {
	if e.status == nil {
		return nil, fmt.Errorf("status monitor not initialized")
	}

	status, err := e.status.GetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	status.LoadedModels = e.LoadedModels()

	return status, nil
}
