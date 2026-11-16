package service

import (
	"context"
	"sync"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/port"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
)

type ForecastEngineService struct {
	forecaster *domain.RunRateForecaster
	simulator  *domain.ContractAmortizationSimulator
	cachePort  port.ReadModelCachePort
	mu         sync.RWMutex
	states     map[string]domain.RunRateInput
}

func NewForecastEngineService(cachePort port.ReadModelCachePort) *ForecastEngineService {
	return &ForecastEngineService{
		forecaster: domain.NewRunRateForecaster(),
		simulator:  domain.NewContractAmortizationSimulator(),
		cachePort:  cachePort,
		states:     make(map[string]domain.RunRateInput),
	}
}

func (s *ForecastEngineService) RecalculateMonthlyForecast(
	ctx context.Context,
	input domain.RunRateInput,
) (domain.RunRateForecastResult, error) {
	result, err := s.forecaster.ForecastMonthlyRunRate(input)
	if err != nil {
		return domain.RunRateForecastResult{}, err
	}
	s.mu.Lock()
	s.states[input.TenantID+":"+input.ReferenceMonth] = input
	s.mu.Unlock()

	if s.cachePort != nil {
		_ = s.cachePort.SaveForecastSnapshot(ctx, result)
	}
	return result, nil
}

func (s *ForecastEngineService) HandleCommittedLedgerEvent(
	ctx context.Context,
	event port.LedgerEntryCommittedEvent,
) (domain.RunRateForecastResult, error) {
	refMonth := "2026-03"
	currentDay := 15
	totalDays := 31
	if t, err := time.Parse("2006-01-02", event.PostingDate); err == nil {
		refMonth = t.Format("2006-01")
		currentDay = t.Day()
		totalDays = daysInMonth(t.Year(), t.Month())
	}

	key := event.TenantID + ":" + refMonth
	s.mu.Lock()
	state, exists := s.states[key]
	if !exists {
		state = domain.RunRateInput{
			TenantID:              event.TenantID,
			ReferenceMonth:        refMonth,
			CurrentDayOfMonth:     currentDay,
			TotalDaysInMonth:      totalDays,
			VariableSpendToDate:   decimal.Zero,
			SettledFixedExpenses:  decimal.NewFromInt(3500),
			CurrentCardInvoices:   decimal.NewFromInt(2000),
			ScheduledInstallments: decimal.NewFromInt(1500),
			RegularNetIncome:      decimal.NewFromInt(12000),
		}
	}
	state.CurrentDayOfMonth = currentDay
	state.TotalDaysInMonth = totalDays
	if event.Direction == "CREDIT" {
		state.VariableSpendToDate = state.VariableSpendToDate.Sub(event.TotalAmount)
		if state.VariableSpendToDate.IsNegative() {
			state.VariableSpendToDate = decimal.Zero
		}
	} else {
		state.VariableSpendToDate = state.VariableSpendToDate.Add(event.TotalAmount)
	}
	s.states[key] = state
	s.mu.Unlock()

	return s.RecalculateMonthlyForecast(ctx, state)
}

func (s *ForecastEngineService) SimulateContractAmortization(
	_ context.Context,
	input domain.ContractSimulationInput,
) (domain.ContractSimulationOutput, error) {
	return s.simulator.Simulate(input)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
