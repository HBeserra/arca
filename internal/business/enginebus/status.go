package enginebus

import (
	"changeme/internal/business/enginebus/monitor"
	"context"
	"fmt"
)

func (e *Engine) Status(ctx context.Context) (*monitor.Status, error) {
	status, err := e.statusCache.GetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	status.LoadedModels = e.LoadedModels()

	return status, nil
}
