package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type AmortizationSystem string

const (
	AmortizationSAC       AmortizationSystem = "SAC"
	AmortizationPrice     AmortizationSystem = "PRICE"
	AmortizationConsorcio AmortizationSystem = "CONSORCIO"
)

type PrepaymentStrategy string

const (
	StrategyReduceTerm        PrepaymentStrategy = "REDUCE_TERM"
	StrategyReduceInstallment PrepaymentStrategy = "REDUCE_INSTALLMENT"
)

var (
	ErrInvalidBalance      = errors.New("outstanding balance must be strictly positive")
	ErrInvalidInstallments = errors.New("remaining installments must be >= 1")
	ErrInvalidExtraAmount  = errors.New("extra amortization amount must be >= 0 and < outstanding balance")
)

// ContractSimulationInput holds immutable parameters for SAC, Price, and Consórcio simulation (UC-04).
type ContractSimulationInput struct {
	TenantID                  string
	ContractID                string
	System                    AmortizationSystem
	Strategy                  PrepaymentStrategy
	OutstandingBalance        decimal.Decimal
	RemainingInstallments     int
	MonthlyInterestRate       decimal.Decimal // e.g., 0.0085 (0.85% a.m.)
	ConsorcioAdminFeeRate     decimal.Decimal // e.g., 0.1600 (16% over letter of credit)
	ConsorcioReadjustmentRate decimal.Decimal // e.g., 0.0500 (5% annual index readjustment on letter of credit)
	MonthlyInsuranceAndFees   decimal.Decimal // MIP + DFI + fixed monthly admin fee
	ExtraAmortizationAmount   decimal.Decimal // Extraordinary lump-sum prepayment or Consórcio bid (lance)
	StartCompetenceMonth      string          // YYYY-MM (defaults to "2026-04")
}

// InstallmentRow represents a single immutable installment in an amortization table.
type InstallmentRow struct {
	InstallmentNumber      int             `json:"installment_number"`
	DueMonth               string          `json:"due_month"`
	PrincipalAmount        decimal.Decimal `json:"principal_amount"`
	InterestAmount         decimal.Decimal `json:"interest_amount"`
	FeesAmount             decimal.Decimal `json:"fees_amount"`
	TotalInstallmentAmount decimal.Decimal `json:"total_installment_amount"`
	RemainingBalance       decimal.Decimal `json:"remaining_balance"`
}

// ScheduleSummary aggregates total metrics for an amortization schedule.
type ScheduleSummary struct {
	TotalInstallments      int             `json:"total_installments"`
	FirstInstallmentAmount decimal.Decimal `json:"first_installment_amount"`
	LastInstallmentAmount  decimal.Decimal `json:"last_installment_amount"`
	TotalInterestPaid      decimal.Decimal `json:"total_interest_paid"`
	TotalFeesPaid          decimal.Decimal `json:"total_fees_paid"`
	TotalPaid              decimal.Decimal `json:"total_paid"`
}

// ContractSimulationOutput holds the pure immutable comparison between baseline and prepaid schedules.
type ContractSimulationOutput struct {
	TenantID                    string             `json:"tenant_id"`
	ContractID                  string             `json:"contract_id"`
	System                      AmortizationSystem `json:"amortization_system"`
	Strategy                    PrepaymentStrategy `json:"prepayment_strategy"`
	OriginalSummary             ScheduleSummary    `json:"original_summary"`
	SimulatedSummary            ScheduleSummary    `json:"simulated_summary"`
	InstallmentsReduced         int                `json:"installments_reduced"`
	MonthlyInstallmentReduction decimal.Decimal    `json:"monthly_installment_reduction"`
	InterestSavingsAmount       decimal.Decimal    `json:"interest_savings_amount"`
	EffectiveRoiPercentage      decimal.Decimal    `json:"effective_roi_percentage"`
	OriginalSchedule            []InstallmentRow   `json:"original_schedule"`
	SimulatedSchedule           []InstallmentRow   `json:"simulated_schedule"`
}

// ContractAmortizationSimulator implements exact Half-Even decimal mathematics for SAC, Price, and Consórcio.
type ContractAmortizationSimulator struct{}

func NewContractAmortizationSimulator() *ContractAmortizationSimulator {
	return &ContractAmortizationSimulator{}
}

