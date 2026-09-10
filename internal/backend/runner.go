package backend

import (
	"context"
)

func RunProcessing(ctx context.Context, cfg ProcessingConfig) (ProcessingResult, error) {
	if cfg.FrequencyModeEnabled {
		return runFrequencyProcessing(ctx, cfg)
	}
	return runProcessingNative(ctx, cfg)
}
