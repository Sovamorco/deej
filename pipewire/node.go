package pipewire

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

type Node struct {
	PortID int
	ID     int
	Binary string
}

func (n *Node) SetVolume(ctx context.Context, v float32) error {
	logger := zerolog.Ctx(ctx)

	logger.Debug().Str("binary", n.Binary).Int("id", n.ID).Float32("volume", v).Msg("setting volume")

	vstring := fmt.Sprintf("'{ volume: %.6f }'", v)

	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("pw-cli s %d Props '%s'", n.ID, vstring))

	err := cmd.Run()
	if err != nil {
		return errorx.Decorate(err, "run command")
	}

	return nil
}
