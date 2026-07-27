package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrLockHeld = errors.New("eod run lock held")

func eodLockKey(tradeDate string) string {
	return fmt.Sprintf("eod:run:lock:%s", tradeDate)
}

func AcquireEODLock(ctx context.Context, rdb redis.Cmdable, tradeDate string, ttl time.Duration) (unlock func(), err error) {
	key := eodLockKey(tradeDate)
	ok, err := rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockHeld
	}
	return func() {
		_ = rdb.Del(context.Background(), key).Err()
	}, nil
}
