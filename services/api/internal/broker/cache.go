package broker

import (
	"context"
	"sync"
	"time"
)

type cachedClient struct {
	inner Client
	ttl   time.Duration

	mu            sync.Mutex
	account       *Account
	accountExpiry time.Time
	positions     []Position
	posExpiry     time.Time
	posOK         bool
}

// NewCachedClient wraps a Client and caches GetAccount / ListPositions for ttl.
// A successful SubmitOrder invalidates both caches.
func NewCachedClient(inner Client, ttl time.Duration) Client {
	return &cachedClient{inner: inner, ttl: ttl}
}

func (c *cachedClient) GetAccount(ctx context.Context) (Account, error) {
	c.mu.Lock()
	if c.account != nil && time.Now().Before(c.accountExpiry) {
		acct := *c.account
		c.mu.Unlock()
		return acct, nil
	}
	c.mu.Unlock()

	acct, err := c.inner.GetAccount(ctx)
	if err != nil {
		return Account{}, err
	}

	c.mu.Lock()
	c.account = &acct
	c.accountExpiry = time.Now().Add(c.ttl)
	c.mu.Unlock()
	return acct, nil
}

func (c *cachedClient) ListPositions(ctx context.Context) ([]Position, error) {
	c.mu.Lock()
	if c.posOK && time.Now().Before(c.posExpiry) {
		out := append([]Position(nil), c.positions...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	positions, err := c.inner.ListPositions(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.positions = append([]Position(nil), positions...)
	c.posExpiry = time.Now().Add(c.ttl)
	c.posOK = true
	c.mu.Unlock()
	return positions, nil
}

func (c *cachedClient) SubmitOrder(ctx context.Context, req OrderRequest) (Order, error) {
	order, err := c.inner.SubmitOrder(ctx, req)
	if err != nil {
		return Order{}, err
	}
	c.invalidate()
	return order, nil
}

func (c *cachedClient) GetOrder(ctx context.Context, brokerOrderID string) (Order, error) {
	return c.inner.GetOrder(ctx, brokerOrderID)
}

func (c *cachedClient) ListOrders(ctx context.Context, status string) ([]Order, error) {
	return c.inner.ListOrders(ctx, status)
}

func (c *cachedClient) invalidate() {
	c.mu.Lock()
	c.account = nil
	c.accountExpiry = time.Time{}
	c.positions = nil
	c.posExpiry = time.Time{}
	c.posOK = false
	c.mu.Unlock()
}