func (s *ContractAmortizationSimulator) Simulate(input ContractSimulationInput) (ContractSimulationOutput, error) {
	if !input.OutstandingBalance.IsPositive() {
		return ContractSimulationOutput{}, ErrInvalidBalance
	}
	if input.RemainingInstallments < 1 {
		return ContractSimulationOutput{}, ErrInvalidInstallments
	}
	if input.ExtraAmortizationAmount.IsNegative() || input.ExtraAmortizationAmount.GreaterThanOrEqual(input.OutstandingBalance) {
		return ContractSimulationOutput{}, ErrInvalidExtraAmount
	}

	startMonth := input.StartCompetenceMonth
	if startMonth == "" {
		startMonth = "2026-04"
	}

	origSchedule := s.buildSchedule(
		input.System,
		input.OutstandingBalance,
		input.RemainingInstallments,
		input.MonthlyInterestRate,
		input.ConsorcioAdminFeeRate,
		input.ConsorcioReadjustmentRate,
		input.MonthlyInsuranceAndFees,
		startMonth,
	)
	origSummary := summarizeSchedule(origSchedule, decimal.Zero)

	newPrincipal := input.OutstandingBalance.Sub(input.ExtraAmortizationAmount).RoundBank(4)
	simTerm := input.RemainingInstallments

	if input.ExtraAmortizationAmount.IsPositive() && input.Strategy == StrategyReduceTerm {
		simTerm = s.computeReducedTerm(
			input.System,
			input.OutstandingBalance,
			newPrincipal,
			input.RemainingInstallments,
			input.MonthlyInterestRate,
		)
	}

	simSchedule := s.buildSchedule(
		input.System,
		newPrincipal,
		simTerm,
		input.MonthlyInterestRate,
		input.ConsorcioAdminFeeRate,
		input.ConsorcioReadjustmentRate,
		input.MonthlyInsuranceAndFees,
		startMonth,
	)
	simSummary := summarizeSchedule(simSchedule, input.ExtraAmortizationAmount)

	installmentsReduced := origSummary.TotalInstallments - simSummary.TotalInstallments
	monthlyReduction := origSummary.FirstInstallmentAmount.Sub(simSummary.FirstInstallmentAmount).RoundBank(4)
	if monthlyReduction.IsNegative() {
		monthlyReduction = decimal.Zero.RoundBank(4)
	}

	// Total savings = (Original Interest + Original Fees) - (Simulated Interest + Simulated Fees)
	origCost := origSummary.TotalInterestPaid.Add(origSummary.TotalFeesPaid)
	simCost := simSummary.TotalInterestPaid.Add(simSummary.TotalFeesPaid)
	interestSavings := origCost.Sub(simCost).RoundBank(4)

	roiPct := decimal.Zero.RoundBank(4)
	if input.ExtraAmortizationAmount.IsPositive() {
		roiPct = interestSavings.
			Mul(decimal.NewFromInt(100)).
			DivRound(input.ExtraAmortizationAmount, 8).
			RoundBank(4)
	}

	return ContractSimulationOutput{
		TenantID:                    input.TenantID,
		ContractID:                  input.ContractID,
		System:                      input.System,
		Strategy:                    input.Strategy,
		OriginalSummary:             origSummary,
		SimulatedSummary:            simSummary,
		InstallmentsReduced:         installmentsReduced,
		MonthlyInstallmentReduction: monthlyReduction,
		InterestSavingsAmount:       interestSavings,
		EffectiveRoiPercentage:      roiPct,
		OriginalSchedule:            origSchedule,
		SimulatedSchedule:           simSchedule,
	}, nil
}

func (s *ContractAmortizationSimulator) computeReducedTerm(
	system AmortizationSystem,
	originalBalance decimal.Decimal,
	newBalance decimal.Decimal,
	originalTerm int,
	monthlyRate decimal.Decimal,
) int {
	switch system {
	case AmortizationSAC, AmortizationConsorcio:
		// Keep constant principal amortization quota from original schedule
		origQuota := originalBalance.DivRound(decimal.NewFromInt(int64(originalTerm)), 8)
		if !origQuota.IsPositive() {
			return originalTerm
		}
		ceilTerm := newBalance.DivRound(origQuota, 8).Ceil().IntPart()
		if ceilTerm < 1 {
			ceilTerm = 1
		}
		if int(ceilTerm) >= originalTerm {
			return originalTerm - 1
		}
		return int(ceilTerm)

	case AmortizationPrice:
		// Maintain original PMT and solve how many installments are needed for newBalance
		origPmt := computePricePmt(originalBalance, monthlyRate, originalTerm)
		balance := newBalance
		count := 0
		oneCent := decimal.RequireFromString("0.0100")
		for balance.GreaterThan(oneCent) && count < originalTerm {
			count++
			interest := balance.Mul(monthlyRate).RoundBank(4)
			principal := origPmt.Sub(interest).RoundBank(4)
			if !principal.IsPositive() {
				return originalTerm
			}
			balance = balance.Sub(principal).RoundBank(4)
		}
		if count < 1 {
			count = 1
		}
		return count
	default:
		return originalTerm
	}
}

