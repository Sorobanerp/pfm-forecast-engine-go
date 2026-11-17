package domain

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractAmortizationSimulator_EdgeCasesAndValidation(t *testing.T) {
	sim := NewContractAmortizationSimulator()

	baseInput := ContractSimulationInput{
		TenantID:                "t-1",
		ContractID:              "c-1",
		System:                  AmortizationSAC,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.NewFromFloat(100000.00),
		RemainingInstallments:   120,
		MonthlyInterestRate:     decimal.NewFromFloat(0.01),
		MonthlyInsuranceAndFees: decimal.NewFromFloat(50.00),
		ExtraAmortizationAmount: decimal.NewFromFloat(10000.00),
		StartCompetenceMonth:    "2026-04",
	}

	// 1. OutstandingBalance <= 0
	badBal := baseInput
	badBal.OutstandingBalance = decimal.Zero
	_, err := sim.Simulate(badBal)
	assert.ErrorIs(t, err, ErrInvalidBalance)

	// 2. RemainingInstallments <= 0
	badInst := baseInput
	badInst.RemainingInstallments = 0
	_, err = sim.Simulate(badInst)
	assert.ErrorIs(t, err, ErrInvalidInstallments)

	// 3. ExtraAmortization exceeds balance
	badExtra := baseInput
	badExtra.ExtraAmortizationAmount = decimal.NewFromFloat(150000.00)
	_, err = sim.Simulate(badExtra)
	assert.ErrorIs(t, err, ErrInvalidExtraAmount)

	// 4. Zero interest rate SAC & PRICE
	zeroRateSAC := baseInput
	zeroRateSAC.MonthlyInterestRate = decimal.Zero
	resZeroSAC, err := sim.Simulate(zeroRateSAC)
	require.NoError(t, err)
	assert.NotNil(t, resZeroSAC)

	zeroRatePrice := baseInput
	zeroRatePrice.System = AmortizationPrice
	zeroRatePrice.MonthlyInterestRate = decimal.Zero
	resZeroPrice, err := sim.Simulate(zeroRatePrice)
	require.NoError(t, err)
	assert.NotNil(t, resZeroPrice)

	// 5. PRICE reduce term
	priceReduceTerm := baseInput
	priceReduceTerm.System = AmortizationPrice
	priceReduceTerm.Strategy = StrategyReduceTerm
	resPriceTerm, err := sim.Simulate(priceReduceTerm)
	require.NoError(t, err)
	assert.NotNil(t, resPriceTerm)
	assert.True(t, resPriceTerm.InstallmentsReduced > 0)

	// 6. PRICE reduce installment
	priceReduceInst := baseInput
	priceReduceInst.System = AmortizationPrice
	priceReduceInst.Strategy = StrategyReduceInstallment
	resPriceInst, err := sim.Simulate(priceReduceInst)
	require.NoError(t, err)
	assert.NotNil(t, resPriceInst)

	// 7. SAC reduce installment
	sacReduceInst := baseInput
	sacReduceInst.System = AmortizationSAC
	sacReduceInst.Strategy = StrategyReduceInstallment
	resSacInst, err := sim.Simulate(sacReduceInst)
	require.NoError(t, err)
	assert.NotNil(t, resSacInst)

	// 8. Consorcio simulation
	consorcioInput := baseInput
	consorcioInput.System = AmortizationConsorcio
	consorcioInput.ConsorcioAdminFeeRate = decimal.NewFromFloat(0.16)
	consorcioInput.ConsorcioReadjustmentRate = decimal.NewFromFloat(0.05)
	resConsorcio, err := sim.Simulate(consorcioInput)
	require.NoError(t, err)
	assert.NotNil(t, resConsorcio)
}

