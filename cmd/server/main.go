package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpcadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/in/grpc"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/in/kafka"
	redisadapter "github.com/dantas-pfm/pfm-forecast-engine-go/internal/adapter/out/redis"
	"github.com/dantas-pfm/pfm-forecast-engine-go/internal/application/service"
	"go.opentelemetry.io/otel"
)

func main() {
	_ = otel.Tracer("pfm-forecast-engine-go")

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = ":9091"
	}

	cacheAdapter := redisadapter.NewResilientRedisCacheAdapter(redisAddr)
	engineService := service.NewForecastEngineService(cacheAdapter)
	consumer := kafka.NewLedgerCommittedConsumer([]string{"localhost:9092"}, "pfm-forecast-engine-group", engineService)
	defer func() { _ = consumer.Close() }()

	grpcSrv := grpcadapter.NewForecastGrpcServer(engineService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP","service":"pfm-forecast-engine-go","port":9091}`))
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

	srv := &http.Server{
		Addr:              grpcPort,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		log.Printf("pfm-forecast-engine-go listening on %s (HTTP/2 & gRPC)", grpcPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("forecast engine server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	_ = srv.Close()
	grpcSrv.GracefulStop()
}
