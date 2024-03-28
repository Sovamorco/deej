package session

import (
	"context"
	"fmt"

	"github.com/jfreymuth/pulse/proto"
	"github.com/rs/zerolog"
)

// normal PulseAudio volume (100%).
const maxVolume = 0x10000

type PASession struct {
	baseSession `exhaustruct:"optional"`

	processName string

	client   *proto.Client
	notifier *VolumeNotifier

	sinkInputIndex    uint32
	sinkInputChannels byte
}

func newPASession(
	logger *zerolog.Logger,
	client *proto.Client,
	notifier *VolumeNotifier,
	sinkInputIndex uint32,
	sinkInputChannels byte,
	processName string,
) *PASession {
	s := &PASession{
		processName:       processName,
		client:            client,
		notifier:          notifier,
		sinkInputIndex:    sinkInputIndex,
		sinkInputChannels: sinkInputChannels,
	}

	s.name = processName
	s.humanReadableDesc = processName

	// use a self-identifying session name e.g. deej.sessions.chrome
	s.logger = logger.With().Str("session", s.Key()).Logger()
	s.logger.Debug().Msg("Session created")

	return s
}

func (s *PASession) GetVolume() float32 {
	request := proto.GetSinkInputInfo{
		SinkInputIndex: s.sinkInputIndex,
	}

	var reply proto.GetSinkInputInfoReply

	if err := s.client.Request(&request, &reply); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to get session volume")
	}

	level := parseChannelVolumes(reply.ChannelVolumes)

	return level
}

func (s *PASession) SetVolume(ctx context.Context, v float32) error {
	volumes := createChannelVolumes(s.sinkInputChannels, v)
	request := proto.SetSinkInputVolume{
		SinkInputIndex: s.sinkInputIndex,
		ChannelVolumes: volumes,
	}

	if err := s.client.Request(&request, nil); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to set session volume")

		return fmt.Errorf("adjust session volume: %w", err)
	}

	go s.notifier.Notify(ctx, s.processName, v)

	s.logger.Debug().Float32("volume", v).Msg("Adjusting session volume")

	return nil
}

func (s *PASession) Release() {
	s.logger.Debug().Msg("Releasing audio session")
}

func createChannelVolumes(channels byte, volume float32) []uint32 {
	volumes := make([]uint32, channels)

	for i := range volumes {
		volumes[i] = uint32(volume * maxVolume)
	}

	return volumes
}

func parseChannelVolumes(volumes []uint32) float32 {
	var level uint32

	for _, volume := range volumes {
		level += volume
	}

	return float32(level) / float32(len(volumes)) / float32(maxVolume)
}
