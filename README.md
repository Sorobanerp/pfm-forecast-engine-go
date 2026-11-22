# PFM Forecast Engine Service — Golang 1.24 (Projeção & Alertas)

[![Gitflow](https://img.shields.io/badge/Gitflow-develop%20%7C%20main-indigo)](CONTRIBUTING.md)
[![SemVer](https://img.shields.io/badge/SemVer-2.0.0-blue)](CHANGELOG.md)
[![Docs](https://img.shields.io/badge/PFM%20Docs-V3.0-8CBA51)](https://sorobanerp.github.io/pfm-docs/)

---

## 1. Visão Geral & Papel na Arquitetura

Motor preditivo de fluxo de caixa em **Golang 1.24+**: simulação de liquidez futura (30, 60 e 90 dias), cálculo de *burn rate* de orçamento (**RN01/RN02**) e emissão de alertas em 80% e 100% do teto.

- **Repositório:** `Sorobanerp/pfm-forecast-engine-go`
- **Vínculo com `pfm-docs`:** `ADR-001, ADR-003, ADR-005 | US-05, UC-05, UC-06`

```mermaid
flowchart LR
    LEDGER["pfm-core-ledger-java"] -->|gRPC / Kafka| FORECAST["pfm-forecast-engine-go (Go 1.24)"]
    FORECAST -->|Alertas 80% / 100%| KAFKA[["Upstash Kafka"]]
```

---

## 2. Pré-requisitos & Variáveis de Ambiente

### Pré-requisitos
- Go `1.24+`
- `golangci-lint` e `gosec`

### Variáveis de Ambiente (Sem Segredos Hardcoded)
| Variável | Padrão Local | Descrição |
| :--- | :---: | :--- |
| `GRPC_PORT` | `18110` | Porta gRPC & Health HTTP do Forecast Engine |
| `KAFKA_BROKERS` | `localhost:19092` | Endereço dos brokers Kafka |

> **Segurança (Zero-Trust):** Nunca commite tokens, senhas, chaves AWS ou strings de conexão com credenciais reais. Utilize `.env` local (ignorado no `.gitignore`) ou AWS Secrets Manager / GitHub OIDC.

---

## 3. Como Executar Localmente

```bash
export GRPC_PORT=18110
go run ./cmd/...
# Healthcheck: curl -f http://localhost:18110/health
```

---

## 4. Como Testar e Validar

```bash
go test -v -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

---

## 5. Como Provisionar / Validar na Nuvem

1. Fluxo Gitflow: `feat/*` $\rightarrow$ `develop` $\rightarrow$ `release/vX.Y.Z` $\rightarrow$ `main`.
2. Implantação contínua via imagem OCI Distroless Arm64 no ECS Fargate Spot (`pfm-forecast-engine-infra`).

Consulte [CONTRIBUTING.md](CONTRIBUTING.md) para regras de Conventional Commits e [CHANGELOG.md](CHANGELOG.md) para histórico de versões.

---

## Execução e Debug Local (CLI, VS Code e IntelliJ IDEA)

1. **Bootstrap Completo do Ambiente Local (Zero Segredos no Git):**
   Na raiz do workspace (`/home/dantas/workspace/pfm-platform/`), execute:
   ```bash
   ./init-local-development.sh
   ```
   O script gera automaticamente o arquivo `.env.local` (ignorado pelo `.gitignore`, tendo `.env.example` como template seguro), sobe o `pfm-shared-infra/docker-compose.local.yml` (PostgreSQL 16 `:5432`, Kafka/Redpanda `:9092`, Console Kafka `:8081`, Redis/Valkey `:6379`, LocalStack S3+KMS `:4566`), cria os tópicos Kafka e provisiona a chave KMS `alias/pfm-master-key`.

2. **Execução com Perfil `local` & Debug na IDE:**
   - **`pfm-core-ledger-java` (IntelliJ IDEA / Java 25 LTS):** Abra o projeto no IntelliJ IDEA e selecione a Run Configuration compartilhada **`CoreLedgerApplication (local)`** (`.run/CoreLedgerApplication (local).run.xml`, `SPRING_PROFILES_ACTIVE=local`, HTTP `:8082`, gRPC `:9090` com Server Reflection habilitado) ou rode `SPRING_PROFILES_ACTIVE=local ./mvnw spring-boot:run`.
   - **`pfm-ingestion-bff-go` (VS Code / Go):** Pressione **`F5`** (`Run PFM Ingestion BFF (APP_ENV=local :8080)` em `.vscode/launch.json`) e acesse o **Swagger UI (OpenAPI 3.1)** em `http://localhost:8080/swagger/index.html` ou use `requests-local.http`.
   - **`pfm-forecast-engine-go` (VS Code / Go):** Pressione **`F5`** (`Run PFM Forecast Engine (APP_ENV=local :9091 gRPC)` em `.vscode/launch.json`) e teste via `grpcurl -plaintext localhost:9091 list`.
   - **`pfm-ai-worker-python` (VS Code / Python 3.12):** Pressione **`F5`** (`Run PFM AI Worker Python (APP_ENV=local)` em `.vscode/launch.json`), operando em modo determinístico local quando `GEMINI_API_KEY` estiver vazia.
