package neonauth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/project/data/neonauth"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// manualClock is a clock the test moves by hand.
type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// upstream is a SessionVerifier that counts calls and replays a result.
type upstream struct {
	calls atomic.Int64
	user  entity.SessionUser
	err   error
	gate  chan struct{} // when set, Verify blocks until it is closed
}

func (u *upstream) Verify(context.Context, entity.SessionToken) (entity.SessionUser, error) {
	u.calls.Add(1)
	if u.gate != nil {
		<-u.gate
	}
	return u.user, u.err
}

func parse(t *testing.T, s string) entity.SessionToken {
	t.Helper()
	tok, err := entity.ParseSessionToken(s)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func newCache(up *upstream, clk *manualClock, ttl time.Duration, max int) *neonauth.CachingVerifier {
	return neonauth.NewCachingVerifier(up, clk, ttl, max)
}

func TestCachingVerifierServesPositiveResultsWithinTTL(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}}
	clk := &manualClock{now: t0}
	cache := newCache(up, clk, 5*time.Second, 10)
	tok := parse(t, "tok-a")

	_, _ = cache.Verify(context.Background(), tok)
	clk.Advance(4999 * time.Millisecond)
	user, err := cache.Verify(context.Background(), tok)

	if err != nil || user.ID != "u1" || up.calls.Load() != 1 {
		t.Fatalf("user %+v err %v upstream calls %d, want a cache hit", user, err, up.calls.Load())
	}
}

func TestCachingVerifierRefetchesAfterTTL(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}}
	clk := &manualClock{now: t0}
	cache := newCache(up, clk, 5*time.Second, 10)
	tok := parse(t, "tok-a")

	_, _ = cache.Verify(context.Background(), tok)
	clk.Advance(5 * time.Second)
	_, _ = cache.Verify(context.Background(), tok)

	if up.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 after the TTL", up.calls.Load())
	}
}

func TestCachingVerifierNeverOutlivesTheSessionExpiry(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1", ExpiresAt: t0.Add(2 * time.Second)}}
	clk := &manualClock{now: t0}
	cache := newCache(up, clk, 5*time.Second, 10)
	tok := parse(t, "tok-a")

	_, _ = cache.Verify(context.Background(), tok)
	clk.Advance(2 * time.Second)
	_, _ = cache.Verify(context.Background(), tok)

	if up.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2: expiresAt caps the entry", up.calls.Load())
	}
}

func TestCachingVerifierDoesNotCacheAlreadyExpiredSessions(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1", ExpiresAt: t0.Add(-time.Second)}}
	cache := newCache(up, &manualClock{now: t0}, 5*time.Second, 10)
	tok := parse(t, "tok-a")

	_, _ = cache.Verify(context.Background(), tok)
	_, _ = cache.Verify(context.Background(), tok)

	if up.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2", up.calls.Load())
	}
}

func TestCachingVerifierDoesNotCacheFailures(t *testing.T) {
	for _, failure := range []error{domainerrors.ErrUnauthorized, domainerrors.ErrAuthUnavailable} {
		t.Run(failure.Error(), func(t *testing.T) {
			up := &upstream{err: failure}
			cache := newCache(up, &manualClock{now: t0}, 5*time.Second, 10)
			tok := parse(t, "tok-a")

			_, err1 := cache.Verify(context.Background(), tok)
			_, err2 := cache.Verify(context.Background(), tok)

			if !errors.Is(err1, failure) || !errors.Is(err2, failure) || up.calls.Load() != 2 {
				t.Fatalf("errs %v %v calls %d, want the failure twice and 2 upstream calls", err1, err2, up.calls.Load())
			}
		})
	}
}

func TestCachingVerifierKeepsTokensApart(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}}
	cache := newCache(up, &manualClock{now: t0}, 5*time.Second, 10)

	_, _ = cache.Verify(context.Background(), parse(t, "tok-a"))
	_, _ = cache.Verify(context.Background(), parse(t, "tok-b"))

	if up.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want one per token", up.calls.Load())
	}
}

func TestCachingVerifierZeroTTLPassesThrough(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}}
	cache := newCache(up, &manualClock{now: t0}, 0, 10)
	tok := parse(t, "tok-a")

	_, _ = cache.Verify(context.Background(), tok)
	_, _ = cache.Verify(context.Background(), tok)

	if up.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 with the cache off", up.calls.Load())
	}
}

func TestCachingVerifierFullCacheSweepsExpiredThenSkipsCaching(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}}
	clk := &manualClock{now: t0}
	cache := newCache(up, clk, 5*time.Second, 2)
	ctx := context.Background()

	_, _ = cache.Verify(ctx, parse(t, "tok-a"))
	_, _ = cache.Verify(ctx, parse(t, "tok-b"))
	_, _ = cache.Verify(ctx, parse(t, "tok-c")) // full: answered but not cached
	_, _ = cache.Verify(ctx, parse(t, "tok-c"))
	afterFull := up.calls.Load()
	clk.Advance(6 * time.Second) // a and b expire and are swept to make room
	_, _ = cache.Verify(ctx, parse(t, "tok-c"))
	_, _ = cache.Verify(ctx, parse(t, "tok-c"))

	if afterFull != 4 || up.calls.Load() != 5 {
		t.Fatalf("calls after full = %d (want 4), total = %d (want 5)", afterFull, up.calls.Load())
	}
}

func TestCachingVerifierMergesConcurrentLookups(t *testing.T) {
	const callers = 20
	up := &upstream{user: entity.SessionUser{ID: "u1"}, gate: make(chan struct{})}
	cache := newCache(up, &manualClock{now: t0}, 5*time.Second, 10)
	tok := parse(t, "tok-a")
	var entered, wg sync.WaitGroup
	entered.Add(callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entered.Done()
			_, _ = cache.Verify(context.Background(), tok)
		}()
	}

	entered.Wait() // every goroutine has started; the gate keeps the first upstream call open
	for up.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let the rest queue behind the in-flight call
	close(up.gate)
	wg.Wait()

	if up.calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 for %d concurrent callers", up.calls.Load(), callers)
	}
}

func TestCachingVerifierLeaderCancellationDoesNotFailOtherCallers(t *testing.T) {
	up := &upstream{user: entity.SessionUser{ID: "u1"}, gate: make(chan struct{})}
	cache := newCache(up, &manualClock{now: t0}, 5*time.Second, 10)
	tok := parse(t, "tok-a")
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := cache.Verify(leaderCtx, tok)
		leaderErr <- err
	}()
	for up.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	type result struct {
		user entity.SessionUser
		err  error
	}
	second := make(chan result, 1)
	go func() {
		user, err := cache.Verify(context.Background(), tok)
		second <- result{user, err}
	}()
	time.Sleep(20 * time.Millisecond)

	cancelLeader()
	gotLeader := <-leaderErr
	close(up.gate)
	got := <-second

	if !errors.Is(gotLeader, domainerrors.ErrAuthUnavailable) || !errors.Is(gotLeader, context.Canceled) {
		t.Fatalf("leader err = %v, want ErrAuthUnavailable wrapping context.Canceled", gotLeader)
	}
	if got.err != nil || got.user.ID != "u1" || up.calls.Load() != 1 {
		t.Fatalf("second caller got %+v, %v after %d upstream calls", got.user, got.err, up.calls.Load())
	}
}
