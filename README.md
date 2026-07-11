# gateway-authorization-service

Implementação do *API Gateway* e do *Authorization Service* em Go para o projeto da disciplina PSPD. Este repositório fornece a camada de entrada (gateway), autenticação/autorizações (RBAC), rate limiting, logging e orquestração entre os microsserviços do Hospital Universitário.

## Visão Rápida

Arquitetura resumo (cliente → gateway → serviços gRPC):

```text
Client (REST, JWT) -> API Gateway (HTTP 8080)
  ├─ gRPC -> Patient Data Service (50051) -> PostgreSQL
  ├─ gRPC -> Authorization Service (50052)
  └─ gRPC -> Data Transform Service (50053)
```

## Recursos Principais

- Validação de JWT e autorização centralizada via Authorization Service
- Controle de acesso baseado em papéis (RBAC)
- Orquestração gRPC entre serviços internos
- Rotas REST para pacientes, coortes e vistas por perfil
- Rate limiting básico e logging por requisição
- Métricas Prometheus em `/metrics`

## Estrutura do Repositório

- **auth_service/**: código do Authorization Service ([auth_service/main.go](auth_service/main.go))
- **gateway/**: código do API Gateway ([gateway/main.go](gateway/main.go), [gateway/auth.go](gateway/auth.go), [gateway/handlers.go](gateway/handlers.go), [gateway/middleware.go](gateway/middleware.go))
- **proto/**: contratos gRPC e arquivos gerados
- **Dockerfile.auth**, **Dockerfile.gateway**, **docker-compose.yml**: containerização e orquestração local

## Pré-requisitos

- Go 1.26.4 ou superior (para builds locais)
- Docker e Docker Compose (para execução via containers)

## Configuração

O gateway lê as seguintes variáveis de ambiente:

- `GATEWAY_PORT`: porta HTTP do gateway, padrão `8080`
- `CORS_ALLOWED_ORIGIN`: origin do frontend liberado pelo middleware de CORS, padrão `http://localhost:5173`
- `KEYCLOAK_URL`: URL base do Keycloak, padrão `https://kiriland.unb.br/keycloak`
- `KEYCLOAK_REALM`: realm do Keycloak, padrão `grupo03`
- `AUTH_SERVICE_TARGET`: endereço gRPC do Authorization Service, padrão `localhost:50052`
- `PATIENT_DATA_TARGET`: endereço gRPC do Patient Data Service, padrão `localhost:50051`
- `DATA_TRANSFORM_TARGET`: endereço gRPC do Data Transform Service, padrão `localhost:50053`

O gateway não carrega `.env` automaticamente (sem `godotenv`) — exporte as variáveis no shell ou use `docker compose`/`env $(cat .env | xargs)` antes de rodar `go run .`.

O token (`Authorization: Bearer <TOKEN>`) precisa ser um access_token **RS256** emitido pelo Keycloak configurado acima: o gateway valida a assinatura via JWKS (`{KEYCLOAK_URL}/realms/{KEYCLOAK_REALM}/protocol/openid-connect/certs`), o `iss` e o algoritmo, e extrai `username` de `preferred_username` e `role` da primeira role em `realm_access.roles` que seja `MEDICO`, `ESTAGIARIO` ou `PESQUISADOR`.

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

O gateway expõe estas rotas principais:

- `GET /api/patients/{id}`: paciente com transformação FHIR conforme o nível de acesso
- `GET /api/patients/{id}/summary`: resumo clínico do paciente
- `GET /api/patients/{id}/history`: histórico clínico do paciente
- `GET /api/cohorts/{condition}/statistics`: estatísticas agregadas da coorte
- `GET /api/cohorts/{condition}/exams`: exames da coorte com anonimização/transformação
- `GET /api/me/patients`: pacientes de médicos
- `GET /api/me/supervised-patients`: pacientes supervisionados por estagiários
- `GET /api/me/projects`: projetos de pesquisadores
- `GET /metrics`: métricas do Prometheus

## Tokens de Teste (geração rápida)

Os tokens já não são gerados manualmente (jwt.io/HS256) — eles vêm do Keycloak real via password grant:

```bash
curl -X POST "https://kiriland.unb.br/keycloak/realms/grupo03/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=pseudopep-frontend" \
  -d "username=med.cardoso" \
  -d "password=PseudoPEP2026!" | jq -r .access_token
```

Usuários de teste disponíveis: `med.*` (role `MEDICO`), `est.*` (role `ESTAGIARIO`), `pes.*` (role `PESQUISADOR`), senha padrão `PseudoPEP2026!`.

Insira o token no header `Authorization: Bearer <TOKEN>` nas requisições.

## Exemplos de Uso (cURL)

- Médico consultando Dashboard (acesso FULL esperado):

```bash
curl -i -H "Authorization: Bearer <TOKEN_MEDICO>" "http://localhost:8080/api/me/patients"
```

- Médico consultando Resumo Clínico de um paciente::

```bash
curl -i -H "Authorization: Bearer <TOKEN_MEDICO>" "http://localhost:8080/api/patients/P000005/summary"
```

- Estagiário (acesso PARTIAL esperado):

```bash
curl -i -H "Authorization: Bearer <TOKEN_ESTAGIARIO>" "http://localhost:8080/api/patients/P000001"
```

- Acesso negado (403) quando não houver vínculo:

```bash
curl -i -H "Authorization: Bearer <TOKEN_ESTAGIARIO>" "http://localhost:8080/api/patients/P000008"
```

- Coorte com estatísticas:

```bash
curl -i -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/cohorts/diabetes/statistics"
```

## Métricas e Observabilidade

- API Gateway HTTP: `http://localhost:8080/metrics`
- Authorization Service (Prometheus): `http://localhost:9091/metrics`

Métricas relevantes: `grpc_server_handled_total`, `promhttp_metric_handler_requests_total`, latências e contadores de erro. O gateway também aplica rate limiting básico por requisição para evitar sobrecarga.

## Estrutura de Testes

- Dados iniciais e seeds: veja [patient-data-service/db/seed.sql](../patient-data-service/db/seed.sql) para exemplos de dados usados nos testes.