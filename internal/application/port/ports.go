package port

import (
	"context"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
)

// LedgerEntryCommittedEvent represents the incoming event from Kafka topic `ledger.entry.committed`.
type LedgerEntryCommittedEvent struct {
	TenantID          string          `json:"tenant_id"`
	IdempotencyKey    string          `json:"idempotency_key"`
	JournalEntryID    string          `json:"journal_entry_id"`
	SourceReferenceID string          `json:"source_reference_id"`
	PostingDate       string          `json:"posting_date"`
	TotalAmount       decimal.Decimal `json:"total_amount"`
	Currency          string          `json:"currency"`
	CategoryCode      string          `json:"category_code"`
	SubcategoryCode   string          `json:"subcategory_code"`
	Direction         string          `json:"direction"`
}

// ForecastUseCase defines the inbound port for monthly Run-Rate recalculation upon ledger commits.
type ForecastUseCase interface {
	RecalculateMonthlyForecast(ctx context.Context, input domain.RunRateInput) (domain.RunRateForecastResult, error)
	HandleCommittedLedgerEvent(ctx context.Context, event LedgerEntryCommittedEvent) (domain.RunRateForecastResult, error)
}

// SimulateContractUseCase defines the inbound port for synchronous gRPC SAC, Price, and Consórcio simulations.
type SimulateContractUseCase interface {
	SimulateContractAmortization(ctx context.Context, input domain.ContractSimulationInput) (domain.ContractSimulationOutput, error)
}

// ReadModelCachePort defines the outbound port for reading/updating the low-latency Redis/Valkey cache (< 25ms).
type ReadModelCachePort interface {
	SaveForecastSnapshot(ctx context.Context, result domain.RunRateForecastResult) error
	GetForecastSnapshot(ctx context.Context, tenantID string, referenceMonth string) (*domain.RunRateForecastResult, error)
}
