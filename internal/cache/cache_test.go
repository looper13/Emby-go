package cache

import (
	"testing"
	"time"
)

func TestMemoryLRUAndTTL(t *testing.T) {
	c := NewMemory(1)
	c.Set("a", []byte("one"), time.Hour)
	if got, ok := c.Get("a"); !ok || string(got) != "one" {
		t.Fatal("memory cache miss")
	}
	c.Set("b", []byte("two"), time.Hour)
	if _, ok := c.Get("a"); ok {
		t.Fatal("LRU did not evict")
	}
	c.Set("c", []byte("three"), time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.Get("c"); ok {
		t.Fatal("TTL did not expire")
	}
}

func TestRedisBehavior(t *testing.T) {
	c := NewRedis("127.0.0.1:6379", "", 15)
	if err := c.Ping(); err != nil {
		t.Skipf("local Redis unavailable: %v", err)
	}
	c.Clear()
	c.Set("test", []byte("value"), time.Minute)
	got, ok := c.Get("test")
	if !ok || string(got) != "value" {
		t.Fatal("redis cache read mismatch")
	}
	c.Delete("test")
	if _, ok := c.Get("test"); ok {
		t.Fatal("redis delete failed")
	}
	c.Set("clear-test", []byte("value"), time.Minute)
	c.Clear()
	if _, ok := c.Get("clear-test"); ok {
		t.Fatal("redis clear failed")
	}
}

// Redis 不可用时：访问要有明确时间/重试预算（不能把查询拖到秒级），
// 失败要计数（设置页显示真实状态），且一律按未命中继续而不是报错。
func TestRedisBudgetAndFailureVisibility(t *testing.T) {
	c := NewRedis("127.0.0.1:1", "", 0)
	options := c.client.Options()
	if options.MaxRetries != redisRetries || options.ReadTimeout != redisOperationTimeout ||
		options.WriteTimeout != redisOperationTimeout || !options.ContextTimeoutEnabled {
		t.Fatalf("Redis 超时/重试预算未收紧: %+v", options)
	}
	start := time.Now()
	if _, ok := c.Get("missing"); ok {
		t.Fatal("Redis 不可用时读取应算未命中")
	}
	if elapsed := time.Since(start); elapsed > redisDialTimeout+redisOperationTimeout {
		t.Fatalf("Redis 故障时 Get 耗时 %v，超出预算", elapsed)
	}
	if c.Errors() == 0 {
		t.Fatal("访问失败没有计数")
	}
	if online, failures := c.Health(); online || failures == 0 {
		t.Fatalf("健康检查 = online:%v failures:%d", online, failures)
	}
	// 写入/删除/清理路径同样不能阻塞或无限重试。
	c.Set("k", []byte("v"), time.Minute)
	c.Delete("k")
	c.Clear()
}
