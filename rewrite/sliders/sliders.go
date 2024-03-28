package sliders

import (
	"bytes"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/joomcode/errorx"
	"github.com/omriharel/deej/rewrite/session"
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
	sync.Mutex `exhaustruct:"optional"`
	prevValue  float32
	value      float32
	sessions   map[string][]session.Session
}

type Sliders struct {
	sync.RWMutex `exhaustruct:"optional"`
	sliders      []*Slider
	sf           *session.PASessionFinder
}

func NewSliders(ctx context.Context, mapping [][]string, sf *session.PASessionFinder) (*Sliders, error) {
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

	err := sliders.FromConfig(mapping)
	if err != nil {
		return nil, errorx.Decorate(err, "init from config")
	}

	go sliders.watchSessions(ctx)

	return sliders, nil
}

func (s *Sliders) watchSessions(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.sf.Updates:
			err := s.updateSessions()
			if err != nil {
				logger.Error().Err(err).Msg("Failed to update sessions")
			}
		}
	}
}

func (s *Sliders) HandleLine(ctx context.Context, line []byte) {
	logger := zerolog.Ctx(ctx)

	s.RLock()
	defer s.RUnlock()

	nvs := bytes.Split(line, []byte("|"))

	if len(nvs) < len(s.sliders) {
		return
	}

	for i, nv := range nvs {
		nvi, err := strconv.Atoi(strings.TrimSpace(string(nv)))
		if err != nil {
			return
		}

		if nvi > maxValue {
			return
		}

		slider := s.sliders[i]

		slider.Lock()

		nvf := float32(nvi) / maxValue

		if math.Abs(float64(nvf-slider.prevValue)) < noiseMargin {
			slider.Unlock()

			continue
		}

		slider.prevValue = slider.value
		slider.value = nvf

		logger.Debug().
			Int("idx", i).
			Float32("value", slider.value).
			Strs("targets", maps.Keys(slider.sessions)).
			Msg("Slider value changed")

		slider.Unlock()

		slider.handleValueChange(ctx)
	}
}

func (s *Slider) handleValueChange(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	s.Lock()
	defer s.Unlock()

	for _, sessions := range s.sessions {
		for _, sess := range sessions {
			err := sess.SetVolume(s.value)
			if err != nil {
				logger.Error().Err(err).Str("key", sess.Key()).Msg("Failed to set volume")
			}
		}
	}
}

func (s *Sliders) FromConfig(userMapping [][]string) error {
	s.Lock()
	defer s.Unlock()

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

	err := s.updateSessions()
	if err != nil {
		return errorx.Decorate(err, "update sessions")
	}

	return nil
}

func (s *Sliders) updateSessions() error {
	allSessions, err := s.sf.GetAllSessions()
	if err != nil {
		return errorx.Decorate(err, "get all sessions")
	}

	systemSessions := []string{
		session.MasterSessionName,
		session.SystemSessionName,
		session.InputSessionName,
	}

	unmappedSessions := make([]session.Session, 0, len(allSessions))

	for _, sess := range allSessions {
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

		if !mapped && (!slices.Contains(systemSessions, sess.Key())) {
			unmappedSessions = append(unmappedSessions, sess)
		}
	}

	for _, slider := range s.sliders {
		for target := range slider.sessions {
			if target == targetUnmapped {
				slider.Lock()
				slider.sessions[target] = unmappedSessions
				slider.Unlock()
			}
		}
	}

	return nil
}
