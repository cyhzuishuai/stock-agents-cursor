package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const eodBusyLockKey = "eod:run:lock:busy"

var ErrLockHeld = errors.New("eod run lock held")

func AcquireEODLock(ctx context.Context, rdb redis.Cmdable, ttl time.Duration) (unlock func(), err error) {
	ok, err := rdb.SetNX(ctx, eodBusyLockKey, "1", ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockHeld
	}
	return func() {
		_ = rdb.Del(context.Background(), eodBusyLockKey).Err()
	}, nil
}
