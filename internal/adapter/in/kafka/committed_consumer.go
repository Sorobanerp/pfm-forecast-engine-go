package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/port"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	kafkago "github.com/segmentio/kafka-go"
)

const LedgerEntryCommittedTopic = "ledger.entry.committed"

type LedgerCommittedConsumer struct {
	reader          *kafkago.Reader
	forecastUseCase port.ForecastUseCase
}

func NewLedgerCommittedConsumer(brokers []string, groupID string, forecastUseCase port.ForecastUseCase) *LedgerCommittedConsumer {
	var r *kafkago.Reader
	if len(brokers) > 0 {
		r = kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:  brokers,
			GroupID:  groupID,
			Topic:    LedgerEntryCommittedTopic,
			MinBytes: 1,
			MaxBytes: 10e6,
		})
	}
	return &LedgerCommittedConsumer{
		reader:          r,
		forecastUseCase: forecastUseCase,
	}
}

// ProcessMessagePayload parses a `ledger.entry.committed` payload, recalculates the monthly Run-Rate
// and updates the read-model cache in < 25ms.
func (c *LedgerCommittedConsumer) ProcessMessagePayload(
	ctx context.Context,
	payload []byte,
) (domain.RunRateForecastResult, time.Duration, error) {
	start := time.Now()
	var event port.LedgerEntryCommittedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return domain.RunRateForecastResult{}, 0, err
	}
	res, err := c.forecastUseCase.HandleCommittedLedgerEvent(ctx, event)
	elapsed := time.Since(start)
	return res, elapsed, err
}

func (c *LedgerCommittedConsumer) Close() error {
	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}
