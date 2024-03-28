package session

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jfreymuth/pulse/proto"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

const (
	paClientTimeout = 30 * time.Second
)

type PASessionFinder struct {
	notifier *VolumeNotifier

	client *proto.Client
	conn   net.Conn

	Updates chan struct{}
}

func NewSessionFinder(ctx context.Context, notifier *VolumeNotifier) (*PASessionFinder, error) {
	logger := zerolog.Ctx(ctx)

	client, conn, err := proto.Connect("")
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to establish PulseAudio connection")

		return nil, errorx.Decorate(err, "establish pulseaudio connection")
	}

	client.SetTimeout(paClientTimeout)

	request := proto.SetClientName{
		Props: proto.PropList{
			"application.name": proto.PropListString("deej"),
		},
	}

	var reply proto.SetClientNameReply

	if err := client.Request(&request, &reply); err != nil {
		return nil, fmt.Errorf("set client name: %w", err)
	}

	sf := &PASessionFinder{
		notifier: notifier,
		client:   client,
		conn:     conn,
		Updates:  make(chan struct{}),
	}

	client.Callback = sf.paUpdateCallbackCtx(ctx)

	err = client.Request(&proto.Subscribe{Mask: proto.SubscriptionMaskAll}, nil)
	if err != nil {
		panic(err)
	}

	logger.Debug().Msg("Created PA session finder instance")

	return sf, nil
}

func (sf *PASessionFinder) paUpdateCallbackCtx(ctx context.Context) func(ival any) {
	logger := zerolog.Ctx(ctx)

	return func(ival any) {
		val, ok := ival.(*proto.SubscribeEvent)

		if !ok {
			return
		}

		if val.Event.GetFacility() != proto.EventSinkSinkInput {
			return
		}

		//nolint: exhaustive // we are not exhaustive here.
		switch val.Event.GetType() {
		case proto.EventNew:
			logger.Info().Msg("Received new sink event")
		case proto.EventRemove:
			logger.Info().Msg("Received remove sink event")
		default:
			return
		}

		sf.Updates <- struct{}{}
	}
}

func (sf *PASessionFinder) GetAllSessions(ctx context.Context) ([]Session, error) {
	logger := zerolog.Ctx(ctx)

	sessions := []Session{}

	// get the master sink session
	masterSink, err := sf.getMasterSinkSession(ctx)
	if err == nil {
		sessions = append(sessions, masterSink)
	} else {
		logger.Warn().Err(err).Msg("Failed to get master audio sink session")
	}

	// get the master source session
	masterSource, err := sf.getMasterSourceSession(ctx)
	if err == nil {
		sessions = append(sessions, masterSource)
	} else {
		logger.Warn().Err(err).Msg("Failed to get master audio source session")
	}

	// enumerate sink inputs and add sessions along the way
	if err := sf.enumerateAndAddSessions(ctx, &sessions); err != nil {
		logger.Warn().Err(err).Msg("Failed to enumerate audio sessions")

		return nil, fmt.Errorf("enumerate audio sessions: %w", err)
	}

	return sessions, nil
}

func (sf *PASessionFinder) Release(ctx context.Context) error {
	logger := zerolog.Ctx(ctx)

	if err := sf.conn.Close(); err != nil {
		logger.Warn().Err(err).Msg("Failed to close PulseAudio connection")

		return fmt.Errorf("close PulseAudio connection: %w", err)
	}

	logger.Debug().Msg("Released PA session finder instance")

	return nil
}

func (sf *PASessionFinder) getMasterSinkSession(ctx context.Context) (*masterSession, error) {
	logger := zerolog.Ctx(ctx)

	//nolint:exhaustruct
	request := proto.GetSinkInfo{
		SinkIndex: proto.Undefined,
	}

	var reply proto.GetSinkInfoReply

	if err := sf.client.Request(&request, &reply); err != nil {
		logger.Warn().Err(err).Msg("Failed to get master sink info")

		return nil, fmt.Errorf("get master sink info: %w", err)
	}

	// create the master sink session
	sink := newMasterSession(logger, sf.client, reply.SinkIndex, reply.Channels, true)

	return sink, nil
}

func (sf *PASessionFinder) getMasterSourceSession(ctx context.Context) (*masterSession, error) {
	logger := zerolog.Ctx(ctx)

	//nolint:exhaustruct
	request := proto.GetSourceInfo{
		SourceIndex: proto.Undefined,
	}

	var reply proto.GetSourceInfoReply

	if err := sf.client.Request(&request, &reply); err != nil {
		logger.Warn().Err(err).Msg("Failed to get master source info")

		return nil, fmt.Errorf("get master source info: %w", err)
	}

	// create the master source session
	source := newMasterSession(logger, sf.client, reply.SourceIndex, reply.Channels, false)

	return source, nil
}

func (sf *PASessionFinder) enumerateAndAddSessions(ctx context.Context, sessions *[]Session) error {
	logger := zerolog.Ctx(ctx)

	request := proto.GetSinkInputInfoList{}
	reply := proto.GetSinkInputInfoListReply{}

	if err := sf.client.Request(&request, &reply); err != nil {
		logger.Warn().Err(err).Msg("Failed to get sink input list")

		return fmt.Errorf("get sink input list: %w", err)
	}

	for _, info := range reply {
		name, ok := info.Properties["application.process.binary"]

		if !ok {
			logger.Warn().Int("sinkInputIndex", int(info.SinkInputIndex)).Msg("Failed to get sink input's process name")

			continue
		}

		// create the deej session object
		newSession := newPASession(
			logger,
			sf.client, sf.notifier,
			info.SinkInputIndex, info.Channels, name.String(),
		)

		// add it to our slice
		*sessions = append(*sessions, newSession)
	}

	return nil
}
