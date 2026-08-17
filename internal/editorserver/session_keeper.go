package editorserver

import (
	"context"
	"errors"
	"sync"
	"time"

	"cvpp/internal/erp"
	"cvpp/internal/progress"
)

type sessionKeeper struct {
	mu       sync.Mutex
	client   *erp.Client
	interval time.Duration
}

func newSessionKeeper(interval time.Duration) *sessionKeeper {
	return &sessionKeeper{interval: interval}
}

func (keeper *sessionKeeper) SetClient(client *erp.Client) {
	if client == nil {
		return
	}
	keeper.mu.Lock()
	keeper.client = client
	keeper.mu.Unlock()
}

func (keeper *sessionKeeper) current() *erp.Client {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	return keeper.client
}

func (keeper *sessionKeeper) clear(client *erp.Client) {
	keeper.mu.Lock()
	if keeper.client == client {
		keeper.client = nil
	}
	keeper.mu.Unlock()
}

func (keeper *sessionKeeper) Run(ctx context.Context, busy func() bool, restore func() (*erp.Client, error)) {
	if client, err := restore(); err == nil {
		keeper.SetClient(client)
		progress.Logf("ERP session: restored for 10-minute keep-alive")
	}
	ticker := time.NewTicker(keeper.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if busy != nil && busy() {
				continue
			}
			client := keeper.current()
			if client == nil {
				continue
			}
			requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			err := client.KeepAlive(requestCtx)
			cancel()
			if err == nil {
				progress.Logf("ERP session: keep-alive succeeded")
				continue
			}
			if errors.Is(err, erp.ErrSessionRejected) {
				keeper.clear(client)
				progress.Logf("ERP session: keep-alive found an expired session; the next ERP action will request login")
				continue
			}
			progress.Logf("ERP session: keep-alive failed temporarily (%v)", err)
		}
	}
}
