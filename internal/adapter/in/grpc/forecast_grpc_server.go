package grpcadapter

import (
	"context"
	"net"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/port"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

type SimulateContractRequestDTO struct {
	TenantID                  string `json:"tenant_id"`
	ContractID                string `json:"contract_id"`
	AmortizationSystem        string `json:"amortization_system"`
	PrepaymentStrategy        string `json:"prepayment_strategy"`
	OutstandingBalance        string `json:"outstanding_balance"`
	RemainingInstallments     int    `json:"remaining_installments"`
	MonthlyInterestRate       string `json:"monthly_interest_rate"`
	ConsorcioAdminFeeRate     string `json:"consorcio_admin_fee_rate"`
	ConsorcioReadjustmentRate string `json:"consorcio_readjustment_rate"`
	MonthlyInsuranceAndFees   string `json:"monthly_insurance_and_fees"`
	ExtraAmortizationAmount   string `json:"extra_amortization_amount"`
	StartCompetenceMonth      string `json:"start_competence_month"`
}

type ForecastGrpcServer struct {
	simulateUseCase port.SimulateContractUseCase
	grpcServer      *grpc.Server
}

func NewForecastGrpcServer(simulateUseCase port.SimulateContractUseCase) *ForecastGrpcServer {
	return &ForecastGrpcServer{
		simulateUseCase: simulateUseCase,
		grpcServer:      grpc.NewServer(),
	}
}

func (s *ForecastGrpcServer) SimulateContractAmortization(
	ctx context.Context,
	req SimulateContractRequestDTO,
) (domain.ContractSimulationOutput, error) {
	balance, err := decimal.NewFromString(req.OutstandingBalance)
	if err != nil {
		return domain.ContractSimulationOutput{}, err
	}
	rate, _ := decimal.NewFromString(defaultZero(req.MonthlyInterestRate))
	adminRate, _ := decimal.NewFromString(defaultZero(req.ConsorcioAdminFeeRate))
	readjRate, _ := decimal.NewFromString(defaultZero(req.ConsorcioReadjustmentRate))
	fees, _ := decimal.NewFromString(defaultZero(req.MonthlyInsuranceAndFees))
	extra, _ := decimal.NewFromString(defaultZero(req.ExtraAmortizationAmount))

	input := domain.ContractSimulationInput{
		TenantID:                  req.TenantID,
		ContractID:                req.ContractID,
		System:                    domain.AmortizationSystem(req.AmortizationSystem),
		Strategy:                  domain.PrepaymentStrategy(req.PrepaymentStrategy),
		OutstandingBalance:        balance,
		RemainingInstallments:     req.RemainingInstallments,
		MonthlyInterestRate:       rate,
		ConsorcioAdminFeeRate:     adminRate,
		ConsorcioReadjustmentRate: readjRate,
		MonthlyInsuranceAndFees:   fees,
		ExtraAmortizationAmount:   extra,
		StartCompetenceMonth:      req.StartCompetenceMonth,
	}
	return s.simulateUseCase.SimulateContractAmortization(ctx, input)
}

func (s *ForecastGrpcServer) Serve(listener net.Listener) error {
	return s.grpcServer.Serve(listener)
}

func (s *ForecastGrpcServer) GracefulStop() {
	s.grpcServer.GracefulStop()
}

func defaultZero(val string) string {
	if val == "" {
		return "0.0000"
	}
	return val
}
