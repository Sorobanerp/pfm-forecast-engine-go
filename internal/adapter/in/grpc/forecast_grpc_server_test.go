package grpcadapter

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/domain"
	"github.com/shopspring/decimal"
)

type fakeUseCase struct {
	got domain.ContractSimulationInput
	out domain.ContractSimulationOutput
	err error
}

func (f *fakeUseCase) SimulateContractAmortization(_ context.Context, in domain.ContractSimulationInput) (domain.ContractSimulationOutput, error) {
	f.got = in
	return f.out, f.err
}

func TestSimulateContractAmortization_MapsRequest(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name    string
		req     SimulateContractRequestDTO
		ucErr   error
		wantErr error
		badNum  bool
		check   func(t *testing.T, in domain.ContractSimulationInput)
	}{
		{
			name: "full mapping",
			req: SimulateContractRequestDTO{
				TenantID: "t", ContractID: "c", AmortizationSystem: "PRICE", PrepaymentStrategy: "REDUCE_TERM",
				OutstandingBalance: "1000.50", RemainingInstallments: 10, MonthlyInterestRate: "0.0085",
				ConsorcioAdminFeeRate: "0.16", ConsorcioReadjustmentRate: "0.05", MonthlyInsuranceAndFees: "12.34",
				ExtraAmortizationAmount: "100", StartCompetenceMonth: "2026-05",
			},
			check: func(t *testing.T, in domain.ContractSimulationInput) {
				if in.TenantID != "t" || in.ContractID != "c" || in.System != domain.AmortizationPrice || in.Strategy != domain.StrategyReduceTerm {
					t.Fatalf("bad identity mapping: %+v", in)
				}
				if !in.OutstandingBalance.Equal(decimal.RequireFromString("1000.50")) ||
					!in.MonthlyInterestRate.Equal(decimal.RequireFromString("0.0085")) ||
					!in.ConsorcioAdminFeeRate.Equal(decimal.RequireFromString("0.16")) ||
					!in.ConsorcioReadjustmentRate.Equal(decimal.RequireFromString("0.05")) ||
					!in.MonthlyInsuranceAndFees.Equal(decimal.RequireFromString("12.34")) ||
					!in.ExtraAmortizationAmount.Equal(decimal.NewFromInt(100)) {
					t.Fatalf("bad decimal mapping: %+v", in)
				}
				if in.RemainingInstallments != 10 || in.StartCompetenceMonth != "2026-05" {
					t.Fatalf("bad scalar mapping: %+v", in)
				}
			},
		},
		{
			name: "optional fields default to zero",
			req:  SimulateContractRequestDTO{OutstandingBalance: "10", RemainingInstallments: 1},
			check: func(t *testing.T, in domain.ContractSimulationInput) {
				if !in.MonthlyInterestRate.IsZero() || !in.ConsorcioAdminFeeRate.IsZero() || !in.ConsorcioReadjustmentRate.IsZero() ||
					!in.MonthlyInsuranceAndFees.IsZero() || !in.ExtraAmortizationAmount.IsZero() {
					t.Fatalf("expected zero defaults: %+v", in)
				}
			},
		},
		{name: "invalid balance", req: SimulateContractRequestDTO{OutstandingBalance: "abc"}, badNum: true},
		{name: "use case error propagated", req: SimulateContractRequestDTO{OutstandingBalance: "10"}, ucErr: boom, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uc := &fakeUseCase{err: tt.ucErr}
			srv := NewForecastGrpcServer(uc)
			_, err := srv.SimulateContractAmortization(context.Background(), tt.req)
			switch {
			case tt.badNum:
				if err == nil {
					t.Fatal("expected parse error")
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got %v want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				tt.check(t, uc.got)
			}
		})
	}
}

func TestDefaultZero(t *testing.T) {
	t.Parallel()
	if got := defaultZero(""); got != "0.0000" {
		t.Fatalf("got %q", got)
	}
	if got := defaultZero("5"); got != "5" {
		t.Fatalf("got %q", got)
	}
}

func TestServeAndGracefulStop(t *testing.T) {
	t.Parallel()
	srv := NewForecastGrpcServer(&fakeUseCase{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	time.Sleep(50 * time.Millisecond)
	srv.GracefulStop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after GracefulStop")
	}
}
