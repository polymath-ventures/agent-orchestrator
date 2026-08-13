package httpd

import "log/slog"

func normalizeAPIDeps(deps APIDeps, log *slog.Logger) APIDeps {
	if deps.DeviceLive == nil && deps.Presence != nil {
		deps.DeviceLive = deps.Presence
	}
	if deps.DeviceRoster != nil && deps.DeviceLive == nil {
		loggerOrDefault(log).Warn("mobile device roster configured without liveness source")
	}
	return deps
}
