package monitor_test

import (
	"changeme/internal/business/enginebus/monitor"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitor_not_nil(t *testing.T) {
	mon := monitor.New(context.Background(), slog.Default(), 1*time.Second)
	require.NotNil(t, mon)

	statsCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	sts, err := mon.GetStatus(statsCtx)
	require.NoError(t, err)
	require.NotNil(t, sts)

	require.NotNil(t, sts.Memory)
	require.NotNil(t, sts.Cpu)
	require.NotNil(t, sts.Gpu)

	assert.False(t, sts.LoadedModels)

	// Memory
	assert.Greater(t, sts.Memory.Total, uint64(0))
	assert.Greater(t, sts.Memory.Used, uint64(0))

	// CPU
	assert.Greater(t, sts.Cpu.ProcessUsagePct, float64(0))
	assert.Greater(t, sts.Cpu.SystemUsagePct, float64(0))

	totalGB := float64(sts.Memory.Total) / (1024 * 1024 * 1024)
	usedGB := float64(sts.Memory.Used) / (1024 * 1024 * 1024)

	t.Logf("Memory: Total GB=%.2f, Used=%.2f", totalGB, usedGB)

	raw, err := mem.VirtualMemory()
	require.NoError(t, err)

	memoryFields := reflect.TypeOf(*raw).NumField()
	for i := 0; i < memoryFields; i++ {
		field := reflect.TypeOf(*raw).Field(i)
		value := reflect.ValueOf(*raw).Field(i)

		switch field.Type.Kind() {
		case reflect.Uint64:
			t.Logf("Memory field: %s = %s", field.Name, parseMemorySize(value.Uint()))
		case reflect.Float64:
			t.Logf("Memory field: %s = %.2f%%", field.Name, value.Float())
		default:
			t.Logf("Memory field: %s = %v", field.Name, value.Interface())
		}
	}

}

func parseMemorySize(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