func (s *ContractAmortizationSimulator) buildSchedule(
	system AmortizationSystem,
	principalBalance decimal.Decimal,
	term int,
	monthlyRate decimal.Decimal,
	consorcioAdminRate decimal.Decimal,
	consorcioReadjustmentRate decimal.Decimal,
	monthlyFees decimal.Decimal,
	startCompetenceMonth string,
) []InstallmentRow {
	rows := make([]InstallmentRow, 0, term)
	balance := principalBalance.RoundBank(4)
	fees := monthlyFees.RoundBank(4)

	switch system {
	case AmortizationSAC:
		baseQuota := balance.DivRound(decimal.NewFromInt(int64(term)), 8).RoundBank(4)
		for i := 1; i <= term; i++ {
			interest := balance.Mul(monthlyRate).RoundBank(4)
			quota := baseQuota
			if i == term {
				quota = balance
			}
			totalInst := quota.Add(interest).Add(fees).RoundBank(4)
			balance = balance.Sub(quota).RoundBank(4)
			rows = append(rows, InstallmentRow{
				InstallmentNumber:      i,
				DueMonth:               addMonths(startCompetenceMonth, i-1),
				PrincipalAmount:        quota,
				InterestAmount:         interest,
				FeesAmount:             fees,
				TotalInstallmentAmount: totalInst,
				RemainingBalance:       balance,
			})
		}

	case AmortizationPrice:
		pmt := computePricePmt(balance, monthlyRate, term)
		for i := 1; i <= term; i++ {
			interest := balance.Mul(monthlyRate).RoundBank(4)
			principal := pmt.Sub(interest).RoundBank(4)
			if i == term {
				principal = balance
				pmt = principal.Add(interest).RoundBank(4)
			}
			totalInst := pmt.Add(fees).RoundBank(4)
			balance = balance.Sub(principal).RoundBank(4)
			rows = append(rows, InstallmentRow{
				InstallmentNumber:      i,
				DueMonth:               addMonths(startCompetenceMonth, i-1),
				PrincipalAmount:        principal,
				InterestAmount:         interest,
				FeesAmount:             fees,
				TotalInstallmentAmount: totalInst,
				RemainingBalance:       balance,
			})
		}

	case AmortizationConsorcio:
		// Consórcio: No compound interest; admin fee rate is diluted across installments,
		// and annual readjustment (e.g. INCC/IPCA every 12 months) adjusts remaining balance & quota.
		currentBalance := balance
		for i := 1; i <= term; i++ {
			if i > 1 && (i-1)%12 == 0 && consorcioReadjustmentRate.IsPositive() {
				multiplier := decimal.NewFromInt(1).Add(consorcioReadjustmentRate)
				currentBalance = currentBalance.Mul(multiplier).RoundBank(4)
			}
			remainingMonths := term - i + 1
			quota := currentBalance.DivRound(decimal.NewFromInt(int64(remainingMonths)), 8).RoundBank(4)
			if i == term {
				quota = currentBalance
			}
			adminFeeComponent := quota.Mul(consorcioAdminRate).RoundBank(4)
			totalInst := quota.Add(adminFeeComponent).Add(fees).RoundBank(4)
			currentBalance = currentBalance.Sub(quota).RoundBank(4)

			rows = append(rows, InstallmentRow{
				InstallmentNumber:      i,
				DueMonth:               addMonths(startCompetenceMonth, i-1),
				PrincipalAmount:        quota,
				InterestAmount:         adminFeeComponent,
				FeesAmount:             fees,
				TotalInstallmentAmount: totalInst,
				RemainingBalance:       currentBalance,
			})
		}
	}

	return rows
}

func computePricePmt(principal decimal.Decimal, monthlyRate decimal.Decimal, term int) decimal.Decimal {
	if !monthlyRate.IsPositive() {
		return principal.DivRound(decimal.NewFromInt(int64(term)), 8).RoundBank(4)
	}
	one := decimal.NewFromInt(1)
	onePlusR := one.Add(monthlyRate)
	// (1 + i)^n using exact decimal Pow
	factor := onePlusR.Pow(decimal.NewFromInt(int64(term)))
	numerator := principal.Mul(monthlyRate).Mul(factor)
	denominator := factor.Sub(one)
	return numerator.DivRound(denominator, 8).RoundBank(4)
}

func summarizeSchedule(rows []InstallmentRow, extraPrepayment decimal.Decimal) ScheduleSummary {
	if len(rows) == 0 {
		return ScheduleSummary{}
	}
	totalInterest := decimal.Zero
	totalFees := decimal.Zero
	totalPaid := extraPrepayment.RoundBank(4)

	for _, r := range rows {
		totalInterest = totalInterest.Add(r.InterestAmount)
		totalFees = totalFees.Add(r.FeesAmount)
		totalPaid = totalPaid.Add(r.TotalInstallmentAmount)
	}

	return ScheduleSummary{
		TotalInstallments:      len(rows),
		FirstInstallmentAmount: rows[0].TotalInstallmentAmount.RoundBank(4),
		LastInstallmentAmount:  rows[len(rows)-1].TotalInstallmentAmount.RoundBank(4),
		TotalInterestPaid:      totalInterest.RoundBank(4),
		TotalFeesPaid:          totalFees.RoundBank(4),
		TotalPaid:              totalPaid.RoundBank(4),
	}
}

func addMonths(ym string, offset int) string {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		t = time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	}
	target := t.AddDate(0, offset, 0)
	return fmt.Sprintf("%04d-%02d", target.Year(), int(target.Month()))
}
