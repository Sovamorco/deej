package sliders

import (
	"bytes"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omriharel/deej/session"
	"github.com/rs/zerolog"
)

const (
	maxValue = 1023.0
	// noise margin for absolute value change.
	noiseMargin            = 0.002
	sessionVolumeInitDelay = 150 * time.Millisecond

	targetUnmapped = "deej.unmapped"
)

type Slider struct {
	sync.RWMutex `exhaustruct:"optional"`

	parent *Sliders

	value   float32
	targets []string
	sm      *session.Monitor
}

type Sliders struct {
	sync.RWMutex `exhaustruct:"optional"`

	sliders []*Slider
	sm      *session.Monitor

	unmappedProcesses []string
}

func NewSliders(ctx context.Context, mapping [][]string, sm *session.Monitor) *Sliders {
	logger := zerolog.Ctx(ctx)

	sliders := &Sliders{
		sliders: make([]*Slider, len(mapping)),
		sm:      sm,

		unmappedProcesses: make([]string, 0),
	}

	for i := range len(mapping) {
		sliders.sliders[i] = &Slider{
			parent: sliders,

			// set to -1 because it's an impossible value, so it will prompt a change on first read.
			value:   -1,
			targets: make([]string, 0),
			sm:      sm,
		}
	}

	sliders.FromConfig(ctx, mapping)

	sliders.sm.OnUpdate(sliders.refreshUnmapped)
	sliders.sm.OnUpdate(sliders.setVolumes)

	logger.Debug().Msg("Sliders initialized")

	return sliders
}

func (s *Sliders) HandleLine(ctx context.Context, line []byte) {
	logger := zerolog.Ctx(ctx)

	nvs := bytes.Split(line, []byte("|"))

	s.RLock()

	sls := len(s.sliders)

	s.RUnlock()

	if len(nvs) < sls {
		return
	}

	for i, nv := range nvs {
		if i > sls {
			break
		}

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

		if math.Abs(float64(nvf-slider.value)) < noiseMargin {
			slider.Unlock()

			continue
		}

		slider.value = nvf

		logger.Debug().
			Int("idx", i).
			Float32("value", slider.value).
			Strs("targets", slider.targets).
			Msg("Slider value changed")

		slider.Unlock()

		go slider.handleValueChange(ctx)
	}
}

func (s *Slider) handleValueChange(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	s.RLock()
	defer s.RUnlock()

	for _, target := range s.targets {
		s.sm.RLock()

		for _, node := range s.sm.Nodes[target] {
			err := node.SetVolume(ctx, s.value)
			if err != nil {
				logger.Error().Err(err).Str("binary", node.Binary).Msg("Failed to set volume")
			}
		}

		if target == targetUnmapped {
			s.parent.RLock()

			for _, process := range s.parent.unmappedProcesses {
				for _, node := range s.sm.Nodes[process] {
					err := node.SetVolume(ctx, s.value)
					if err != nil {
						logger.Error().Err(err).Str("binary", node.Binary).Msg("Failed to set volume")
					}
				}
			}

			s.parent.RUnlock()
		}

		s.sm.RUnlock()
	}
}

func (s *Sliders) FromConfig(ctx context.Context, userMapping [][]string) {
	s.RLock()

	for i, targets := range userMapping {
		if i >= len(s.sliders) {
			break
		}

		slider := s.sliders[i]

		slider.Lock()

		slider.targets = make([]string, 0, len(targets))

		for _, target := range targets {
			if target != "" {
				slider.targets = append(slider.targets, target)
			}
		}

		slider.Unlock()
	}

	s.RUnlock()

	s.refreshUnmapped(ctx)
}

func (s *Sliders) refreshUnmapped(_ context.Context) {
	s.RLock()

	s.sm.RLock()
	defer s.sm.RUnlock()

	unmapped := make([]string, 0)

	for process := range s.sm.Nodes {
		mapped := false

		for _, slider := range s.sliders {
			slider.RLock()

			if slices.Contains(slider.targets, process) {
				mapped = true

				slider.RUnlock()

				break
			}

			slider.RUnlock()
		}

		if !mapped {
			unmapped = append(unmapped, process)
		}
	}

	s.RUnlock()

	s.Lock()

	s.unmappedProcesses = unmapped

	s.Unlock()
}

func (s *Sliders) setVolumes(ctx context.Context) {
	s.RLock()
	defer s.RUnlock()

	for _, slider := range s.sliders {
		go slider.handleValueChange(ctx)
	}
}
