package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestRunRateForecaster_DeficitRiskAndTrafficLight(t *testing.T) {
	t.Parallel()
	forecaster := NewRunRateForecaster()

	// Day 10 of a 30-day month:
	// Variable spend so far = R$ 2.500,00 -> Daily moving average = R$ 250,00/day
	// Remaining 20 days * R$ 250,00 = R$ 5.000,00 projected remaining variable
	// Settled Fixed (3.500) + Current Invoices (2.800) + Scheduled Installments (1.200) + Var To Date (2.500) + Projected Var (5.000) = R$ 15.000,00
	// Regular Net Income = R$ 12.000,00 -> Projected Deficit = -R$ 3.000,00 (TrafficLight RED)
	input := RunRateInput{
		TenantID:              "tenant-dantas-001",
		ReferenceMonth:        "2026-04",
		CurrentDayOfMonth:     10,
		TotalDaysInMonth:      30,
		VariableSpendToDate:   decimal.RequireFromString("2500.0000"),
		SettledFixedExpenses:  decimal.RequireFromString("3500.0000"),
		CurrentCardInvoices:   decimal.RequireFromString("2800.0000"),
		ScheduledInstallments: decimal.RequireFromString("1200.0000"),
		RegularNetIncome:      decimal.RequireFromString("12000.0000"),
	}

	res, err := forecaster.ForecastMonthlyRunRate(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.DailyMovingAverage.Equal(decimal.RequireFromString("250.0000")) {
		t.Fatalf("expected daily avg 250.0000, got %s", res.DailyMovingAverage)
	}
	if res.RemainingDays != 20 {
		t.Fatalf("expected 20 remaining days, got %d", res.RemainingDays)
	}
	if !res.ProjectedRemainingVar.Equal(decimal.RequireFromString("5000.0000")) {
		t.Fatalf("expected projected remaining var 5000.0000, got %s", res.ProjectedRemainingVar)
	}
	if !res.TotalProjectedOutflow.Equal(decimal.RequireFromString("15000.0000")) {
		t.Fatalf("expected total projected outflow 15000.0000, got %s", res.TotalProjectedOutflow)
	}
	if !res.ProjectedSurplusOrDeficit.Equal(decimal.RequireFromString("-3000.0000")) {
		t.Fatalf("expected deficit -3000.0000, got %s", res.ProjectedSurplusOrDeficit)
	}
	if res.TrafficLight != TrafficLightRed || !res.HasDeficitRisk {
		t.Fatalf("expected RED traffic light with deficit risk, got %s (%v)", res.TrafficLight, res.HasDeficitRisk)
	}
}

func TestContractAmortizationSimulator_SAC_ReduceTermVsReduceInstallment(t *testing.T) {
	t.Parallel()
	sim := NewContractAmortizationSimulator()

	baseBalance := decimal.RequireFromString("300000.0000")
	extraPrepay := decimal.RequireFromString("60000.0000")
	rate := decimal.RequireFromString("0.0080") // 0.80% a.m.
	fees := decimal.RequireFromString("85.0000")

	// 1. SAC Reduce Term (Reduzir Prazo)
	reduceTermInput := ContractSimulationInput{
		TenantID:                "tenant-dantas-001",
		ContractID:              "contract-sac-01",
		System:                  AmortizationSAC,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      baseBalance,
		RemainingInstallments:   120,
		MonthlyInterestRate:     rate,
		MonthlyInsuranceAndFees: fees,
		ExtraAmortizationAmount: extraPrepay,
		StartCompetenceMonth:    "2026-04",
	}

	outTerm, err := sim.Simulate(reduceTermInput)
	if err != nil {
		t.Fatalf("unexpected error in SAC REDUCE_TERM: %v", err)
	}

	// Verify input was not mutated
	if !reduceTermInput.OutstandingBalance.Equal(baseBalance) {
		t.Fatalf("input struct was mutated!")
	}
	// 300k / 120 = 2.500/month principal; with 60k extra -> 240k / 2.500 = 96 installments (24 months reduced!)
	if outTerm.InstallmentsReduced != 24 {
		t.Fatalf("expected 24 installments reduced in SAC, got %d", outTerm.InstallmentsReduced)
	}
	if !outTerm.InterestSavingsAmount.IsPositive() {
		t.Fatalf("expected positive interest savings in SAC REDUCE_TERM, got %s", outTerm.InterestSavingsAmount)
	}

	// 2. SAC Reduce Installment (Reduzir Parcela)
	reduceInstInput := reduceTermInput
	reduceInstInput.Strategy = StrategyReduceInstallment

	outInst, err := sim.Simulate(reduceInstInput)
	if err != nil {
		t.Fatalf("unexpected error in SAC REDUCE_INSTALLMENT: %v", err)
	}
	if outInst.InstallmentsReduced != 0 {
		t.Fatalf("expected 0 installments reduced when keeping original term, got %d", outInst.InstallmentsReduced)
	}
	if !outInst.MonthlyInstallmentReduction.IsPositive() {
		t.Fatalf("expected positive monthly installment reduction, got %s", outInst.MonthlyInstallmentReduction)
	}
	// Reducing term saves strictly more total interest + fees than reducing installment
	if !outTerm.InterestSavingsAmount.GreaterThan(outInst.InterestSavingsAmount) {
		t.Fatalf("expected REDUCE_TERM savings (%s) > REDUCE_INSTALLMENT savings (%s)",
			outTerm.InterestSavingsAmount, outInst.InterestSavingsAmount)
	}
}

func TestContractAmortizationSimulator_Price(t *testing.T) {
	t.Parallel()
	sim := NewContractAmortizationSimulator()

	input := ContractSimulationInput{
		TenantID:                "tenant-dantas-001",
		ContractID:              "contract-price-01",
		System:                  AmortizationPrice,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.RequireFromString("100000.0000"),
		RemainingInstallments:   60,
		MonthlyInterestRate:     decimal.RequireFromString("0.0100"), // 1.00% a.m.
		MonthlyInsuranceAndFees: decimal.RequireFromString("40.0000"),
		ExtraAmortizationAmount: decimal.RequireFromString("25000.0000"),
		StartCompetenceMonth:    "2026-04",
	}

	out, err := sim.Simulate(input)
	if err != nil {
		t.Fatalf("unexpected error in Price simulation: %v", err)
	}
	if out.InstallmentsReduced <= 0 {
		t.Fatalf("expected installments reduced > 0 in Price REDUCE_TERM, got %d", out.InstallmentsReduced)
	}
	if !out.InterestSavingsAmount.IsPositive() {
		t.Fatalf("expected positive interest savings in Price simulation, got %s", out.InterestSavingsAmount)
	}
	lastBalance := out.SimulatedSchedule[len(out.SimulatedSchedule)-1].RemainingBalance
	if !lastBalance.IsZero() {
		t.Fatalf("expected final remaining balance 0.0000, got %s", lastBalance)
	}
}

func TestContractAmortizationSimulator_ConsorcioReadjustmentAndLance(t *testing.T) {
	t.Parallel()
	sim := NewContractAmortizationSimulator()

	input := ContractSimulationInput{
		TenantID:                  "tenant-dantas-001",
		ContractID:                "contract-consorcio-01",
		System:                    AmortizationConsorcio,
		Strategy:                  StrategyReduceInstallment,
		OutstandingBalance:        decimal.RequireFromString("200000.0000"),
		RemainingInstallments:     48,
		MonthlyInterestRate:       decimal.Zero,
		ConsorcioAdminFeeRate:     decimal.RequireFromString("0.1600"), // 16% admin fee
		ConsorcioReadjustmentRate: decimal.RequireFromString("0.0500"), // 5% annual readjustment (month 13, 25, 37)
		MonthlyInsuranceAndFees:   decimal.RequireFromString("25.0000"),
		ExtraAmortizationAmount:   decimal.RequireFromString("50000.0000"), // Lance de R$ 50.000,00
		StartCompetenceMonth:      "2026-04",
	}

	start := time.Now()
	out, err := sim.Simulate(input)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error in Consorcio simulation: %v", err)
	}
	if elapsed >= 25*time.Millisecond {
		t.Fatalf("expected simulation under 25ms, took %v", elapsed)
	}

	// Check annual readjustment happened on installment 13 (principal quota increases at month 13)
	if !out.OriginalSchedule[12].PrincipalAmount.GreaterThan(out.OriginalSchedule[11].PrincipalAmount) {
		t.Fatalf("expected Consorcio annual readjustment at month 13 (%s > %s)",
			out.OriginalSchedule[12].PrincipalAmount, out.OriginalSchedule[11].PrincipalAmount)
	}
	if !out.MonthlyInstallmentReduction.IsPositive() {
		t.Fatalf("expected Consorcio lance to reduce monthly installment, got %s", out.MonthlyInstallmentReduction)
	}
	if !out.InterestSavingsAmount.IsPositive() {
		t.Fatalf("expected Consorcio lance to save proportional admin fees/readjustments, got %s", out.InterestSavingsAmount)
	}
}

