package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

type TrafficLightStatus string

const (
	TrafficLightGreen  TrafficLightStatus = "GREEN"
	TrafficLightYellow TrafficLightStatus = "YELLOW"
	TrafficLightRed    TrafficLightStatus = "RED"
)

var (
	ErrInvalidDaysInMonth = errors.New("current day and total days in month must be valid (1 <= current <= total <= 31)")
	ErrNegativeIncome     = errors.New("regular net income must be strictly positive")
)

// RunRateInput holds the immutable monthly snapshot for a tenant (RN02 / UC-06).
type RunRateInput struct {
	TenantID              string
	ReferenceMonth        string
	CurrentDayOfMonth     int
	TotalDaysInMonth      int
	VariableSpendToDate   decimal.Decimal
	DailyMovingAverage    decimal.Decimal // Optional override; if zero, computed as VariableSpendToDate / CurrentDayOfMonth
	SettledFixedExpenses  decimal.Decimal
	CurrentCardInvoices   decimal.Decimal
	ScheduledInstallments decimal.Decimal
	RegularNetIncome      decimal.Decimal
}

// RunRateForecastResult represents the immutable monthly cash-flow & competence projection.
type RunRateForecastResult struct {
	TenantID                  string             `json:"tenant_id"`
	ReferenceMonth            string             `json:"reference_month"`
	DailyMovingAverage        decimal.Decimal    `json:"daily_moving_average"`
	RemainingDays             int                `json:"remaining_days"`
	ProjectedRemainingVar     decimal.Decimal    `json:"projected_remaining_variable"`
	TotalProjectedOutflow     decimal.Decimal    `json:"total_projected_outflow"`
	RegularNetIncome          decimal.Decimal    `json:"regular_net_income"`
	ProjectedSurplusOrDeficit decimal.Decimal    `json:"projected_surplus_or_deficit"`
	IncomeCommitmentPct       decimal.Decimal    `json:"income_commitment_pct"`
	TrafficLight              TrafficLightStatus `json:"traffic_light"`
	HasDeficitRisk            bool               `json:"has_deficit_risk"`
	CalculatedAt              time.Time          `json:"calculated_at"`
}

// RunRateForecaster calculates the daily variable spending pace and confronts total projected
// commitments against the tenant's regular net income (RN02 / UC-06).
type RunRateForecaster struct{}

func NewRunRateForecaster() *RunRateForecaster {
	return &RunRateForecaster{}
}

func (f *RunRateForecaster) ForecastMonthlyRunRate(input RunRateInput) (RunRateForecastResult, error) {
	if input.CurrentDayOfMonth < 1 || input.TotalDaysInMonth < 28 || input.CurrentDayOfMonth > input.TotalDaysInMonth {
		return RunRateForecastResult{}, ErrInvalidDaysInMonth
	}
	if !input.RegularNetIncome.IsPositive() {
		return RunRateForecastResult{}, ErrNegativeIncome
	}

	dailyAvg := input.DailyMovingAverage
	if !dailyAvg.IsPositive() {
		dailyAvg = input.VariableSpendToDate.
			DivRound(decimal.NewFromInt(int64(input.CurrentDayOfMonth)), 8).
			RoundBank(4)
	} else {
		dailyAvg = dailyAvg.RoundBank(4)
	}

	remainingDays := input.TotalDaysInMonth - input.CurrentDayOfMonth
	projectedRemainingVar := dailyAvg.
		Mul(decimal.NewFromInt(int64(remainingDays))).
		RoundBank(4)

	totalProjectedOutflow := input.SettledFixedExpenses.
		Add(input.CurrentCardInvoices).
		Add(input.ScheduledInstallments).
		Add(input.VariableSpendToDate).
		Add(projectedRemainingVar).
		RoundBank(4)

	surplusOrDeficit := input.RegularNetIncome.Sub(totalProjectedOutflow).RoundBank(4)

	commitmentPct := totalProjectedOutflow.
		Mul(decimal.NewFromInt(100)).
		DivRound(input.RegularNetIncome, 8).
		RoundBank(4)

	yellowThreshold := decimal.NewFromInt(85)
	redThreshold := decimal.NewFromInt(100)

	status := TrafficLightGreen
	hasDeficit := false
	if commitmentPct.GreaterThan(redThreshold) || surplusOrDeficit.IsNegative() {
		status = TrafficLightRed
		hasDeficit = true
	} else if commitmentPct.GreaterThan(yellowThreshold) {
		status = TrafficLightYellow
	}

	return RunRateForecastResult{
		TenantID:                  input.TenantID,
		ReferenceMonth:            input.ReferenceMonth,
		DailyMovingAverage:        dailyAvg,
		RemainingDays:             remainingDays,
		ProjectedRemainingVar:     projectedRemainingVar,
		TotalProjectedOutflow:     totalProjectedOutflow,
		RegularNetIncome:          input.RegularNetIncome.RoundBank(4),
		ProjectedSurplusOrDeficit: surplusOrDeficit,
		IncomeCommitmentPct:       commitmentPct,
		TrafficLight:              status,
		HasDeficitRisk:            hasDeficit,
		CalculatedAt:              time.Now().UTC(),
	}, nil
}
