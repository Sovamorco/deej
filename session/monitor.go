package session

import (
	"context"
	"sync"

	"github.com/joomcode/errorx"
	"github.com/omriharel/deej/pipewire"
	"github.com/rs/zerolog"
)

type Monitor struct {
	sync.RWMutex `exhaustruct:"optional"`

	events chan pipewire.Event
	Nodes  map[string]map[int]*pipewire.Node

	updateFuncs []func(context.Context)
}

func NewMonitor(ctx context.Context) (*Monitor, error) {
	events, err := pipewire.MonitorOutputs(ctx)
	if err != nil {
		return nil, errorx.Decorate(err, "monitor outputs")
	}

	m := &Monitor{
		events: events,
		Nodes:  make(map[string]map[int]*pipewire.Node),

		updateFuncs: make([]func(context.Context), 0),
	}

	go m.handleEvents(ctx)

	return m, nil
}

func (m *Monitor) OnUpdate(f func(context.Context)) {
	m.Lock()
	defer m.Unlock()

	m.updateFuncs = append(m.updateFuncs, f)
}

func (m *Monitor) handleEvents(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-m.events:
			logger.Debug().Str("action", string(event.Action)).Int("port", event.Port.ID).Msg("got event")

			switch event.Action {
			case pipewire.ActionRemove:
				m.Lock()

				deletePortNode(m.Nodes, event.Port)

				m.Unlock()
			case pipewire.ActionAdd:
				node, err := pipewire.GetPortNode(ctx, event.Port.ID)
				if err != nil {
					// this is not important, it just means that the update was not related to a port at all.
					logger.Debug().Err(err).Msg("failed to get port node")

					continue
				}

				m.Lock()

				if _, ok := m.Nodes[node.Binary]; !ok {
					m.Nodes[node.Binary] = make(map[int]*pipewire.Node, 1)
				}

				m.Nodes[node.Binary][node.ID] = node

				m.Unlock()
			}

			m.RLock()

			logger.Debug().Any("nodes", m.Nodes).Msg("current nodes")

			m.RUnlock()

			for _, f := range m.updateFuncs {
				f(ctx)
			}
		}
	}
}

func deletePortNode(nodes map[string]map[int]*pipewire.Node, port pipewire.Port) {
	for key, nodesm := range nodes {
		for nodeID, node := range nodesm {
			if node.PortID == port.ID {
				delete(nodes[node.Binary], nodeID)
			}
		}

		if len(nodesm) == 0 {
			delete(nodes, key)
		}
	}
}
