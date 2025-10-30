package manager

import (
	"context"
	"errors"
	"fmt"
	"sync"

	loggerpkg "autobot/internal/logger"
	"autobot/internal/trader"
)

// TraderManager 负责管理多个自动交易实例。
type TraderManager struct {
	mu      sync.RWMutex
	traders map[string]*trader.AutoTrader
}

// New 创建空的管理器。
func New() *TraderManager {
	return &TraderManager{traders: make(map[string]*trader.AutoTrader)}
}

// Register 添加交易实例。
func (m *TraderManager) Register(at *trader.AutoTrader) error {
	if at == nil {
		return errors.New("auto trader is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := at.Name()
	if _, exists := m.traders[name]; exists {
		return fmt.Errorf("trader %s already registered", name)
	}
	m.traders[name] = at
	loggerpkg.Get("manager").Printf("registered trader name=%s", name)
	return nil
}

// Run 启动所有交易实例并阻塞直至上下文取消。
func (m *TraderManager) Run(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.traders) == 0 {
		return errors.New("no traders registered")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(m.traders))
	loggerpkg.Get("manager").Printf("starting %d traders", len(m.traders))

	for name, at := range m.traders {
		wg.Add(1)
		go func(name string, at *trader.AutoTrader) {
			defer wg.Done()
			if err := at.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- fmt.Errorf("%s: %w", name, err)
			}
		}(name, at)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		loggerpkg.Get("manager").Printf("trader stopped err=%v", err)
		return err
	}
	loggerpkg.Get("manager").Printf("all traders stopped")
	return nil
}

// Names 返回已注册的交易实例名称。
func (m *TraderManager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.traders))
	for name := range m.traders {
		names = append(names, name)
	}
	return names
}
