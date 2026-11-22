#!/usr/bin/env bash
# ==============================================================================
# PFM PULSE — LOCAL FOREGROUND RUNNER & LIVE DATADOG JSON LOGGER
# Service: pfm-forecast-engine-go (Go 1.27+ | gRPC + HTTP/2 h2c :9091)
# CloudWatch: 100% DISABLED (CLOUDWATCH_LOGS_ENABLED=false)
# ==============================================================================
set -euo pipefail

cd "$(dirname "$0")"
mkdir -p logs

if [[ -f ".env.local" ]]; then
  set -a
  # shellcheck disable=SC1091
  source ".env.local"
  set +a
fi

export APP_ENV="local"
export LOG_LEVEL="debug"
export CLOUDWATCH_LOGS_ENABLED="false"
export AWS_CLOUDWATCH_ENABLED="false"

echo "=============================================================================="
echo " [pfm-forecast-engine-go] Verificando Infraestrutura Base Local..."
echo "=============================================================================="
for pair in "PostgreSQL:5432" "Redpanda_Kafka:9092" "Redis_Valkey:6379" "LocalStack:4566"; do
  name="${pair%%:*}"
  port="${pair##*:}"
  if ! (echo > "/dev/tcp/127.0.0.1/$port") >/dev/null 2>&1; then
    echo " [ERRO] $name nao esta acessivel em localhost:$port!"
    echo "        Execute primeiro: /home/dantas/workspace/pfm-platform/init-local-development.sh"
    exit 1
  fi
  echo " [OK] $name ativo em localhost:$port"
done

echo "=============================================================================="
echo " [pfm-forecast-engine-go] Iniciando em Foreground (gRPC :9091)"
echo " Logs ao vivo no terminal e gravados em: $(pwd)/logs/app-local.log"
echo "=============================================================================="
exec go run ./cmd/server 2>&1 | tee -a ./logs/app-local.log
