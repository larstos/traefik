package plugin

import (
	"fmt"
	goplugin "plugin"

	"github.com/containous/traefik/log"
	"github.com/containous/traefik/safe"
)

// Manager is in charge of instantiating plugins and storing those in memory
type Manager struct {
	middlewares map[string][]Middleware
}

// NewManager builds a new manager
func NewManager() *Manager {
	return &Manager{
		middlewares: make(map[string][]Middleware),
	}
}

// Load loads a plugin
func (m *Manager) Load(plugin Plugin) error {
	errChan := make(chan error)
	defer close(errChan)

	safe.GoWithRecover(func() {
		instance := (interface{})(nil)
		switch plugin.Type {
		case PluginGo:
			instance = m.loadGoPlugin(plugin, errChan)
		case PluginGrpc:
			instance = m.loadRemotePlugin(plugin, errChan)
		default:
			errChan <- fmt.Errorf("unknown plugin type: %s", plugin.Type)
			return
		}

		if instance == nil {
			errChan <- fmt.Errorf("plugin from %+v can not be loaded", plugin)
			return
		}
		if middleware, ok := instance.(Middleware); ok {
			// if RemotePluginMiddleware, add to middleware list
			if _, ok := m.middlewares[middleware.Name()]; ok {
				m.middlewares[middleware.Name()] = append(m.middlewares[middleware.Name()], middleware)
			} else {
				m.middlewares[middleware.Name()] = []Middleware{middleware}
			}
		} else {
			errChan <- fmt.Errorf("plugin from %+v does not implement any plugin interface", plugin)
			return
		}
		errChan <- nil
		return
	}, func(err interface{}) {
		log.Errorf("Error in plugin Go routine: %s", err)
	})

	if err, ok := <-errChan; ok {
		return err
	}
	return nil
}

func (m *Manager) loadRemotePlugin(plugin Plugin, errChan chan error) Middleware {
	log.Debugf("Loading Remote Plugin from %s", plugin.Path)
	p, err := NewRemotePluginMiddleware(plugin)
	if err != nil {
		errChan <- fmt.Errorf("error in plugin loading: %s", err)
		return nil
	}
	return p
}

func (m *Manager) loadGoPlugin(plugin Plugin, errChan chan error) interface{} {
	p, err := goplugin.Open(plugin.Path)
	if err != nil {
		errChan <- fmt.Errorf("error opening plugin: %s", err)
		return nil
	}
	loader, err := p.Lookup("Load")
	if err != nil {
		errChan <- fmt.Errorf("error in plugin Lookup: %s", err)
		return nil
	}
	load, ok := loader.(func() interface{})
	if !ok {
		errChan <- fmt.Errorf("plugin from %+v does not implement Load() interface{} function", plugin)
		return nil
	}
	return load()
}

// GetMiddlewares return a list of all Middleware plugins
func (m *Manager) GetMiddlewares(entryname string) []Middleware {
	if m != nil && m.middlewares != nil {
		return m.middlewares[entryname]
	}
	return []Middleware{}
}

// Stop method shuts down all the plugin middlewares
func (m *Manager) Stop() {
	if m != nil {
		for _, list := range m.middlewares {
			for _, plug := range list {
				plug.Stop()
			}
		}
	}
}
