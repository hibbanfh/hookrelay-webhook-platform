package neonauth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

// Clock provides the current time.
type Clock interface{ Now() time.Time }

type cacheKey [sha256.Size]byte

type cacheEntry struct {
	user      entity.SessionUser
	expiresAt time.Time
}

// CachingVerifier remembers positive results for a short TTL so that a burst of dashboard
// requests costs one call to Neon. Failures are never cached. The TTL bounds how long a
// signed-out session keeps working (ADR 0011). Keys are SHA-256 digests, so the cache never
// holds a token.
type CachingVerifier struct {
	next       repository.SessionVerifier
	clock      Clock
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[cacheKey]cacheEntry
	flight  singleflight.Group
}

// NewCachingVerifier wraps next. A ttl of zero or less turns the cache off.
func NewCachingVerifier(next repository.SessionVerifier, clock Clock, ttl time.Duration, maxEntries int) *CachingVerifier {
	return &CachingVerifier{
		next:       next,
		clock:      clock,
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[cacheKey]cacheEntry),
	}
}

// Verify answers from the cache, or asks next once per token no matter how many callers wait.
func (c *CachingVerifier) Verify(ctx context.Context, token entity.SessionToken) (entity.SessionUser, error) {
	if c.ttl <= 0 {
		return c.next.Verify(ctx, token)
	}
	key := cacheKey(sha256.Sum256([]byte(token.Value())))
	if user, ok := c.lookup(key); ok {
		return user, nil
	}
	return c.verifyShared(ctx, key, token)
}

// verifyShared merges concurrent lookups of one token. The upstream call runs detached from the
// first caller's context, so that caller giving up does not fail the others; the HTTP client
// timeout still bounds it. Each caller waits only as long as its own context allows.
func (c *CachingVerifier) verifyShared(ctx context.Context, key cacheKey, token entity.SessionToken) (entity.SessionUser, error) {
	detached := context.WithoutCancel(ctx)
	done := c.flight.DoChan(string(key[:]), func() (any, error) {
		return c.fetchAndStore(detached, key, token)
	})
	select {
	case <-ctx.Done():
		return entity.SessionUser{}, fmt.Errorf("wait for session verification: %w: %w", domainerrors.ErrAuthUnavailable, ctx.Err())
	case res := <-done:
		return userFromFlight(res)
	}
}

func userFromFlight(res singleflight.Result) (entity.SessionUser, error) {
	if res.Err != nil {
		return entity.SessionUser{}, res.Err
	}
	user, ok := res.Val.(entity.SessionUser)
	if !ok {
		return entity.SessionUser{}, fmt.Errorf("unexpected verification result %T: %w", res.Val, domainerrors.ErrAuthUnavailable)
	}
	return user, nil
}

func (c *CachingVerifier) fetchAndStore(ctx context.Context, key cacheKey, token entity.SessionToken) (any, error) {
	user, err := c.next.Verify(ctx, token)
	if err != nil {
		return nil, err
	}
	c.store(key, user)
	return user, nil
}

// lookup returns a live entry and deletes an expired one.
func (c *CachingVerifier) lookup(key cacheKey) (entity.SessionUser, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return entity.SessionUser{}, false
	}
	if !c.clock.Now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return entity.SessionUser{}, false
	}
	return entry.user, true
}

// store keeps user until min(now+ttl, session expiry). When the cache is full it first sweeps
// expired entries and, if there is still no room, skips caching: the answer stays correct.
func (c *CachingVerifier) store(key cacheKey, user entity.SessionUser) {
	now := c.clock.Now()
	expiresAt := now.Add(c.ttl)
	if !user.ExpiresAt.IsZero() && user.ExpiresAt.Before(expiresAt) {
		expiresAt = user.ExpiresAt
	}
	if !expiresAt.After(now) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxEntries {
		c.sweepLocked(now)
	}
	if len(c.entries) >= c.maxEntries {
		return
	}
	c.entries[key] = cacheEntry{user: user, expiresAt: expiresAt}
}

func (c *CachingVerifier) sweepLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}
