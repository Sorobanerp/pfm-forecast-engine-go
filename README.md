# pfm-forecast-engine-go

Motor Quantitativo de Projeção Mensal (Run-rate) e Simulador Imutável de Amortização de Contratos (SAC, Tabela Price e Consórcio) em **Arquitetura Hexagonal (Ports & Adapters)** com `shopspring/decimal` (Half-Even).

## Componentes Principais
- `internal/domain/`:
  - `RunRateForecaster`: Ritmo diário de gastos variáveis (`media_movel_diaria * dias_restantes_no_mes`), confronto contra `renda_liquida_regular`, semáforo (`GREEN`, `YELLOW`, `RED`) e déficit/superávit projetado (RN02 / UC-06).
  - `ContractAmortizationSimulator`: Simulação matemática exata (Half-Even) para **Financiamento SAC**, **Financiamento Price** e **Consórcio** (amortização extraordinária / lance reduzindo prazo vs. reduzindo parcela e economia total de juros/taxas) sem mutação de estado (UC-04).
- `internal/application/port/`: `ForecastUseCase`, `SimulateContractUseCase` e `ReadModelCachePort`.
- `internal/adapter/in/kafka/`: Consumer do tópico `ledger.entry.committed` recalculando o estado mensal do tenant e atualizando o cache Redis em `< 25ms`.
- `internal/adapter/in/grpc/`: Servidor gRPC (HTTP/2) implementando `SimulateContractAmortization`.
- `internal/adapter/out/redis/`: Adaptador Redis/Valkey protegido por **Timeout (500ms)**, **Retry com Backoff** e **Circuit Breaker** com fallback gracioso em memória.
