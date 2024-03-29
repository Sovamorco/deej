package pipewire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strconv"

	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

const (
	pwTypeNode         = "PipeWire:Interface:Node"
	pwMediaClassOutput = "Stream/Output/Audio"

	pwTypePort = "PipeWire:Interface:Port"

	ActionAdd    = "add"
	ActionRemove = "remove"
)

type Action string

type Dump []struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info struct {
		Props struct {
			Name       string `json:"application.name"`
			Binary     string `json:"application.process.binary"`
			MediaClass string `json:"media.class"`
			NodeID     int    `json:"node.id"`
		} `json:"props"`
		Params struct {
			Props []struct {
				Volume float32 `json:"volume"`
			} `json:"props"`
		} `json:"params"`
	} `json:"info"`
}

type Port struct {
	ID int
}

type Event struct {
	Port   Port
	Action Action
}

type PWMonitor struct {
	cmd    *exec.Cmd
	events chan Event
}

func MonitorOutputs(ctx context.Context) (chan Event, error) {
	cmd := exec.CommandContext(ctx, "pw-link", "-Iom")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errorx.Decorate(err, "get stdout pipe")
	}

	pwm := PWMonitor{
		cmd:    cmd,
		events: make(chan Event),
	}

	err = cmd.Start()
	if err != nil {
		return nil, errorx.Decorate(err, "start command")
	}

	go pwm.parseMonitorOutputs(ctx, stdout)

	return pwm.events, nil
}

func (pwm *PWMonitor) parseMonitorOutputs(ctx context.Context, stream io.Reader) {
	logger := zerolog.Ctx(ctx)

	defer pwm.close(logger.With().Logger())

	bs := bufio.NewScanner(stream)

	for bs.Scan() {
		line := bs.Bytes()

		// we need first 6 bytes.
		if len(line) < 6 {
			continue
		}

		var action Action

		actionSymbol := line[0]
		switch actionSymbol {
		case '+', '=':
			action = ActionAdd
		case '-':
			action = ActionRemove
		default:
			continue
		}

		oidb := line[1:6]

		oid, err := strconv.Atoi(string(bytes.TrimSpace(oidb)))
		if err != nil {
			logger.Error().Err(err).Msg("parse object id")

			continue
		}

		pwm.events <- Event{
			Action: action,
			Port:   Port{ID: oid},
		}
	}
}

func GetPortNode(ctx context.Context, oid int) (*Node, error) {
	logger := zerolog.Ctx(ctx).With().Int("oid", oid).Logger()

	obj, err := getObjectInfo(ctx, oid)
	if err != nil {
		logger.Error().Err(err).Msg("get object info")
	}

	if len(obj) != 1 || obj[0].ID != oid || obj[0].Type != pwTypePort || obj[0].Info.Props.NodeID == 0 {
		return nil, errorx.IllegalState.New("invalid port object")
	}

	node, err := getObjectInfo(ctx, obj[0].Info.Props.NodeID)
	if err != nil {
		return nil, errorx.Decorate(err, "get node object")
	}

	if len(node) != 1 || node[0].Type != pwTypeNode {
		return nil, errorx.IllegalState.New("invalid node object")
	}

	if node[0].Info.Props.MediaClass != pwMediaClassOutput {
		return nil, errorx.IllegalState.New("invalid node media class")
	}

	name := node[0].Info.Props.Binary
	if len(name) == 0 {
		name = node[0].Info.Props.Name
	}

	return &Node{
		PortID: oid,
		ID:     node[0].ID,
		Binary: name,
	}, nil
}

func getObjectInfo(ctx context.Context, oid int) (Dump, error) {
	cmd := exec.CommandContext(ctx, "pw-dump", strconv.Itoa(oid))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errorx.Decorate(err, "get stdout pipe")
	}

	err = cmd.Start()
	if err != nil {
		return nil, errorx.Decorate(err, "start command")
	}

	b, err := io.ReadAll(stdout)
	if err != nil {
		return nil, errorx.Decorate(err, "read stdout")
	}

	err = cmd.Wait()
	if err != nil {
		return nil, errorx.Decorate(err, "wait for command")
	}

	var dump Dump

	err = json.Unmarshal(b, &dump)
	if err != nil {
		return nil, errorx.Decorate(err, "unmarshal data")
	}

	return dump, nil
}

func (pwm *PWMonitor) close(logger zerolog.Logger) {
	if pwm.cmd == nil {
		return
	}

	err := pwm.cmd.Wait()
	if err != nil {
		logger.Error().Err(err).Msg("wait for pw-dump command")
	}

	pwm.cmd = nil

	if pwm.events != nil {
		close(pwm.events)
	}

	pwm.events = nil
}
