package backend

import (
	"context"
	"math"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const processingPhaseEvent = "processing:phase"
const processingProgressEvent = "processing:progress"

// emitProcessingPhase notifies the desktop UI about the current pipeline step.
// It is a no-op when ctx is not a Wails runtime context (e.g. in unit tests).
func emitProcessingPhase(ctx context.Context, phaseID string) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, processingPhaseEvent, phaseID)
}

// emitProcessingProgress sends a 0–100 value for the given pipeline phase during long work.
func emitProcessingProgress(ctx context.Context, phaseID string, percent float64) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		return
	}
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}
	wailsruntime.EventsEmit(ctx, processingProgressEvent, phaseID, percent)
}

// maybeEmitRowProgress reports coarse progress while iterating rows (avoids flooding the UI).
func maybeEmitRowProgress(ctx context.Context, phaseID string, index, total int) {
	if total <= 0 {
		return
	}
	step := total / 25
	if step < 1 {
		step = 1
	}
	first := index == 0
	last := index == total-1
	if !first && !last && index%step != 0 {
		return
	}
	var pct float64
	if total == 1 {
		pct = 100
	} else {
		pct = (float64(index) + 1) / float64(total) * 100
	}
	if last {
		pct = 100
	}
	emitProcessingProgress(ctx, phaseID, pct)
}

// maybeEmitProgressInRange maps row index to a percent sub-range within [minPct, maxPct].
func maybeEmitProgressInRange(ctx context.Context, phaseID string, index, total int, minPct, maxPct float64) {
	if total <= 0 || maxPct < minPct {
		return
	}
	step := total / 25
	if step < 1 {
		step = 1
	}
	first := index == 0
	last := index == total-1
	if !first && !last && index%step != 0 {
		return
	}
	span := maxPct - minPct
	var pct float64
	if total == 1 {
		pct = maxPct
	} else {
		pct = minPct + (float64(index)+1)/float64(total)*span
	}
	if last {
		pct = maxPct
	}
	emitProcessingProgress(ctx, phaseID, pct)
}
