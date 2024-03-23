package deej

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/jfreymuth/pulse/proto"
)

// normal PulseAudio volume (100%)
const maxVolume = 0x10000

type PASession struct {
	baseSession

	processName string

	client *proto.Client

	sinkInputIndex    uint32
	sinkInputChannels byte
	prevNotifID       int
	prevNotifIDExpires time.Time
}

type masterSession struct {
	baseSession

	client *proto.Client

	streamIndex    uint32
	streamChannels byte
	isOutput       bool
}

func newPASession(
	logger *zap.SugaredLogger,
	client *proto.Client,
	sinkInputIndex uint32,
	sinkInputChannels byte,
	processName string,
) *PASession {

	s := &PASession{
		client:            client,
		sinkInputIndex:    sinkInputIndex,
		sinkInputChannels: sinkInputChannels,
	}

	s.processName = processName
	s.name = processName
	s.humanReadableDesc = processName

	// use a self-identifying session name e.g. deej.sessions.chrome
	s.logger = logger.Named(s.Key())
	s.logger.Debugw(sessionCreationLogMessage, "session", s)

	return s
}

func newMasterSession(
	logger *zap.SugaredLogger,
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
		key = masterSessionName
	} else {
		key = inputSessionName
	}

	s.logger = logger.Named(key)
	s.master = true
	s.name = key
	s.humanReadableDesc = key

	s.logger.Debugw(sessionCreationLogMessage, "session", s)

	return s
}

func (s *PASession) GetVolume() float32 {
	request := proto.GetSinkInputInfo{
		SinkInputIndex: s.sinkInputIndex,
	}
	reply := proto.GetSinkInputInfoReply{}

	if err := s.client.Request(&request, &reply); err != nil {
		s.logger.Warnw("Failed to get session volume", "error", err)
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
		s.logger.Warnw("Failed to set session volume", "error", err)
		return fmt.Errorf("adjust session volume: %w", err)
	}

	go s.notify(v)

	s.logger.Debugw("Adjusting session volume", "to", fmt.Sprintf("%.2f", v))

	return nil
}

func (s *PASession) notify(v float32) {
	nsArgs := []string{
		"-u", "low",
		"-t", "1000",
		"-a", "deej",
		"-i", "deej",
		"-p",
		fmt.Sprintf("volume for %s changed to %.d%%", s.processName, int(v * 100)),
	}

	if s.prevNotifID != 0 {
		nsArgs = append(nsArgs, "-r", strconv.Itoa(s.prevNotifID))
	}
	cmd := exec.Command("notify-send", nsArgs...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.logger.Debugf("Failed to get stdout pipe: %+v", err)

		return
	}

	err = cmd.Start()
	if err != nil {
		s.logger.Debugf("Failed to send notif: %+v", err)

		return
	}

	notifIDB, err := io.ReadAll(stdout)
	if err != nil {
		s.logger.Debugf("Failed to read notif: %+v", err)
	}

	err = cmd.Wait()
	if err != nil {
		s.logger.Debugf("Failed to wait for notif: %+v", err)

		return
	}

	lastcn := len(notifIDB) - 1
	if notifIDB[lastcn] == '\n' {
		notifIDB = notifIDB[:lastcn]
	}

	notifID, err := strconv.Atoi(string(notifIDB))
	if err != nil {
		s.logger.Debugf("Failed to convert notifID: %+v", err)

		return
	}

	lastNotifID := s.prevNotifID

	s.prevNotifID = notifID
	s.prevNotifIDExpires = time.Now().Add(1 * time.Second)

	if lastNotifID == 0 {
		go func() {
			for {
				<-time.After(time.Until(s.prevNotifIDExpires))

				if time.Now().After(s.prevNotifIDExpires) {
					s.logger.Debug("Invalidating notif id: ", s.prevNotifID)
					s.prevNotifID = 0
					
					return
				}
			}
		}()
	}

	s.logger.Debug("Notif ID: ", s.prevNotifID)
}

func (s *PASession) Release() {
	s.logger.Debug("Releasing audio session")
}

func (s *PASession) String() string {
	return fmt.Sprintf(sessionStringFormat, s.humanReadableDesc, s.GetVolume())
}

func (s *masterSession) GetVolume() float32 {
	var level float32

	if s.isOutput {
		request := proto.GetSinkInfo{
			SinkIndex: s.streamIndex,
		}
		reply := proto.GetSinkInfoReply{}

		if err := s.client.Request(&request, &reply); err != nil {
			s.logger.Warnw("Failed to get session volume", "error", err)
			return 0
		}

		level = parseChannelVolumes(reply.ChannelVolumes)
	} else {
		request := proto.GetSourceInfo{
			SourceIndex: s.streamIndex,
		}
		reply := proto.GetSourceInfoReply{}

		if err := s.client.Request(&request, &reply); err != nil {
			s.logger.Warnw("Failed to get session volume", "error", err)
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
		request = &proto.SetSinkVolume{
			SinkIndex:      s.streamIndex,
			ChannelVolumes: volumes,
		}
	} else {
		request = &proto.SetSourceVolume{
			SourceIndex:    s.streamIndex,
			ChannelVolumes: volumes,
		}
	}

	if err := s.client.Request(request, nil); err != nil {
		s.logger.Warnw("Failed to set session volume",
			"error", err,
			"volume", v)

		return fmt.Errorf("adjust session volume: %w", err)
	}

	s.logger.Debugw("Adjusting session volume", "to", fmt.Sprintf("%.2f", v))

	return nil
}

func (s *masterSession) Release() {
	s.logger.Debug("Releasing audio session")
}

func (s *masterSession) String() string {
	return fmt.Sprintf(sessionStringFormat, s.humanReadableDesc, s.GetVolume())
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
