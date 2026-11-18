package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	grpcadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/in/grpc"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/in/kafka"
	redisadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/out/redis"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/service"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/observability"
	"go.opentelemetry.io/otel"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv); err != nil {
		stop()
		log.Fatalf("forecast engine server error: %v", err) //nolint:gocritic
	}
}

// resolveGrpcAddr returns the listen address, defaulting the port and prefixing ":" when needed.
func resolveGrpcAddr(getenv func(string) string) string {
	grpcPort := getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "18110"
	}
	if !strings.Contains(grpcPort, ":") {
		grpcPort = ":" + grpcPort
	}
	return grpcPort
}

func newMux(grpcSrv *grpcadapter.ForecastGrpcServer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP","service":"pfm-forecast-engine-go","port":18110}`))
	})
	mux.HandleFunc("POST /pfm.v1.ForecastService/SimulateContractAmortization", func(w http.ResponseWriter, r *http.Request) {
		var dto grpcadapter.SimulateContractRequestDTO
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&dto); err != nil {
			http.Error(w, `{"error":"invalid simulation request"}`, http.StatusBadRequest)
			return
		}
		out, err := grpcSrv.SimulateContractAmortization(r.Context(), dto)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}

// run wires the service and blocks until ctx is cancelled or the HTTP server fails.
func run(ctx context.Context, getenv func(string) string) error {
	_ = observability.InitOtlpConfig(getenv, "pfm-forecast-engine-go")
	_ = otel.Tracer("pfm-forecast-engine-go")

	redisAddr := getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:16379"
	}
	grpcPort := resolveGrpcAddr(getenv)

	cacheAdapter := redisadapter.NewResilientRedisCacheAdapter(redisAddr)
	engineService := service.NewForecastEngineService(cacheAdapter)
	brokers := "localhost:19092"
	if v := strings.TrimSpace(getenv("KAFKA_BROKERS")); v != "" {
		brokers = v
	}
	consumer := kafka.NewLedgerCommittedConsumer(strings.Split(brokers, ","), "pfm-forecast-engine-group", engineService)
	defer func() { _ = consumer.Close() }()

	grpcSrv := grpcadapter.NewForecastGrpcServer(engineService)

	srv := &http.Server{
		Addr:              grpcPort,
		Handler:           newMux(grpcSrv),
		ReadHeaderTimeout: 3 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pfm-forecast-engine-go listening on %s (HTTP/2 & gRPC)", grpcPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		grpcSrv.GracefulStop()
		return err
	case <-ctx.Done():
	}
	_ = srv.Close()
	grpcSrv.GracefulStop()
	return nil
}
