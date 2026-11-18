package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	grpcadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/in/grpc"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/service"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveGrpcAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, ":18110"},
		{"bare port", map[string]string{"GRPC_PORT": "9000"}, ":9000"},
		{"host and port", map[string]string{"GRPC_PORT": "127.0.0.1:9000"}, "127.0.0.1:9000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveGrpcAddr(envMap(tt.env)); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestMuxRoutes(t *testing.T) {
	t.Parallel()
	mux := newMux(grpcadapter.NewForecastGrpcServer(service.NewForecastEngineService(nil)))
	valid := `{"tenant_id":"t","contract_id":"c","amortization_system":"SAC","outstanding_balance":"1200.00","remaining_installments":12,"monthly_interest_rate":"0.01"}`
	tests := []struct {
		name, method, path, body string
		wantStatus               int
		wantContains             string
	}{
		{"health", http.MethodGet, "/health", "", http.StatusOK, `"status":"UP"`},
		{"simulate ok", http.MethodPost, "/pfm.v1.ForecastService/SimulateContractAmortization", valid, http.StatusOK, `"installments_reduced":0`},
		{"bad json", http.MethodPost, "/pfm.v1.ForecastService/SimulateContractAmortization", "{", http.StatusBadRequest, "invalid simulation request"},
		{"domain error", http.MethodPost, "/pfm.v1.ForecastService/SimulateContractAmortization", `{"outstanding_balance":"10","remaining_installments":0}`, http.StatusUnprocessableEntity, "remaining installments"},
		{"wrong method", http.MethodGet, "/pfm.v1.ForecastService/SimulateContractAmortization", "", http.StatusMethodNotAllowed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d want %d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantContains) {
				t.Fatalf("body %q missing %q", rec.Body.String(), tt.wantContains)
			}
		})
	}
}

func TestRun_ShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, envMap(map[string]string{"GRPC_PORT": "127.0.0.1:0", "KAFKA_BROKERS": " k1:9092,k2:9092 ", "REDIS_ADDR": ""}))
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

func TestRun_ReturnsListenError(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), envMap(map[string]string{"GRPC_PORT": "127.0.0.1:notaport", "REDIS_ADDR": "x"}))
	if err == nil {
		t.Fatal("expected listen error")
	}
}
