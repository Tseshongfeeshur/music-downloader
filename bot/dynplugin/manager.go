package dynplugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

type Manager struct {
	mu        sync.RWMutex
	platforms map[string]*scriptPlatform
	logger    *logpkg.Logger
}

func NewManager(logger *logpkg.Logger) *Manager {
	return &Manager{
		platforms: make(map[string]*scriptPlatform),
		logger:    logger,
	}
}

func (m *Manager) Load(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	return m.reload(ctx, cfg, platformManager)
}

func (m *Manager) Reload(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	return m.reload(ctx, cfg, platformManager)
}

func (m *Manager) reload(ctx context.Context, cfg *config.Config, platformManager platform.Manager) error {
	if cfg == nil {
		return fmt.Errorf("config required")
	}
	pluginNames := cfg.PluginNames()
	if len(pluginNames) == 0 {
		return nil
	}
	loaded := make(map[string]struct{})

	for _, name := range pluginNames {
		if name == "" {
			continue
		}
		if !pluginEnabled(cfg, name) {
			continue
		}
		if _, ok := platformplugins.Get(name); ok {
			continue
		}
		plug, meta, err := loadScriptPlugin(ctx, name, cfg, m.logger)
		if err != nil {
			if m.logger != nil {
				m.logger.Warn("script plugin load failed", "plugin", name, "error", err)
			}
			continue
		}
		if meta == nil || len(meta.Platforms) == 0 {
			if m.logger != nil {
				m.logger.Warn("script plugin returned no platforms", "plugin", name)
			}
			continue
		}
		for _, info := range meta.Platforms {
			if info.Name == "" {
				continue
			}
			loaded[info.Name] = struct{}{}
			m.mu.Lock()
			if existing, ok := m.platforms[info.Name]; ok {
				existing.update(plug, info)
				m.mu.Unlock()
				continue
			}
			plat := newScriptPlatform(plug, info)
			m.platforms[info.Name] = plat
			m.mu.Unlock()
			if platformManager != nil {
				platformManager.Register(plat)
			}
			if m.logger != nil {
				m.logger.Info("script platform registered", "plugin", name, "platform", info.Name)
			}
		}
	}

	m.mu.RLock()
	for name, plat := range m.platforms {
		if _, ok := loaded[name]; !ok {
			plat.disable()
			if m.logger != nil {
				m.logger.Info("script platform disabled", "platform", name)
			}
		}
	}
	m.mu.RUnlock()

	return nil
}

func pluginEnabled(cfg *config.Config, name string) bool {
	pluginCfg, ok := cfg.GetPluginConfig(name)
	if !ok {
		return true
	}
	if _, hasKey := pluginCfg["enabled"]; hasKey {
		return cfg.GetPluginBool(name, "enabled")
	}
	return true
}
