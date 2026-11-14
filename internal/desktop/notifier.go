package desktop

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// notifierService starts the Wails notification service without letting it
// stop the app. macOS only grants notifications to a binary inside an .app
// bundle, so a dev build run straight from go run has none, and the service
// failing would otherwise end the app at launch.
type notifierService struct {
	a *App
}

func (s *notifierService) ServiceName() string {
	return "pelton-notifications"
}

func (s *notifierService) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	if s.a.notifier == nil {
		return nil
	}
	if err := s.a.notifier.ServiceStartup(ctx, opts); err != nil {
		s.a.log.Warn("notifications unavailable", "err", err)
		return nil
	}
	s.a.notifyReady.Store(true)
	return nil
}

func (s *notifierService) ServiceShutdown() error {
	if s.a.notifier == nil || !s.a.notifyReady.Load() {
		return nil
	}
	return s.a.notifier.ServiceShutdown()
}
