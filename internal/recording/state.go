package recording

import "sync/atomic"

// State tracks whether request/response capture is enabled at runtime.
type State struct {
	enabled atomic.Bool
}

func NewState(initial bool) *State {
	s := &State{}
	s.enabled.Store(initial)
	return s
}

func (s *State) Enabled() bool {
	if s == nil {
		return true
	}
	return s.enabled.Load()
}

func (s *State) SetEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.enabled.Store(enabled)
}
