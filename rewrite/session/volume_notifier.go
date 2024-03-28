package session

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	notificationLifetime = 1 * time.Second
	notificationMaxFreq  = 100 * time.Millisecond
)

type ProcessNotifyData struct {
	sync.Mutex         `exhaustruct:"optional"`
	logger             zerolog.Logger
	Name               string
	LastNotificationID int `exhaustruct:"optional"`
	// those two are separate because created at is set before creation, and expires at is set after creation.
	// that matters if an error occurred.
	LastNotificationCreatedAt time.Time `exhaustruct:"optional"`
	LastNotificationExpiresAt time.Time `exhaustruct:"optional"`
}

type VolumeNotifier struct {
	sync.Mutex `exhaustruct:"optional"`
	logger     zerolog.Logger
	processmap map[string]*ProcessNotifyData
}

func NewVolumeNotifier(logger zerolog.Logger) *VolumeNotifier {
	vn := &VolumeNotifier{
		logger:     logger.With().Str("name", "volume_notifier").Logger(),
		processmap: make(map[string]*ProcessNotifyData),
	}

	return vn
}

func (vn *VolumeNotifier) Notify(name string, volume float32) {
	vn.Lock()

	process, ok := vn.processmap[name]
	if !ok {
		process = &ProcessNotifyData{
			logger: vn.logger.With().Str("name", name).Logger(),
			Name:   name,
		}
		vn.processmap[name] = process
	}

	vn.Unlock()

	err := process.Notify(volume)
	if err != nil {
		vn.logger.Warn().Err(err).Str("process", name).Msg("Failed to notify process")

		return
	}
}

func (p *ProcessNotifyData) Notify(v float32) error {
	p.Lock()
	defer p.Unlock()

	if p.LastNotificationCreatedAt.Add(notificationMaxFreq).After(time.Now()) {
		return nil
	}

	p.LastNotificationCreatedAt = time.Now()

	nsArgs := []string{
		"-u", "low",
		"-t", strconv.Itoa(int(notificationLifetime / time.Millisecond)),
		"-a", "deej",
		"-i", "deej",
		"-p",
		//nolint:gomnd // percentage from 0-1 float.
		fmt.Sprintf("volume for %s changed to %.d%%", p.Name, int(v*100)),
	}

	if p.LastNotificationID != 0 {
		nsArgs = append(nsArgs, "-r", strconv.Itoa(p.LastNotificationID))
	}

	notifIDB, err := p.runNotify(nsArgs)
	if err != nil {
		return fmt.Errorf("run notify: %w", err)
	}

	notifID, err := strconv.Atoi(string(notifIDB))
	if err != nil {
		return fmt.Errorf("parse notif id: %w", err)
	}

	lastNotifID := p.LastNotificationID

	p.LastNotificationID = notifID
	p.LastNotificationExpiresAt = time.Now().Add(notificationLifetime)

	if lastNotifID == 0 {
		go func() {
			for {
				<-time.After(time.Until(p.LastNotificationExpiresAt))

				if time.Now().After(p.LastNotificationExpiresAt) {
					p.logger.Debug().Int("id", p.LastNotificationID).Msg("Invalidating notif id")
					p.LastNotificationID = 0

					return
				}
			}
		}()
	}

	p.logger.Debug().Int("id", p.LastNotificationID).Msg("Notified")

	return nil
}

func (p *ProcessNotifyData) runNotify(args []string) ([]byte, error) {
	cmd := exec.Command("notify-send", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("get stderr pipe: %w", err)
	}

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("send notif: %w", err)
	}

	notifIDB, err := io.ReadAll(stdout)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to read notify-send stdout")
	}

	notifErr, err := io.ReadAll(stderr)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to read notify-send stderr")
	}

	err = cmd.Wait()
	if err != nil {
		p.logger.Warn().Str("output", string(notifErr)).Msg("Error output from notify-send")

		return nil, fmt.Errorf("wait for notif: %w", err)
	}

	if notifIDB == nil || len(notifIDB) < 1 {
		return []byte{'0'}, nil
	}

	lastcn := len(notifIDB) - 1
	if notifIDB[lastcn] == '\n' {
		notifIDB = notifIDB[:lastcn]
	}

	return notifIDB, nil
}