func TestRunRateForecaster_EdgeCasesAndValidation(t *testing.T) {
	forecaster := NewRunRateForecaster()

	// 1. Invalid days in month
	_, err := forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:          "t-1",
		CurrentDayOfMonth: 0,
		TotalDaysInMonth:  30,
		RegularNetIncome:  decimal.NewFromFloat(5000.00),
	})
	assert.ErrorIs(t, err, ErrInvalidDaysInMonth)

	_, err = forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:          "t-1",
		CurrentDayOfMonth: 31,
		TotalDaysInMonth:  30,
		RegularNetIncome:  decimal.NewFromFloat(5000.00),
	})
	assert.ErrorIs(t, err, ErrInvalidDaysInMonth)

	// 2. Negative regular net income
	_, err = forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:          "t-1",
		CurrentDayOfMonth: 10,
		TotalDaysInMonth:  30,
		RegularNetIncome:  decimal.Zero,
	})
	assert.ErrorIs(t, err, ErrNegativeIncome)

	// 3. Override DailyMovingAverage
	resOverride, err := forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:            "t-1",
		CurrentDayOfMonth:   10,
		TotalDaysInMonth:    30,
		VariableSpendToDate: decimal.NewFromFloat(1000.00),
		DailyMovingAverage:  decimal.NewFromFloat(150.00),
		RegularNetIncome:    decimal.NewFromFloat(5000.00),
	})
	require.NoError(t, err)
	assert.True(t, resOverride.DailyMovingAverage.Equal(decimal.NewFromFloat(150.00)))

	// 4. Yellow Traffic Light (85% - 100%)
	// Income: 5000. Fixed: 1500. Var: 1500 (day 15 of 30 -> 100/day -> remaining 1500). Total: 4500 (90%)
	resYellow, err := forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:             "t-1",
		CurrentDayOfMonth:    15,
		TotalDaysInMonth:     30,
		VariableSpendToDate:  decimal.NewFromFloat(1500.00),
		SettledFixedExpenses: decimal.NewFromFloat(1500.00),
		RegularNetIncome:     decimal.NewFromFloat(5000.00),
	})
	require.NoError(t, err)
	assert.Equal(t, TrafficLightYellow, resYellow.TrafficLight)

	// 5. Red Traffic Light (> 100%) and Deficit Risk
	resRed, err := forecaster.ForecastMonthlyRunRate(RunRateInput{
		TenantID:             "t-1",
		CurrentDayOfMonth:    15,
		TotalDaysInMonth:     30,
		VariableSpendToDate:  decimal.NewFromFloat(3500.00),
		SettledFixedExpenses: decimal.NewFromFloat(3000.00),
		RegularNetIncome:     decimal.NewFromFloat(5000.00),
	})
	require.NoError(t, err)
	assert.Equal(t, TrafficLightRed, resRed.TrafficLight)
	assert.True(t, resRed.HasDeficitRisk)
}

func TestContractAmortizationSimulator_AdditionalCoverage(t *testing.T) {
	sim := NewContractAmortizationSimulator()

	// 1. computeReducedTerm edge cases
	// SAC with minimal extra prepayment -> ceilTerm >= originalTerm
	resMinExtra, err := sim.Simulate(ContractSimulationInput{
		TenantID:                "t-1",
		ContractID:              "c-1",
		System:                  AmortizationSAC,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.NewFromFloat(10000.00),
		RemainingInstallments:   10,
		MonthlyInterestRate:     decimal.NewFromFloat(0.01),
		ExtraAmortizationAmount: decimal.RequireFromString("0.0100"),
		StartCompetenceMonth:    "invalid-date-format",
	})
	require.NoError(t, err)
	assert.NotNil(t, resMinExtra)

	// Consorcio reduce term
	resConsorcioTerm, err := sim.Simulate(ContractSimulationInput{
		TenantID:                "t-1",
		ContractID:              "c-1",
		System:                  AmortizationConsorcio,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.NewFromFloat(50000.00),
		RemainingInstallments:   50,
		ConsorcioAdminFeeRate:   decimal.NewFromFloat(0.15),
		ExtraAmortizationAmount: decimal.NewFromFloat(10000.00),
	})
	require.NoError(t, err)
	assert.NotNil(t, resConsorcioTerm)

	// Price almost full prepayment -> count < 1
	resPriceAlmostFull, err := sim.Simulate(ContractSimulationInput{
		TenantID:                "t-1",
		ContractID:              "c-1",
		System:                  AmortizationPrice,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.NewFromFloat(10000.00),
		RemainingInstallments:   10,
		MonthlyInterestRate:     decimal.NewFromFloat(0.01),
		ExtraAmortizationAmount: decimal.RequireFromString("9999.9950"),
	})
	require.NoError(t, err)
	assert.NotNil(t, resPriceAlmostFull)

	// default system in computeReducedTerm
	term := sim.computeReducedTerm("UNKNOWN", decimal.NewFromFloat(100), decimal.NewFromFloat(50), 10, decimal.Zero)
	assert.Equal(t, 10, term)
}
