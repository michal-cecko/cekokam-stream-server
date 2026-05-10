package health

import (
	"sync/atomic"
	"time"
)

type State struct {
	lastSync          atomic.Int64
	lastChannelTickOK atomic.Int64
}

func New() *State { return &State{} }

func (s *State) RecordSync() {
	s.lastSync.Store(time.Now().UnixNano())
}

func (s *State) RecordChannelTick() {
	s.lastChannelTickOK.Store(time.Now().UnixNano())
}

func (s *State) Healthy(window time.Duration) bool {
	now := time.Now().UnixNano()
	cutoff := now - window.Nanoseconds()
	return s.lastSync.Load() >= cutoff && s.lastChannelTickOK.Load() >= cutoff
}
