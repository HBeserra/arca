package enginebus

import "context"

type (
	// Tool represents a utility that the engine can execute in behalf of the user/ai. It has a name, description, and an Execute method that takes arguments and returns results.
	Tool interface {
		Name() string
		Description() string
		Execute(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	}

	ToolProvider interface {
		GetTools() []Tool
	}
)
