package streamctx

import "context"

// WithShutdown returns a child of requestCtx that is also canceled when
// shutdownCtx fires. Long-lived HTTP surfaces use this to drain promptly during
// daemon shutdown without canceling bounded REST handlers.
func WithShutdown(requestCtx, shutdownCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(requestCtx)
	if shutdownCtx == nil {
		return ctx, cancel
	}
	go func() {
		select {
		case <-shutdownCtx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
