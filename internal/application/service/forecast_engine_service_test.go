package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/port"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
)

type fakeCache struct {
	saved []domain.RunRateForecastResult
	err   error
}

func (f *fakeCache) SaveForecastSnapshot(_ context.Context, r domain.RunRateForecastResult) error {
	f.saved = append(f.saved, r)
	return f.err
}

func (f *fakeCache) GetForecastSnapshot(context.Context, string, string) (*domain.RunRateForecastResult, error) {
	return nil, nil
}

func validInput() domain.RunRateInput {
	return domain.RunRateInput{
		TenantID: "t1", ReferenceMonth: "2026-04", CurrentDayOfMonth: 10, TotalDaysInMonth: 30,
		VariableSpendToDate: decimal.NewFromInt(1000), SettledFixedExpenses: decimal.NewFromInt(100),
		RegularNetIncome: decimal.NewFromInt(10000),
	}
}

func TestRecalculateMonthlyForecast(t *testing.T) {
	t.Parallel()
	bad := validInput()
	bad.RegularNetIncome = decimal.Zero
	tests := []struct {
		name      string
		input     domain.RunRateInput
		cache     *fakeCache
		nilCache  bool
		wantErr   error
		wantSaved int
	}{
		{"saves snapshot", validInput(), &fakeCache{}, false, nil, 1},
		{"cache error ignored", validInput(), &fakeCache{err: errors.New("down")}, false, nil, 1},
		{"nil cache", validInput(), nil, true, nil, 0},
		{"domain validation error", bad, &fakeCache{}, false, domain.ErrNegativeIncome, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var svc *ForecastEngineService
			if tt.nilCache {
				svc = NewForecastEngineService(nil)
			} else {
				svc = NewForecastEngineService(tt.cache)
			}
			res, err := svc.RecalculateMonthlyForecast(context.Background(), tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err=%v want %v", err, tt.wantErr)
			}
			if tt.cache != nil && len(tt.cache.saved) != tt.wantSaved {
				t.Fatalf("saved %d want %d", len(tt.cache.saved), tt.wantSaved)
			}
			if tt.wantErr == nil && res.TenantID != "t1" {
				t.Fatalf("unexpected result %+v", res)
			}
		})
	}
}

func TestHandleCommittedLedgerEvent(t *testing.T) {
	t.Parallel()
	ev := func(date, dir, amt string) port.LedgerEntryCommittedEvent {
		return port.LedgerEntryCommittedEvent{TenantID: "t1", PostingDate: date, Direction: dir, TotalAmount: decimal.RequireFromString(amt)}
	}
	tests := []struct {
		name      string
		events    []port.LedgerEntryCommittedEvent
		wantMonth string
		wantDay   int // remaining = total - day
		wantTotal int
		wantSpend string // variable spend implied by daily avg * day
	}{
		{"debit accumulates", []port.LedgerEntryCommittedEvent{ev("2026-04-10", "DEBIT", "100")}, "2026-04", 10, 30, "100"},
		{"two debits same month", []port.LedgerEntryCommittedEvent{ev("2026-02-10", "DEBIT", "100"), ev("2026-02-10", "DEBIT", "50")}, "2026-02", 10, 28, "150"},
		{"credit reduces", []port.LedgerEntryCommittedEvent{ev("2026-04-10", "DEBIT", "100"), ev("2026-04-10", "CREDIT", "40")}, "2026-04", 10, 30, "60"},
		{"credit floors at zero", []port.LedgerEntryCommittedEvent{ev("2026-04-10", "CREDIT", "40")}, "2026-04", 10, 30, "0"},
		{"invalid date uses defaults", []port.LedgerEntryCommittedEvent{ev("garbage", "DEBIT", "300")}, "2026-03", 15, 31, "300"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache := &fakeCache{}
			svc := NewForecastEngineService(cache)
			var res domain.RunRateForecastResult
			var err error
			for _, e := range tt.events {
				res, err = svc.HandleCommittedLedgerEvent(context.Background(), e)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if res.ReferenceMonth != tt.wantMonth || res.RemainingDays != tt.wantTotal-tt.wantDay {
				t.Fatalf("got month=%s remaining=%d", res.ReferenceMonth, res.RemainingDays)
			}
			spend := res.DailyMovingAverage.Mul(decimal.NewFromInt(int64(tt.wantDay)))
			if !spend.Equal(decimal.RequireFromString(tt.wantSpend)) {
				t.Fatalf("implied spend %s want %s", spend, tt.wantSpend)
			}
			if len(cache.saved) != len(tt.events) {
				t.Fatalf("expected %d snapshots, got %d", len(tt.events), len(cache.saved))
			}
		})
	}
}

func TestSimulateContractAmortization(t *testing.T) {
	t.Parallel()
	svc := NewForecastEngineService(nil)
	out, err := svc.SimulateContractAmortization(context.Background(), domain.ContractSimulationInput{
		System: domain.AmortizationSAC, OutstandingBalance: decimal.NewFromInt(1200), RemainingInstallments: 12,
	})
	if err != nil || out.OriginalSummary.TotalInstallments != 12 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if _, err = svc.SimulateContractAmortization(context.Background(), domain.ContractSimulationInput{}); !errors.Is(err, domain.ErrInvalidBalance) {
		t.Fatalf("err=%v", err)
	}
}

func TestDaysInMonth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		y    int
		m    time.Month
		want int
	}{{2026, time.January, 31}, {2026, time.February, 28}, {2028, time.February, 29}, {2026, time.April, 30}, {2026, time.December, 31}}
	for _, tt := range tests {
		if got := daysInMonth(tt.y, tt.m); got != tt.want {
			t.Fatalf("%d-%d got %d want %d", tt.y, tt.m, got, tt.want)
		}
	}
}
