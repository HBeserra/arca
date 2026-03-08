package services

import (
	"context"
	"fmt"

	"changeme/internal/business/enginebus"
)

// AppStatus is the JSON-serialisable view of the system status.
type AppStatus struct {
	LoadedModels bool         `json:"loadedModels"`
	Memory       MemoryStatus `json:"memory"`
	Cpu          CpuStatus    `json:"cpu"`
}

// MemoryStatus carries memory usage info.
type MemoryStatus struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedPct        float64 `json:"usedPct"`
}

// CpuStatus carries CPU usage info.
type CpuStatus struct {
	ProcessUsagePct float64 `json:"processUsagePct"`
	SystemUsagePct  float64 `json:"systemUsagePct"`
}

// MonitorService exposes system stats to the frontend.
type MonitorService struct {
	eng enginebus.ExtEngine
}

// NewMonitorService returns a new MonitorService.
func NewMonitorService(eng enginebus.ExtEngine) *MonitorService {
	return &MonitorService{eng: eng}
}

// GetStatus returns current memory and CPU statistics.
func (s *MonitorService) GetStatus() (*AppStatus, error) {
	ctx := context.Background()

	st, err := s.eng.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("monitor: get status: %w", err)
	}

	return &AppStatus{
		LoadedModels: st.LoadedModels,
		Memory: MemoryStatus{
			TotalBytes:     st.Memory.Total,
			UsedBytes:      st.Memory.Used,
			AvailableBytes: st.Memory.Available,
			UsedPct:        st.Memory.UsedPct,
		},
		Cpu: CpuStatus{
			ProcessUsagePct: st.Cpu.ProcessUsagePct,
			SystemUsagePct:  st.Cpu.SystemUsagePct,
		},
	}, nil
}
