package tray

import (
	"context"

	"github.com/getlantern/systray"
	"github.com/rs/zerolog"

	"github.com/omriharel/deej/icon"
)

func InitializeTray(
	ctx context.Context, cancel context.CancelFunc,
	start func(context.Context, context.CancelFunc) error,
) int {
	exitCode := 0

	logger := zerolog.Ctx(ctx)

	onReady := func() {
		logger.Debug().Msg("Tray instance ready")

		systray.SetTemplateIcon(icon.DeejLogo, icon.DeejLogo)
		systray.SetTitle("deej")
		systray.SetTooltip("deej")

		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "Stop deej and quit")

		// wait on things to happen
		go func() {
			select {
			// quit
			case <-ctx.Done():
				logger.Info().Msg("Context cancelled, stopping")
			case <-quit.ClickedCh:
				logger.Info().Msg("Quit menu item clicked, stopping")

				cancel()
			}

			systray.Quit()
		}()

		err := start(ctx, cancel)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to run deej")

			exitCode = 1
		}
	}

	onExit := func() {
		logger.Debug().Msg("Tray exited")
	}

	// start the tray icon
	logger.Debug().Msg("Running in tray")
	systray.Run(onReady, onExit)

	return exitCode
}
