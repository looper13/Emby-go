package cache

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "emby-go:cache:"

// Redis 访问预算。缓存是加速层：它慢或挂掉时，请求必须很快退化成
// 「未命中 → 重新计算」，不能把正常查询拖到秒级。go-redis 默认单次读 3 秒、
// 最多 3 次重试，一次 Get 最坏能拖十几秒，所以这里把超时和重试都收紧
// （本地/内网 Redis 正常亚毫秒级返回）。
const (
	redisDialTimeout      = 2 * time.Second        // 建连（含握手），启动自检也用它
	redisOperationTimeout = 800 * time.Millisecond // 单次命令含重试的总预算
	redisRetries          = 1                      // 只重试一次，不叠加长退避
	redisLogInterval      = 30 * time.Second       // 故障日志限流间隔
)

type Redis struct {
	client     *redis.Client
	ctx        context.Context
	errors     atomic.Uint64
	lastLogged atomic.Int64 // 上次故障日志时间（UnixNano）
}

func NewRedis(addr, password string, db int) *Redis {
	return &Redis{
		client: redis.NewClient(&redis.Options{
			Addr: addr, Password: password, DB: db,
			DialTimeout:     redisDialTimeout,
			ReadTimeout:     redisOperationTimeout,
			WriteTimeout:    redisOperationTimeout,
			PoolTimeout:     redisOperationTimeout,
			MaxRetries:      redisRetries,
			MinRetryBackoff: 20 * time.Millisecond,
			MaxRetryBackoff: 100 * time.Millisecond,
			// 让超时预算由传进来的 context 统一约束（含重试之间的等待）；
			// 否则重试会在 ctx 之外继续占用时间，故障时越重试越慢。
			ContextTimeoutEnabled: true,
		}),
		ctx: context.Background(),
	}
}

// opContext 单次命令的时间预算。
func (r *Redis) opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.ctx, redisOperationTimeout)
}

// note 记录一次访问故障：累计计数 + 限流告警。
//
// 缓存故障不影响正确性（调用方按未命中处理），但也不能静默：
// 「Redis 挂了，所有请求都在打数据库」必须能从日志和设置页看出来。
func (r *Redis) note(op string, err error) {
	if err == nil || errors.Is(err, redis.Nil) {
		return
	}
	total := r.errors.Add(1)
	now := time.Now().UnixNano()
	last := r.lastLogged.Load()
	if now-last < int64(redisLogInterval) || !r.lastLogged.CompareAndSwap(last, now) {
		return
	}
	slog.Warn("Redis 缓存访问失败，已按未命中处理", "op", op, "error", err, "failures", total)
}

func (r *Redis) key(key string) string { return keyPrefix + key }

// Errors 累计访问故障次数（诊断用）。
func (r *Redis) Errors() uint64 { return r.errors.Load() }

// Health 探活并返回累计故障次数，供管理端设置页显示真实状态。
func (r *Redis) Health() (bool, uint64) {
	ctx, cancel := r.opContext()
	defer cancel()
	err := r.client.Ping(ctx).Err()
	r.note("ping", err)
	return err == nil, r.errors.Load()
}

func (r *Redis) Get(key string) ([]byte, bool) {
	ctx, cancel := r.opContext()
	defer cancel()
	value, err := r.client.Get(ctx, r.key(key)).Bytes()
	r.note("get", err)
	return value, err == nil
}

func (r *Redis) Set(key string, value []byte, ttl time.Duration) {
	ctx, cancel := r.opContext()
	defer cancel()
	r.note("set", r.client.Set(ctx, r.key(key), value, ttl).Err())
}

func (r *Redis) Delete(key string) {
	ctx, cancel := r.opContext()
	defer cancel()
	r.note("delete", r.client.Del(ctx, r.key(key)).Err())
}

// Clear 按前缀删除缓存键。每批用独立预算：整表清理可能很多轮，
// 不能靠一个 context 卡住整个流程，也不能因为一批失败就永远转下去。
func (r *Redis) Clear() {
	var cursor uint64
	for {
		ctx, cancel := r.opContext()
		keys, next, err := r.client.Scan(ctx, cursor, keyPrefix+"*", 100).Result()
		if err != nil {
			cancel()
			r.note("scan", err)
			return
		}
		if len(keys) > 0 {
			r.note("clear", r.client.Del(ctx, keys...).Err())
		}
		cancel()
		if next == 0 {
			return
		}
		cursor = next
	}
}

// Ping 启动自检：连接失败会拒绝启动，因此给建连留出完整预算。
func (r *Redis) Ping() error {
	ctx, cancel := context.WithTimeout(r.ctx, redisDialTimeout)
	defer cancel()
	return r.client.Ping(ctx).Err()
}
