package enginebus

import "context"

type (
	// Tool represents a utility the engine can execute on behalf of the AI.
	Tool interface {
		Name() string
		Schema() map[string]any
		Execute(ctx context.Context, args map[string]any) (string, error)
	}

	// ToolGateway controls which tools are available per session.
	ToolGateway interface {
		Tools() []Tool
	}

	// ToolCallInfo carries info about a single AI tool invocation.
	ToolCallInfo struct {
		Name   string
		Args   map[string]any
		Result string
	}
)

// Gateway is a simple ToolGateway backed by a static list of tools.
type Gateway struct {
	tools []Tool
}

// NewGateway creates a Gateway with the given tools.
func NewGateway(tools ...Tool) *Gateway {
	return &Gateway{tools: tools}
}

// Tools returns the gateway's tool list.
func (g *Gateway) Tools() []Tool {
	return g.tools
}