func BenchmarkForecastAndAmortizationUnder25ms(b *testing.B) {
	forecaster := NewRunRateForecaster()
	sim := NewContractAmortizationSimulator()

	rrInput := RunRateInput{
		TenantID:              "tenant-bench",
		ReferenceMonth:        "2026-04",
		CurrentDayOfMonth:     15,
		TotalDaysInMonth:      30,
		VariableSpendToDate:   decimal.RequireFromString("3200.0000"),
		SettledFixedExpenses:  decimal.RequireFromString("4100.0000"),
		CurrentCardInvoices:   decimal.RequireFromString("2900.0000"),
		ScheduledInstallments: decimal.RequireFromString("1800.0000"),
		RegularNetIncome:      decimal.RequireFromString("14000.0000"),
	}

	simInput := ContractSimulationInput{
		TenantID:                "tenant-bench",
		ContractID:              "contract-bench-sac-360",
		System:                  AmortizationSAC,
		Strategy:                StrategyReduceTerm,
		OutstandingBalance:      decimal.RequireFromString("450000.0000"),
		RemainingInstallments:   360,
		MonthlyInterestRate:     decimal.RequireFromString("0.0079"),
		MonthlyInsuranceAndFees: decimal.RequireFromString("95.0000"),
		ExtraAmortizationAmount: decimal.RequireFromString("50000.0000"),
		StartCompetenceMonth:    "2026-04",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = forecaster.ForecastMonthlyRunRate(rrInput)
		_, _ = sim.Simulate(simInput)
	}
}
