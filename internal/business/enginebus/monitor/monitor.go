package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type (
	Business struct {
		localCache *statusCache
		process    *process.Process
		logger     *slog.Logger
	}

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

	statusCache struct {
		CreatedAt      time.Time // last time all the status info was fetched; used for caching
		LastUpdate     time.Time // last time the status info was updated; used for cpu/gpu load calculations
		Status         *Status
		Err            error
		RecreateTicker *time.Ticker // used to trigger periodic status refreshes
		RefreshTicker  *time.Ticker // used to trigger periodic status updates (cpu/gpu load)
	}
)

var (
	ErrStatusNotAvailable = fmt.Errorf("status not available")
)

func New(ctx context.Context, logger *slog.Logger, refreshInterval time.Duration) *Business {
	localCache := statusCache{
		CreatedAt:      time.Time{},
		LastUpdate:     time.Time{},
		Status:         nil,
		Err:            nil,
		RecreateTicker: time.NewTicker(5 * time.Minute), // full refresh every 5 minutes
		RefreshTicker:  time.NewTicker(refreshInterval), // update every refreshInterval
	}

	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		panic(fmt.Errorf("failed to get process: %w", err))
	}

	b := &Business{
		localCache: &localCache,
		process:    p,
		logger:     logger.With("component", "monitor"),
	}

	go func() {
		for {
			select {
			case <-localCache.RecreateTicker.C:
				localCache.Status, localCache.Err = b.fetchStatus()
				localCache.CreatedAt = time.Now()
			case <-localCache.RefreshTicker.C:
				if localCache.Status != nil {
					localCache.Status.Cpu, localCache.Err = b.fetchCpuStatus()
					localCache.Status.Gpu, localCache.Err = b.fetchGpuStatus()
					localCache.LastUpdate = time.Now()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	status, err := b.fetchStatus() // Initial fetch to populate cache immediately
	b.localCache.Status = status
	b.localCache.Err = err
	b.localCache.CreatedAt = time.Now()

	return b
}

func (b *Business) GetStatus(ctx context.Context) (*Status, error) {
	if b.localCache.Status == nil || time.Since(b.localCache.CreatedAt) > 5*time.Minute {
		return nil, ErrStatusNotAvailable
	}

	if b.localCache.Err != nil {
		return nil, b.localCache.Err
	}

	return b.localCache.Status, nil
}

func (b *Business) fetchStatus() (*Status, error) {

	memStats, err := b.fetchMemoryStatus()
	if err != nil {
		return nil, fmt.Errorf("fetch memory status: %w", err)
	}

	cpuStats, err := b.fetchCpuStatus()
	if err != nil {
		return nil, fmt.Errorf("fetch cpu status: %w", err)
	}

	gpuStats, err := b.fetchGpuStatus()
	if err != nil {
		return nil, fmt.Errorf("fetch gpu status: %w", err)
	}

	return &Status{
		LoadedModels: false, // Placeholder; actual model loading status would require additional logic
		Memory:       memStats,
		Cpu:          cpuStats,
		Gpu:          gpuStats,
	}, nil
}

func (b *Business) fetchCpuStatus() (CpuStatus, error) {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return CpuStatus{}, fmt.Errorf("fetch CPU status: %w", err)
	}

	cpuPercent, err := p.CPUPercent()
	if err != nil {
		return CpuStatus{}, fmt.Errorf("fetch CPU status: %w", err)
	}

	systemCpuPercent, err := cpu.Percent(time.Second, false)

	if err != nil || len(systemCpuPercent) == 0 {
		return CpuStatus{}, fmt.Errorf("fetch CPU status: %w", err)
	}

	return CpuStatus{
		ProcessUsagePct: cpuPercent,
		SystemUsagePct:  systemCpuPercent[0],
	}, nil
}

func (b *Business) fetchGpuStatus() (GpuStatus, error) {
	return GpuStatus{
		TotalMemory:    0,
		UsedMemory:     0,
		FreeMemory:     0,
		UsedPct:        0,
		SystemUsagePct: 0, // Placeholder; actual system usage would require additional logic
	}, nil
}

func (b *Business) fetchMemoryStatus() (MemoryStatus, error) {
	// memInfo, err := b.process.MemoryInfo()
	// if err != nil {
	// 	return MemoryStatus{}, fmt.Errorf("fetch memory status: %w", err)
	// }

	virtualMem, err := mem.VirtualMemory()
	if err != nil {
		return MemoryStatus{}, fmt.Errorf("fetch virtual memory status: %w", err)
	}

	total := virtualMem.Total
	used := virtualMem.Used + virtualMem.Wired
	available := virtualMem.Available
	usedPct := virtualMem.UsedPercent

	return MemoryStatus{
		Total:     total,
		Used:      used,
		Available: available,
		UsedPct:   usedPct,
	}, nil
}
