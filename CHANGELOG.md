# Changelog — `pfm-forecast-engine-go`

Todas as alterações notáveis neste repositório são documentadas neste arquivo seguindo o padrão [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e versionamento semântico [SemVer 2.0.0](https://semver.org/lang/pt-BR/).

---

## [Unreleased]

### Added
- Compatibilidade do adaptador de cache com **Upstash Redis / Valkey Serverless (`TLS` + `RESP3`)** via `NewUpstashRedisCacheAdapter` e `NewRedisCacheAdapterFromEnv`.
- Validação de quotas por plano do tenant (`ValidateForecastQuota` / `RecalculateMonthlyForecastForPlan`: `FREE` bloqueado para projeção preditiva; `INDIVIDUAL_PRO` e `FAMILY_PLUS` até 90 dias).
- Padronização de governança DevEx (`README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`) e workflow de validação de **Conventional Commits** em Pull Requests para `develop` e `main`.

---

## [0.2.0] - 2026-11-20

### Added
- Motor de projeção de fluxo de caixa (30/60/90d) e disparo de alertas de limiar orçamentário.

### Changed
- Alinhamento com a arquitetura **PFM Core Banking V3.0 (Production-Ready FinOps & Multi-Cloud)**.

### Security
- Aplicação estrita de políticas Zero-Trust (OIDC sem chaves estáticas, Trivy/Semgrep/Gitleaks gates e isolamento por tenant).

### Infrastructure
- Instrumentação OpenTelemetry Go SDK e cobertura de testes $\ge 95\%$.

---

## [0.1.0] - 2026-11-15

### Added
- Estrutura inicial do repositório `pfm-forecast-engine-go`, pipelines GitHub Actions e integração ao ecossistema Gitflow `Sorobanerp`.
