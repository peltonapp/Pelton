package sync

import "errors"

// ErrSoftPaused is returned after the current fetch batch finishes when
// Engine.PauseCheck asks the engine to hold before the next batch. The
// in-flight batch is not cancelled. Ids not yet started are returned by
// SoftPauseRemaining.
var ErrSoftPaused = errors.New("sync: soft-paused")

type softPaused struct {
	remaining []string
}

func (e *softPaused) Error() string { return ErrSoftPaused.Error() }

func (e *softPaused) Unwrap() error { return ErrSoftPaused }

func newSoftPaused(remaining []string) error {
	return &softPaused{remaining: append([]string(nil), remaining...)}
}

// SoftPauseRemaining returns the remote ids that were not started when err
// is ErrSoftPaused, in the same order the caller passed them. The second
// result is false for any other error.
func SoftPauseRemaining(err error) ([]string, bool) {
	var sp *softPaused
	if !errors.As(err, &sp) {
		return nil, false
	}
	return append([]string(nil), sp.remaining...), true
}

func (e *Engine) pauseRequested() bool {
	return e.PauseCheck != nil && e.PauseCheck()
}
