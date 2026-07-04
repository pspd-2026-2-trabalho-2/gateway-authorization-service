# gateway-authorization-service

Implementação do *API Gateway* e do *Authorization Service* em Go para o projeto da disciplina PSPD. Este repositório fornece a camada de entrada (gateway), autenticação/autorizações (RBAC) e orquestração entre os microsserviços do Hospital Universitário.

## Visão Rápida

Arquitetura resumo (cliente → gateway → serviços gRPC):

```text
Client (REST, JWT) -> API Gateway (HTTP 8080)
  ├─ gRPC -> Patient Data Service (50051) -> PostgreSQL
  ├─ gRPC -> Authorization Service (50052)
  └─ gRPC -> Data Transform Service (50053)
```

## Recursos Principais

- Validação de JWT e middleware de autenticação
- Controle de acesso baseado em papéis (RBAC)
- Orquestração gRPC entre serviços internos
- Rate limiting básico por IP/Token
- Logging e métricas Prometheus (/metrics)

## Estrutura do Repositório

- **auth_service/**: código do Authorization Service ([auth_service/main.go](auth_service/main.go))
- **gateway/**: código do API Gateway ([gateway/main.go](gateway/main.go))
- **proto/**: contratos gRPC e arquivos gerados
- **Dockerfile.auth**, **Dockerfile.gateway**, **docker-compose.yml**: containerização e orquestração local

## Pré-requisitos

- Go 1.26.4 ou superior (para builds locais)
- Docker e Docker Compose (para execução via containers)

## Quickstart (com Docker)

1. Copie o arquivo de ambiente:

```bash
cp .env.example .env
```

2. Suba os containers (build e execução):

```bash
docker compose up -d --build
```

3. Acompanhe logs:

```bash
docker compose logs -f
```

Se preferir executar localmente (sem Docker):

```bash
cd auth_service && go run .
cd gateway && go run .
```

## Tokens de Teste (geração rápida)

Use jwt.io para criar tokens HMAC (HS256) com `secret-key` como assinatura. Exemplos de payloads:

- Médico:

```json
{
  "username": "med.cardoso",
  "role": "MEDICO"
}
```

- Estagiário:

```json
{
  "username": "est.souza",
  "role": "ESTAGIARIO"
}
```

Insira o token no header `Authorization: Bearer <TOKEN>` nas requisições.

## Exemplos de Uso (cURL)

- Médico (acesso FULL esperado):

```bash
curl -i -H "Authorization: Bearer <TOKEN_MEDICO>" "http://localhost:8080/api/patients?patient_id=P000005"
```

- Estagiário (acesso PARTIAL esperado):

```bash
curl -i -H "Authorization: Bearer <TOKEN_ESTAGIARIO>" "http://localhost:8080/api/patients?patient_id=P000001"
```

- Acesso negado (403) quando não houver vínculo:

```bash
curl -i -H "Authorization: Bearer <TOKEN_ESTAGIARIO>" "http://localhost:8080/api/patients?patient_id=P000008"
```

## Métricas e Observabilidade

- API Gateway HTTP: `http://localhost:8080/metrics`
- Authorization Service (Prometheus): `http://localhost:9091/metrics`

Métricas relevantes: `grpc_server_handled_total`, `promhttp_metric_handler_requests_total`, latências e contadores de erro.

## Estrutura de Testes

- Dados iniciais e seeds: veja [patient-data-service/db/seed.sql](../patient-data-service/db/seed.sql) para exemplos de dados usados nos testes.