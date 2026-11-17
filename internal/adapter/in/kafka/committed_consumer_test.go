package kafka

import (
	"context"
	"testing"
	"time"

	redisadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/out/redis"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaConsumerAndRedisCacheUpdateUnder25ms(t *testing.T) {
	t.Parallel()

	// Empty addr uses resilient fallback memory so unit tests don't require external Redis daemon,
	// while exercising the full consumer -> service -> cache port pipeline.
	cache := redisadapter.NewResilientRedisCacheAdapter("")
	svc := service.NewForecastEngineService(cache)
	consumer := NewLedgerCommittedConsumer(nil, "test-group", svc)

	payload := []byte(`{
		"tenant_id": "tenant-dantas-001",
		"idempotency_key": "sha256-evt-001",
		"journal_entry_id": "11111111-2222-3333-4444-555555555555",
		"posting_date": "2026-04-10",
		"total_amount": "2500.0000",
		"currency": "BRL",
		"category_code": "ALIMENTACAO",
		"subcategory_code": "RESTAURANTES_GASTRONOMIA",
		"direction": "DEBIT"
	}`)

	res, elapsed, err := consumer.ProcessMessagePayload(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error processing committed ledger event: %v", err)
	}
	if elapsed >= 25*time.Millisecond {
		t.Fatalf("SLO violation: expected recalculation + cache update < 25ms, took %v", elapsed)
	}
	if res.TenantID != "tenant-dantas-001" {
		t.Fatalf("expected tenant-dantas-001, got %s", res.TenantID)
	}

	cached, err := cache.GetForecastSnapshot(context.Background(), "tenant-dantas-001", "2026-04")
	if err != nil || cached == nil {
		t.Fatalf("expected cached forecast snapshot in Redis adapter, got err=%v", err)
	}
	if !cached.TotalProjectedOutflow.Equal(res.TotalProjectedOutflow) {
		t.Fatalf("cached total projected outflow mismatch: %s vs %s", cached.TotalProjectedOutflow, res.TotalProjectedOutflow)
	}

	// Close nil reader
	err = consumer.Close()
	assert.NoError(t, err)

	// Invalid JSON payload
	_, _, err = consumer.ProcessMessagePayload(context.Background(), []byte("{invalid json"))
	assert.Error(t, err)

	// Consumer with brokers
	consumerWithBrokers := NewLedgerCommittedConsumer([]string{"localhost:9092"}, "grp-1", svc)
	require.NotNil(t, consumerWithBrokers)
	err = consumerWithBrokers.Close()
	assert.NoError(t, err)
}
