package session

import (
	"context"
	"strings"

	"github.com/rs/zerolog"
)

// Session represents a single addressable audio session.
type Session interface {
	GetVolume() float32
	SetVolume(ctx context.Context, v float32) error

	// TODO: future mute support
	// GetMute() bool
	// SetMute(m bool) error

	Key() string
	Release()
}

const (
	MasterSessionName = "master" // master device volume
	SystemSessionName = "system" // system sounds volume
	InputSessionName  = "mic"    // microphone input level
)

type baseSession struct {
	logger zerolog.Logger
	system bool
	master bool

	// used by Key(), needs to be set by child
	name string

	// used by String(), needs to be set by child
	humanReadableDesc string
}

func (s *baseSession) Key() string {
	if s.system {
		return SystemSessionName
	}

	if s.master {
		return strings.ToLower(s.name) // could be master or mic, or any device's friendly name
	}

	return strings.ToLower(s.name)
}
