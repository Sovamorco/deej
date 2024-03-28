package session

import (
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

type masterSession struct {
	baseSession `exhaustruct:"optional"`

	client *proto.Client

	streamIndex    uint32
	streamChannels byte
	isOutput       bool
}

func newPASession(
	logger zerolog.Logger,
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
	s.logger = logger.With().Str("name", s.Key()).Logger()
	s.logger.Debug().Str("session", s.name).Msg("Session created")

	return s
}

func newMasterSession(
	logger zerolog.Logger,
	client *proto.Client,
	streamIndex uint32,
	streamChannels byte,
	isOutput bool,
) *masterSession {
	s := &masterSession{
		client:         client,
		streamIndex:    streamIndex,
		streamChannels: streamChannels,
		isOutput:       isOutput,
	}

	var key string

	if isOutput {
		key = MasterSessionName
	} else {
		key = InputSessionName
	}

	s.logger = logger.With().Str("name", key).Logger()
	s.master = true
	s.name = key
	s.humanReadableDesc = key

	s.logger.Debug().Str("session", s.name).Msg("Session created")

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

func (s *PASession) SetVolume(v float32) error {
	volumes := createChannelVolumes(s.sinkInputChannels, v)
	request := proto.SetSinkInputVolume{
		SinkInputIndex: s.sinkInputIndex,
		ChannelVolumes: volumes,
	}

	if err := s.client.Request(&request, nil); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to set session volume")

		return fmt.Errorf("adjust session volume: %w", err)
	}

	go s.notifier.Notify(s.processName, v)

	s.logger.Debug().Float32("volume", v).Msg("Adjusting session volume")

	return nil
}

func (s *PASession) Release() {
	s.logger.Debug().Msg("Releasing audio session")
}

func (s *masterSession) GetVolume() float32 {
	var level float32

	if s.isOutput {
		//nolint:exhaustruct
		request := proto.GetSinkInfo{
			SinkIndex: s.streamIndex,
		}

		var reply proto.GetSinkInfoReply

		if err := s.client.Request(&request, &reply); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get session volume")

			return 0
		}

		level = parseChannelVolumes(reply.ChannelVolumes)
	} else {
		//nolint:exhaustruct
		request := proto.GetSourceInfo{
			SourceIndex: s.streamIndex,
		}

		var reply proto.GetSourceInfoReply

		if err := s.client.Request(&request, &reply); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get session volume")

			return 0
		}

		level = parseChannelVolumes(reply.ChannelVolumes)
	}

	return level
}

func (s *masterSession) SetVolume(v float32) error {
	var request proto.RequestArgs

	volumes := createChannelVolumes(s.streamChannels, v)

	if s.isOutput {
		//nolint:exhaustruct
		request = &proto.SetSinkVolume{
			SinkIndex:      s.streamIndex,
			ChannelVolumes: volumes,
		}
	} else {
		//nolint:exhaustruct
		request = &proto.SetSourceVolume{
			SourceIndex:    s.streamIndex,
			ChannelVolumes: volumes,
		}
	}

	if err := s.client.Request(request, nil); err != nil {
		s.logger.Warn().Err(err).Float32("volume", v).Msg("Failed to set session volume")

		return fmt.Errorf("adjust session volume: %w", err)
	}

	s.logger.Debug().Float32("volume", v).Msg("Adjusting session volume")

	return nil
}

func (s *masterSession) Release() {
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
