package redisadapter

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFakeRedisServer spins up an in-memory RESP server for unit tests.
func startFakeRedisServer(t *testing.T) (string, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	stop := make(chan struct{})

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil || n == 0 {
						return
					}
					text := strings.ToUpper(string(buf[:n]))
					if strings.Contains(text, "HELLO") {
						_, _ = c.Write([]byte("-ERR unknown command 'HELLO'\r\n"))
					} else if strings.Contains(text, "PING") {
						_, _ = c.Write([]byte("+PONG\r\n"))
					} else if strings.Contains(text, "GET") {
						payload := `{"tenant_id":"t-live","reference_month":"2026-04","total_projected_outflow":"4200.0000"}`
						_, _ = c.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(payload), payload)))
					} else {
						_, _ = c.Write([]byte("+OK\r\n"))
					}
				}
			}(conn)
		}
	}()

	cleanup := func() {
		close(stop)
		_ = l.Close()
	}

	return l.Addr().String(), cleanup
}

func TestResilientRedisCacheAdapter_WithLiveRedisProtocol(t *testing.T) {
	addr, cleanup := startFakeRedisServer(t)
	defer cleanup()

	adapter := NewResilientRedisCacheAdapter(addr)
	ctx := context.Background()

	snapshot := domain.RunRateForecastResult{
		TenantID:              "t-live",
		ReferenceMonth:        "2026-04",
		TotalProjectedOutflow: decimal.NewFromFloat(4200.00),
		RegularNetIncome:      decimal.NewFromFloat(5000.00),
		TrafficLight:          domain.TrafficLightGreen,
		CalculatedAt:          time.Now(),
	}

	err := adapter.SaveForecastSnapshot(ctx, snapshot)
	require.NoError(t, err)

	got, err := adapter.GetForecastSnapshot(ctx, "t-live", "2026-04")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "t-live", got.TenantID)
	assert.True(t, got.TotalProjectedOutflow.Equal(decimal.NewFromFloat(4200.00)))
}

func TestResilientRedisCacheAdapter_CircuitOpenShortCircuit(t *testing.T) {
	adapter := NewResilientRedisCacheAdapter("127.0.0.1:54321")
	adapter.mu.Lock()
	adapter.state = "OPEN"
	adapter.openedAt = time.Now()
	adapter.mu.Unlock()

	ctx := context.Background()
	snapshot := domain.RunRateForecastResult{
		TenantID:       "t-open",
		ReferenceMonth: "2026-04",
	}

	// When circuit open, SaveForecastSnapshot skips Redis and returns nil immediately
	err := adapter.SaveForecastSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// When circuit open, GetForecastSnapshot skips Redis and returns from fallbackData
	got, err := adapter.GetForecastSnapshot(ctx, "t-open", "2026-04")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "t-open", got.TenantID)
}

func TestResilientRedisCacheAdapter_FallbackMemory(t *testing.T) {
	ctx := context.Background()
	adapter := NewResilientRedisCacheAdapter("")

	assert.Equal(t, "CLOSED", adapter.CircuitState())

	snapshot := domain.RunRateForecastResult{
		TenantID:                  "tenant-123",
		ReferenceMonth:            "2026-04",
		RemainingDays:             15,
		DailyMovingAverage:        decimal.NewFromFloat(100.00),
		TotalProjectedOutflow:     decimal.NewFromFloat(3000.00),
		RegularNetIncome:          decimal.NewFromFloat(5000.00),
		ProjectedSurplusOrDeficit: decimal.NewFromFloat(2000.00),
		TrafficLight:              domain.TrafficLightGreen,
		CalculatedAt:              time.Now(),
	}

	err := adapter.SaveForecastSnapshot(ctx, snapshot)
	require.NoError(t, err)

	res, err := adapter.GetForecastSnapshot(ctx, "tenant-123", "2026-04")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "tenant-123", res.TenantID)
	assert.Equal(t, "2026-04", res.ReferenceMonth)
	assert.True(t, res.TotalProjectedOutflow.Equal(decimal.NewFromFloat(3000.00)))

	// Non-existent key
	notFound, err := adapter.GetForecastSnapshot(ctx, "non-existent", "2026-04")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestResilientRedisCacheAdapter_CircuitBreakerTransitions(t *testing.T) {
	adapter := NewResilientRedisCacheAdapter("")

	assert.Equal(t, "CLOSED", adapter.CircuitState())
	assert.False(t, adapter.isCircuitOpen())

	// Trigger 3 failures to trip the circuit
	adapter.recordFailure()
	assert.Equal(t, "CLOSED", adapter.CircuitState())
	adapter.recordFailure()
	assert.Equal(t, "CLOSED", adapter.CircuitState())
	adapter.recordFailure()

	assert.Equal(t, "OPEN", adapter.CircuitState())
	assert.True(t, adapter.isCircuitOpen())

	// Simulate cooldown expiry
	adapter.mu.Lock()
	adapter.openedAt = time.Now().Add(-20 * time.Second)
	adapter.mu.Unlock()

	assert.Equal(t, "HALF_OPEN", adapter.CircuitState())
	assert.False(t, adapter.isCircuitOpen())
	assert.Equal(t, "HALF_OPEN", adapter.CircuitState())

	// Record success resets circuit
	adapter.recordSuccess()
	assert.Equal(t, "CLOSED", adapter.CircuitState())
	assert.False(t, adapter.isCircuitOpen())
}

func TestResilientRedisCacheAdapter_ClientFailureGracefulFallback(t *testing.T) {
	ctx := context.Background()
	// Connect to non-responsive port
	adapter := NewResilientRedisCacheAdapter("127.0.0.1:59999")
	adapter.timeout = 50 * time.Millisecond
	adapter.maxRetries = 1

	snapshot := domain.RunRateForecastResult{
		TenantID:              "tenant-fail",
		ReferenceMonth:        "2026-05",
		TotalProjectedOutflow: decimal.NewFromFloat(500.00),
		CalculatedAt:          time.Now(),
	}

	// Should not error even if redis unreachable because local memory fallback works
	err := adapter.SaveForecastSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Get should also fall back to memory
	res, err := adapter.GetForecastSnapshot(ctx, "tenant-fail", "2026-05")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "tenant-fail", res.TenantID)
}
