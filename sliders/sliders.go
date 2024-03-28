package sliders

import (
	"bytes"
	"context"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/joomcode/errorx"
	"github.com/omriharel/deej/session"
	"github.com/rs/zerolog"
	"golang.org/x/exp/maps"
)

const (
	maxValue = 1023.0
	// noise margin for absolute value change.
	noiseMargin = 0.01

	targetUnmapped = "deej.unmapped"
)

type Slider struct {
	sync.RWMutex `exhaustruct:"optional"`
	prevValue    float32
	value        float32
	sessions     map[string][]session.Session
}

type Sliders struct {
	sync.RWMutex `exhaustruct:"optional"`
	sliders      []*Slider
	sf           *session.PASessionFinder
}

func NewSliders(ctx context.Context, mapping [][]string, sf *session.PASessionFinder) (*Sliders, error) {
	logger := zerolog.Ctx(ctx)

	sliders := &Sliders{
		sliders: make([]*Slider, len(mapping)),
		sf:      sf,
	}

	for i := range len(mapping) {
		sliders.sliders[i] = &Slider{
			// set to -1 because it's an impossible value, so it will prompt a change on first read.
			prevValue: -1,
			value:     0,
			sessions:  make(map[string][]session.Session, 0),
		}
	}

	err := sliders.FromConfig(ctx, mapping)
	if err != nil {
		return nil, errorx.Decorate(err, "init from config")
	}

	go sliders.watchSessions(ctx)

	logger.Debug().Msg("Sliders initialized")

	return sliders, nil
}

func (s *Sliders) watchSessions(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.sf.Updates:
			err := s.updateSessions(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to update sessions")
			}
		}
	}
}

func (s *Sliders) HandleLine(ctx context.Context, line []byte) {
	logger := zerolog.Ctx(ctx)

	nvs := bytes.Split(line, []byte("|"))

	s.RLock()

	if len(nvs) < len(s.sliders) {
		return
	}

	s.RUnlock()

	for i, nv := range nvs {
		nvi, err := strconv.Atoi(strings.TrimSpace(string(nv)))
		if err != nil {
			return
		}

		if nvi > maxValue {
			return
		}

		s.RLock()

		slider := s.sliders[i]

		s.RUnlock()

		nvf := float32(nvi) / maxValue

		slider.Lock()

		if math.Abs(float64(nvf-slider.prevValue)) < noiseMargin {
			slider.Unlock()

			continue
		}

		slider.prevValue = slider.value
		slider.value = nvf

		slider.Unlock()

		logger.Debug().
			Int("idx", i).
			Float32("value", slider.value).
			Strs("targets", maps.Keys(slider.sessions)).
			Msg("Slider value changed")

		go slider.handleValueChange(ctx)
	}
}

func (s *Slider) handleValueChange(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	s.RLock()
	defer s.RUnlock()

	for _, sessions := range s.sessions {
		for _, sess := range sessions {
			err := sess.SetVolume(ctx, s.value)
			if err != nil {
				logger.Error().Err(err).Str("key", sess.Key()).Msg("Failed to set volume")
			}
		}
	}
}

func (s *Sliders) FromConfig(ctx context.Context, userMapping [][]string) error {
	s.RLock()

	for i, targets := range userMapping {
		if i >= len(s.sliders) {
			break
		}

		slider := s.sliders[i]

		slider.Lock()

		slider.sessions = make(map[string][]session.Session, len(targets))

		for _, target := range targets {
			if target != "" {
				slider.sessions[target] = make([]session.Session, 0)
			}
		}

		slider.Unlock()
	}

	s.RUnlock()

	err := s.updateSessions(ctx)
	if err != nil {
		return errorx.Decorate(err, "update sessions")
	}

	return nil
}

func (s *Sliders) updateSessions(ctx context.Context) error {
	allSessions, err := s.sf.GetAllSessions(ctx)
	if err != nil {
		return errorx.Decorate(err, "get all sessions")
	}

	s.Lock()
	defer s.Unlock()

	unmappedSessions := make([]session.Session, 0, len(allSessions))

	for _, sess := range allSessions {
		mapped := s.mapSession(sess)

		if !mapped {
			unmappedSessions = append(unmappedSessions, sess)
		}
	}

	for _, slider := range s.sliders {
		slider.Lock()

		for target := range slider.sessions {
			if target == targetUnmapped {
				slider.sessions[target] = unmappedSessions
			}
		}

		slider.Unlock()
	}

	for _, slider := range s.sliders {
		go slider.handleValueChange(ctx)
	}

	return nil
}

func (s *Sliders) mapSession(sess session.Session) bool {
	mapped := false

	for _, slider := range s.sliders {
		for target := range slider.sessions {
			if strings.EqualFold(sess.Key(), target) {
				slider.Lock()
				slider.sessions[target] = append(slider.sessions[target], sess)
				slider.Unlock()

				mapped = true
			}
		}
	}

	return mapped || isSystemSession(sess.Key())
}

func isSystemSession(name string) bool {
	return name == session.MasterSessionName ||
		name == session.SystemSessionName ||
		name == session.InputSessionName
}
