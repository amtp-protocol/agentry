package workflow

import (
	"context"

	"github.com/amtp-protocol/agentry/internal/types"
)

// Manager interfaces for the Multi-Agent Coordination engine
type Manager interface {
	// Initialize takes an incoming message proposing a workflow,
	// extracts the CoordinationConfig, and persists initial states.
	Initialize(ctx context.Context, msg *types.Message) (*types.Workflow, error)

	// ProcessResponse handles an incoming response msg that is tied to an existing workflow,
	// updates the state machine, and potentially triggers downstream events (e.g., dispatching
	// to the next agent in a sequential sequence).
	ProcessResponse(ctx context.Context, workflowID string, replyMsg *types.Message) error

	// Start starts the background tasks like the timeout watcher.
	Start(ctx context.Context)

	// Stop gracefully kills the background tasks.
	Stop() error
}

// Dispatcher defines how the workflow engine actually sends out next-step messages.
// This allows resolving circular dependencies between workflow -> processing -> workflow.
type Dispatcher interface {
	Dispatch(ctx context.Context, msg *types.Message) error
}
