package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/joomcode/errorx"
	"github.com/omriharel/deej/rewrite/config"
	"github.com/omriharel/deej/rewrite/serial"
	"github.com/omriharel/deej/rewrite/session"
	"github.com/omriharel/deej/rewrite/sliders"
	"github.com/rs/zerolog"
)

//nolint:ireturn // that signature is required.
func marshalErrorxStack(ierr error) any {
	err := errorx.Cast(ierr)
	if err == nil {
		return nil
	}

	return err.MarshalStackTrace()
}

func main() {
	//nolint:reassign // that's the way of zerolog.
	zerolog.ErrorStackMarshaler = marshalErrorxStack

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Caller().Timestamp().Stack().Logger().Level(zerolog.DebugLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ctx = logger.WithContext(ctx)

	config, err := config.Load(ctx, "config.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load config")
	}

	notifier := session.NewVolumeNotifier(logger)

	sf, err := session.NewSessionFinder(logger, notifier)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create session finder")
	}

	slds, err := sliders.NewSliders(ctx, config.SliderMapping, sf)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create sliders")
	}

	fmt.Println(config.BaudRate)

	sp := serial.NewSerial(config.SerialPort, config.BaudRate)

	err = sp.Run(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open serial port")
	}

mainloop:
	for {
		select {
		case line := <-sp.Lines:
			logger.Debug().Bytes("line", line).Str("line", string(line)).Msg("Received serial line")

			slds.HandleLine(ctx, line)
		case err := <-sp.Errors:
			logger.Error().Err(err).Msg("Serial error")

			break mainloop
		case <-ctx.Done():
			break mainloop
		}
	}
}
