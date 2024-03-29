package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/joomcode/errorx"
	"github.com/omriharel/deej/config"
	"github.com/omriharel/deej/serial"
	"github.com/omriharel/deej/session"
	"github.com/omriharel/deej/sliders"
	"github.com/omriharel/deej/tray"
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
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	//nolint:reassign // that's the way of zerolog.
	zerolog.ErrorStackMarshaler = marshalErrorxStack

	logger := zerolog.New(zerolog.NewConsoleWriter()).
		With().Caller().Timestamp().Stack().
		Logger().Level(zerolog.DebugLevel)

	ctx = logger.WithContext(ctx)

	defer cancel()

	tray.InitializeTray(ctx, cancel, start)
}

func start(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	logger := zerolog.Ctx(ctx)

	config, err := config.Load(ctx, "config.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load config")
	}

	sm, err := session.NewMonitor(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create session monitor")
	}

	slds := sliders.NewSliders(ctx, config.SliderMapping, sm)

	sp := serial.NewSerial(config.SerialPort, config.BaudRate)

	err = sp.Run(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open serial port")
	}

mainloop:
	for {
		select {
		case line := <-sp.Lines:
			logger.Trace().Bytes("line", line).Str("line", string(line)).Msg("Received serial line")

			slds.HandleLine(ctx, line)
		case err := <-sp.Errors:
			logger.Error().Err(err).Msg("Serial error")

			break mainloop
		case <-ctx.Done():
			break mainloop
		}
	}
}
