package redisadapter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/redis/go-redis/v9"
)

const (
	DefaultRedisTimeout        = 500 * time.Millisecond
	DefaultMaxRetries          = 2
	DefaultForecastSnapshotTTL = 15 * time.Minute
)

var ErrCircuitBreakerOpen = errors.New("redis circuit breaker is OPEN")

type ResilientRedisCacheAdapter struct {
	client           *redis.Client
	timeout          time.Duration
	maxRetries       int
	failureThreshold int
	openCooldown     time.Duration

	mu           sync.RWMutex
	failures     int
	state        string
	openedAt     time.Time
	fallbackData map[string]domain.RunRateForecastResult
}

// RedisConfig configures Upstash Redis / Valkey Serverless connectivity (TLS + RESP3).
type RedisConfig struct {
	Addr     string
	Password string
	UseTLS   bool
	Protocol int // 2 for RESP2, 3 for RESP3 (default)
}

func NewResilientRedisCacheAdapter(addr string) *ResilientRedisCacheAdapter {
	return NewUpstashRedisCacheAdapter(RedisConfig{
		Addr:     addr,
		Protocol: 3,
	})
}

// NewUpstashRedisCacheAdapter creates a cache adapter compatible with Upstash Redis / Valkey Serverless (TLS + RESP3).
func NewUpstashRedisCacheAdapter(cfg RedisConfig) *ResilientRedisCacheAdapter {
	var client *redis.Client
	proto := cfg.Protocol
	if proto != 2 && proto != 3 {
		proto = 3
	}
	if strings.HasPrefix(cfg.Addr, "rediss://") || strings.HasPrefix(cfg.Addr, "redis://") {
		if opts, err := redis.ParseURL(cfg.Addr); err == nil {
			opts.Protocol = proto
			opts.DialTimeout = DefaultRedisTimeout
			opts.ReadTimeout = DefaultRedisTimeout
			opts.WriteTimeout = DefaultRedisTimeout
			if strings.HasPrefix(cfg.Addr, "rediss://") && opts.TLSConfig == nil {
				opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			client = redis.NewClient(opts)
		}
	} else if cfg.Addr != "" {
		opts := &redis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			Protocol:     proto,
			DialTimeout:  DefaultRedisTimeout,
			ReadTimeout:  DefaultRedisTimeout,
			WriteTimeout: DefaultRedisTimeout,
			PoolSize:     32,
			MinIdleConns: 4,
		}
		if cfg.UseTLS {
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		client = redis.NewClient(opts)
	}
	return &ResilientRedisCacheAdapter{
		client:           client,
		timeout:          DefaultRedisTimeout,
		maxRetries:       DefaultMaxRetries,
		failureThreshold: 3,
		openCooldown:     15 * time.Second,
		state:            "CLOSED",
		fallbackData:     make(map[string]domain.RunRateForecastResult),
	}
}

// NewRedisCacheAdapterFromEnv creates an Upstash Redis/Valkey RESP3 adapter from environment variables.
func NewRedisCacheAdapterFromEnv(addr string) *ResilientRedisCacheAdapter {
	useTLS := strings.EqualFold(strings.TrimSpace(os.Getenv("REDIS_TLS")), "true") || strings.HasPrefix(addr, "rediss://")
	return NewUpstashRedisCacheAdapter(RedisConfig{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		UseTLS:   useTLS,
		Protocol: 3,
	})
}

func (a *ResilientRedisCacheAdapter) SaveForecastSnapshot(
	ctx context.Context,
	result domain.RunRateForecastResult,
) error {
	key := fmt.Sprintf("pfm:forecast:%s:%s", result.TenantID, result.ReferenceMonth)

	// Always store in ultra-fast local fallback memory first (< 10µs)
	a.mu.Lock()
	a.fallbackData[key] = result
	a.mu.Unlock()

	if a.client == nil || a.isCircuitOpen() {
		return nil
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= a.maxRetries; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, a.timeout)
		lastErr = a.client.Set(callCtx, key, payload, DefaultForecastSnapshotTTL).Err()
		cancel()
		if lastErr == nil {
			a.recordSuccess()
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}

	a.recordFailure()
	// Graceful fallback: local fallback memory already updated, so caller succeeds without error
	return nil
}

func (a *ResilientRedisCacheAdapter) GetForecastSnapshot(
	ctx context.Context,
	tenantID string,
	referenceMonth string,
) (*domain.RunRateForecastResult, error) {
	key := fmt.Sprintf("pfm:forecast:%s:%s", tenantID, referenceMonth)

	if a.client != nil && !a.isCircuitOpen() {
		callCtx, cancel := context.WithTimeout(ctx, a.timeout)
		raw, err := a.client.Get(callCtx, key).Bytes()
		cancel()
		if err == nil {
			a.recordSuccess()
			var res domain.RunRateForecastResult
			if jsonErr := json.Unmarshal(raw, &res); jsonErr == nil {
				return &res, nil
			}
		} else {
			a.recordFailure()
		}
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if val, ok := a.fallbackData[key]; ok {
		copyVal := val
		return &copyVal, nil
	}
	return nil, nil
}

func (a *ResilientRedisCacheAdapter) CircuitState() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.state == "OPEN" && time.Since(a.openedAt) >= a.openCooldown {
		return "HALF_OPEN"
	}
	return a.state
}

func (a *ResilientRedisCacheAdapter) isCircuitOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == "OPEN" {
		if time.Since(a.openedAt) >= a.openCooldown {
			a.state = "HALF_OPEN"
			return false
		}
		return true
	}
	return false
}

func (a *ResilientRedisCacheAdapter) recordSuccess() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = 0
	a.state = "CLOSED"
}

func (a *ResilientRedisCacheAdapter) recordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	if a.failures >= a.failureThreshold {
		a.state = "OPEN"
		a.openedAt = time.Now()
	}
}
