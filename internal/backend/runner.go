package backend

import (
	"context"
)

func RunProcessing(ctx context.Context, cfg ProcessingConfig) (ProcessingResult, error) {
	return runProcessingNative(ctx, cfg)
}
