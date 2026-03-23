package backend

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const processingPhaseEvent = "processing:phase"

// emitProcessingPhase notifies the desktop UI about the current pipeline step.
// It is a no-op when ctx is not a Wails runtime context (e.g. in unit tests).
func emitProcessingPhase(ctx context.Context, phaseID string) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, processingPhaseEvent, phaseID)
}
