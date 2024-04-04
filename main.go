package main

import (
	"context"
	"os"
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

	exitCode := tray.InitializeTray(ctx, cancel, start)

	os.Exit(exitCode)
}

func start(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	logger := zerolog.Ctx(ctx)

	config, err := config.Load(ctx, "config.yaml")
	if err != nil {
		return errorx.Decorate(err, "load config")
	}

	sm, err := session.NewMonitor(ctx)
	if err != nil {
		return errorx.Decorate(err, "create session monitor")
	}

	slds := sliders.NewSliders(ctx, config.SliderMapping, sm)

	sp := serial.NewSerial(config.SerialPort, config.BaudRate)

	err = sp.Run(ctx)
	if err != nil {
		return errorx.Decorate(err, "run serial connection")
	}

	for {
		select {
		case line := <-sp.Lines:
			logger.Trace().Bytes("line", line).Str("line", string(line)).Msg("Received serial line")

			slds.HandleLine(ctx, line)
		case err := <-sp.Errors:
			logger.Error().Err(err).Msg("Serial error")

			return err
		case <-ctx.Done():
			return nil
		}
	}
}
