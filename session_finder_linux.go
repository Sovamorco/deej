package deej

import (
	"fmt"
	"net"
	"time"

	"github.com/jfreymuth/pulse/proto"
	"go.uber.org/zap"
)

type PASessionFinder struct {
	logger        *zap.SugaredLogger
	sessionLogger *zap.SugaredLogger
	notifier      *VolumeNotifier

	client *proto.Client
	conn   net.Conn

	Updates chan struct{}
}

func newSessionFinder(logger *zap.SugaredLogger, notifier *VolumeNotifier) (*PASessionFinder, error) {
	client, conn, err := proto.Connect("")
	if err != nil {
		logger.Warnw("Failed to establish PulseAudio connection", "error", err)

		return nil, fmt.Errorf("establish PulseAudio connection: %w", err)
	}
	
	client.SetTimeout(30 * time.Second)

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
		logger:        logger.Named("session_finder"),
		sessionLogger: logger.Named("sessions"),
		notifier:      notifier,
		client:        client,
		conn:          conn,
		Updates:       make(chan struct{}),
	}

	client.Callback = func(ival interface{}) {
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
			logger.Info("Received new sink event")
		case proto.EventRemove:
			logger.Info("Received remove sink event")
		default:
			return
		}

		sf.Updates <- struct{}{}
	}

	err = client.Request(&proto.Subscribe{Mask: proto.SubscriptionMaskAll}, nil)
	if err != nil {
		panic(err)
	}

	sf.logger.Debug("Created PA session finder instance")

	return sf, nil
}

func (sf *PASessionFinder) GetAllSessions() ([]Session, error) {
	sessions := []Session{}

	// get the master sink session
	masterSink, err := sf.getMasterSinkSession()
	if err == nil {
		sessions = append(sessions, masterSink)
	} else {
		sf.logger.Warnw("Failed to get master audio sink session", "error", err)
	}

	// get the master source session
	masterSource, err := sf.getMasterSourceSession()
	if err == nil {
		sessions = append(sessions, masterSource)
	} else {
		sf.logger.Warnw("Failed to get master audio source session", "error", err)
	}

	// enumerate sink inputs and add sessions along the way
	if err := sf.enumerateAndAddSessions(&sessions); err != nil {
		sf.logger.Warnw("Failed to enumerate audio sessions", "error", err)

		return nil, fmt.Errorf("enumerate audio sessions: %w", err)
	}

	return sessions, nil
}

func (sf *PASessionFinder) Release() error {
	if err := sf.conn.Close(); err != nil {
		sf.logger.Warnw("Failed to close PulseAudio connection", "error", err)

		return fmt.Errorf("close PulseAudio connection: %w", err)
	}

	sf.logger.Debug("Released PA session finder instance")

	return nil
}

func (sf *PASessionFinder) getMasterSinkSession() (*masterSession, error) {
	//nolint:exhaustruct
	request := proto.GetSinkInfo{
		SinkIndex: proto.Undefined,
	}

	var reply proto.GetSinkInfoReply

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get master sink info", "error", err)

		return nil, fmt.Errorf("get master sink info: %w", err)
	}

	// create the master sink session
	sink := newMasterSession(sf.sessionLogger, sf.client, reply.SinkIndex, reply.Channels, true)

	return sink, nil
}

func (sf *PASessionFinder) getMasterSourceSession() (*masterSession, error) {
	//nolint:exhaustruct
	request := proto.GetSourceInfo{
		SourceIndex: proto.Undefined,
	}

	var reply proto.GetSourceInfoReply

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get master source info", "error", err)

		return nil, fmt.Errorf("get master source info: %w", err)
	}

	// create the master source session
	source := newMasterSession(sf.sessionLogger, sf.client, reply.SourceIndex, reply.Channels, false)

	return source, nil
}

func (sf *PASessionFinder) enumerateAndAddSessions(sessions *[]Session) error {
	request := proto.GetSinkInputInfoList{}
	reply := proto.GetSinkInputInfoListReply{}

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get sink input list", "error", err)

		return fmt.Errorf("get sink input list: %w", err)
	}

	for _, info := range reply {
		name, ok := info.Properties["application.process.binary"]

		if !ok {
			sf.logger.Warnw("Failed to get sink input's process name",
				"sinkInputIndex", info.SinkInputIndex)

			continue
		}

		// create the deej session object
		newSession := newPASession(
			sf.sessionLogger,
			sf.client, sf.notifier,
			info.SinkInputIndex, info.Channels, name.String(),
		)

		// add it to our slice
		*sessions = append(*sessions, newSession)
	}

	return nil
}
