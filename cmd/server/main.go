package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

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
	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcPort, err)
	}

	go func() {
		log.Printf("pfm-forecast-engine-go gRPC server listening on %s", grpcPort)
		if serveErr := grpcSrv.Serve(lis); serveErr != nil {
			log.Printf("gRPC server stopped: %v", serveErr)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	grpcSrv.GracefulStop()
}
