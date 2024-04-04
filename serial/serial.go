package serial

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"github.com/tarm/serial"
)

const (
	readTimeout = 500 * time.Millisecond

	reconnectAttempts = 10
	reconnectDelay    = 1 * time.Second
)

type Serial struct {
	baudRate int
	port     string

	f io.ReadCloser

	Errors chan error
	Lines  chan []byte
}

func NewSerial(port string, baudRate int) *Serial {
	return &Serial{
		baudRate: baudRate,
		port:     port,

		f: nil,

		Errors: make(chan error),
		Lines:  make(chan []byte),
	}
}

func (s *Serial) Run(ctx context.Context) error {
	// do the first connection loudly to catch any errors.
	err := s.open()
	if err != nil {
		return errorx.Decorate(err, "to open serial port")
	}

	go s.run(ctx)

	return nil
}

func (s *Serial) open() error {
	//nolint:exhaustruct
	sp, err := serial.OpenPort(&serial.Config{
		Name:        s.port,
		Baud:        s.baudRate,
		ReadTimeout: readTimeout,
	})
	if err != nil {
		return errorx.Decorate(err, "failed to open serial port")
	}

	s.f = sp

	return nil
}

func (s *Serial) run(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	for {
		err := s.scanLines()
		if err != nil {
			s.Errors <- errorx.Decorate(err, "scan lines")
		}

		err = s.f.Close()
		if err != nil {
			logger.Error().Err(err).Msg("failed to close serial port")
		}

		reconnected := false

		// try to reconnect.
		for attempt := range reconnectAttempts {
			logger.Debug().Int("attempt", attempt).Msg("reconnecting to serial port")

			err = s.open()
			if err == nil {
				reconnected = true

				break
			}

			logger.Error().Err(err).Msg("failed to reopen serial port")

			time.Sleep(reconnectDelay)
		}

		if !reconnected {
			s.Errors <- errorx.IllegalState.New("failed to reconnect to serial port after %d attempts", reconnectAttempts)

			return
		}
	}
}

func (s *Serial) scanLines() error {
	scanner := bufio.NewScanner(s.f)

	for scanner.Scan() {
		s.Lines <- scanner.Bytes()
	}

	if err := scanner.Err(); err != nil {
		return errorx.Decorate(err, "read from serial port")
	}

	return nil
}
