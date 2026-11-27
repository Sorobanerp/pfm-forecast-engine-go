package grpcadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/port"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/observability"
	pfmv1 "github.com/dantas-pfm/pfm-forecast-engine-go/pkg/contracts/pfmv1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
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

type forecastProtoBridge struct {
	pfmv1.UnimplementedForecastServiceServer
	parent *ForecastGrpcServer
}

func (b *forecastProtoBridge) SimulateContractAmortization(
	ctx context.Context,
	req *pfmv1.SimulateContractRequest,
) (*pfmv1.SimulateContractResponse, error) {
	system := "SAC"
	switch req.GetAmortizationSystem() {
	case pfmv1.AmortizationSystem_AMORTIZATION_SYSTEM_PRICE:
		system = "PRICE"
	case pfmv1.AmortizationSystem_AMORTIZATION_SYSTEM_CONSORCIO:
		system = "CONSORCIO"
	}

	strategy := "REDUCE_TERM"
	if req.GetPrepaymentStrategy() == pfmv1.PrepaymentStrategy_PREPAYMENT_STRATEGY_REDUCE_INSTALLMENT {
		strategy = "REDUCE_INSTALLMENT"
	}

	out, err := b.parent.SimulateContractAmortization(ctx, SimulateContractRequestDTO{
		TenantID:                req.GetTenantId(),
		ContractID:              req.GetContractId(),
		AmortizationSystem:      system,
		PrepaymentStrategy:      strategy,
		OutstandingBalance:      req.GetOutstandingBalance(),
		RemainingInstallments:   int(req.GetRemainingInstallments()),
		MonthlyInterestRate:     req.GetMonthlyInterestRate(),
		ConsorcioAdminFeeRate:   req.GetConsorcioAdminFeeRate(),
		MonthlyInsuranceAndFees: req.GetMonthlyInsuranceAndFees(),
		ExtraAmortizationAmount: req.GetExtraAmortizationAmount(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &pfmv1.SimulateContractResponse{
		TenantId:                    out.TenantID,
		ContractId:                  out.ContractID,
		AmortizationSystem:          req.GetAmortizationSystem(),
		PrepaymentStrategy:          req.GetPrepaymentStrategy(),
		InstallmentsReduced:         int32(out.InstallmentsReduced),
		MonthlyInstallmentReduction: out.MonthlyInstallmentReduction.StringFixed(2),
		InterestSavingsAmount:       out.InterestSavingsAmount.StringFixed(2),
		EffectiveRoiPercentage:      out.EffectiveRoiPercentage.StringFixed(2),
	}, nil
}

func datadogGrpcLoggingInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	level := "DEBUG"
	errStr := ""
	if err != nil {
		level = "ERROR"
		errStr = err.Error()
	}
	envName := os.Getenv("APP_ENV")
	if envName == "" {
		envName = "local"
	}
	rawReq, _ := json.Marshal(req)
	logMap := map[string]any{
		"timestamp":               time.Now().UTC().Format(time.RFC3339Nano),
		"level":                   level,
		"service":                 "pfm-forecast-engine-go",
		"env":                     envName,
		"cloudwatch_logs_enabled": false,
		"dd.trace_id":             fmt.Sprintf("%d", start.UnixNano()),
		"grpc.method":             info.FullMethod,
		"duration_ms":             time.Since(start).Milliseconds(),
		"payload_preview":         observability.MaskSensitivePII(string(rawReq)),
	}
	if errStr != "" {
		logMap["error.stack"] = observability.MaskSensitivePII(errStr)
	}
	if rawLog, mErr := json.Marshal(logMap); mErr == nil {
		_, _ = fmt.Fprintln(os.Stdout, string(rawLog))
	}
	return resp, err
}

func NewForecastGrpcServer(simulateUseCase port.SimulateContractUseCase) *ForecastGrpcServer {
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(datadogGrpcLoggingInterceptor)) // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
	s := &ForecastGrpcServer{
		simulateUseCase: simulateUseCase,
		grpcServer:      grpcSrv,
	}
	pfmv1.RegisterForecastServiceServer(grpcSrv, &forecastProtoBridge{parent: s})
	reflection.Register(grpcSrv)
	return s
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

func (s *ForecastGrpcServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.grpcServer.ServeHTTP(w, r)
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
