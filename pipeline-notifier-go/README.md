# PipePulse Backend

Backend Go do PipePulse para receber eventos `workflow_run` do GitHub, persistir as entregas antes do aceite HTTP e processá-las de forma assíncrona e determinística.

O plano central, o escopo do produto e a ordem das fases estão em [`RELATORIO_MVP.md`](./RELATORIO_MVP.md). Esse documento prevalece sobre descrições históricas do protótipo.

## Arquitetura atual

```text
Webhook
  -> limite, headers e HMAC SHA-256
  -> associação por endpoint, repositório e workflow
  -> normalização do evento
  -> PostgreSQL: webhook_deliveries
  -> sinal não durável para o worker
  -> processor
  -> transação de estado, falha, outbox e conclusão da entrega
```

O PostgreSQL é a fonte de verdade. O canal em memória possui capacidade um e serve apenas para acordar o worker. Na inicialização e periodicamente, o worker consulta entregas pendentes, portanto um reinício ou sinal perdido não perde trabalho.

O processor aplica as regras de negócio. O repositório PostgreSQL abre a transação e executa as operações solicitadas pelo processor, sem decidir prioridade temporal ou relevância de falha.

## Pré-requisitos

- Go 1.25.13 ou compatível
- PostgreSQL 16 ou compatível
- Docker ou outro runtime de contêiner somente para os testes de integração locais

Exemplo de PostgreSQL local:

```bash
docker run --rm --name pipepulse-postgres \
  -e POSTGRES_USER=pipepulse \
  -e POSTGRES_PASSWORD=pipepulse \
  -e POSTGRES_DB=pipepulse \
  -p 5432:5432 \
  postgres:16-alpine
```

## Configuração

As configurações de banco, criptografia e OAuth abaixo são obrigatórias. As demais possuem os valores padrão indicados.

| Variável | Padrão | Finalidade |
| --- | --- | --- |
| `DATABASE_URL` | sem padrão | conexão PostgreSQL |
| `DATA_ENCRYPTION_KEY` | sem padrão | chave AES-256 em base64 para os segredos armazenados |
| `APP_BASE_URL` | sem padrão | URL HTTPS pública da API |
| `GITHUB_CLIENT_ID` | sem padrão | client ID do GitHub OAuth App |
| `GITHUB_CLIENT_SECRET` | sem padrão | client secret do GitHub OAuth App |
| `MOBILE_OAUTH_REDIRECT_URI` | sem padrão | deep link de retorno ao aplicativo |
| `PORT` | `3000` | porta HTTP |
| `ENVIRONMENT` | `development` | identificação do ambiente |
| `LOG_LEVEL` | `info` | nível dos logs JSON |
| `WORKER_POLL_INTERVAL` | `5s` | intervalo de recuperação das entregas pendentes |
| `RETENTION_INTERVAL` | `24h` | intervalo da limpeza de retenção |
| `SHUTDOWN_TIMEOUT` | `10s` | limite do encerramento gracioso |

As migrations Goose estão embutidas no binário e são aplicadas antes da abertura da API.

## Execução

```bash
export DATABASE_URL='postgres://pipepulse:pipepulse@localhost:5432/pipepulse?sslmode=disable'
export DATA_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export APP_BASE_URL='http://localhost:3000'
export GITHUB_CLIENT_ID='<github-client-id>'
export GITHUB_CLIENT_SECRET='<github-client-secret>'
export MOBILE_OAUTH_REDIRECT_URI='com.pipepulse.app://oauth'
go run ./cmd/api
```

A API fica disponível em `http://localhost:3000` por padrão.

## Endpoint do webhook GitHub

```text
POST /webhooks/github/{endpoint_id}
```

Exemplo:

```bash
curl -i http://localhost:3000/webhooks/github/00000000-0000-0000-0000-000000000000 \
  -H 'Content-Type: application/json' \
  -H 'X-Hub-Signature-256: sha256=<hmac-hex-do-corpo>' \
  -H 'X-GitHub-Delivery: delivery-1' \
  -H 'X-GitHub-Event: workflow_run' \
  --data '{
    "repository": {"id": 10},
    "workflow_run": {
      "id": 30,
      "workflow_id": 20,
      "run_attempt": 1,
      "status": "completed",
      "conclusion": "failure",
      "updated_at": "2026-07-22T12:00:00Z",
      "head_branch": "main",
      "head_sha": "abc123",
      "html_url": "https://github.com/acme/api/actions/runs/30"
    }
  }'
```

O HMAC é calculado com SHA-256 sobre os bytes exatos do corpo e o segredo individual do repositório. Corpos maiores que 1 MiB, assinaturas inválidas, outros tipos de evento e workflows não cadastrados são rejeitados antes da persistência.

## API do produto

O login começa em `POST /v1/auth/github/start`, que retorna `authorization_url` e `exchange_verifier`. O aplicativo deve guardar o verificador localmente antes de abrir a URL. O callback GitHub entrega ao aplicativo um código de troca de uso único, sem o verificador no deep link. `POST /v1/auth/github/exchange` exige `code` e `exchange_verifier` e retorna access token de 15 minutos e refresh token rotativo de 30 dias. Apenas hashes dos tokens são persistidos.

As rotas autenticadas expõem bootstrap, cadastro e consulta de repositórios, rotação do segredo, feed paginado, dispositivo, logout e exclusão da conta. Todas exigem `Authorization: Bearer <access_token>` e aplicam o workspace da sessão. Um recurso de outro workspace responde `404`.

## Testes

Suíte rápida:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Migrations, transações, rollback, recuperação e retenção contra PostgreSQL efêmero:

```bash
PIPEPULSE_INTEGRATION=1 go test ./internal/database ./internal/repository ./internal/handlers -count=1
```

## Estado da implementação

Implementado até a Fase 4:

- pool PostgreSQL com `pgxpool`
- schema completo do MVP em migration Goose embutida
- persistência idempotente por `X-GitHub-Delivery`
- transação atômica de estado, falha, outbox e entrega
- recuperação de entregas pendentes após reinício
- retenção de entregas, falhas Free e resultados de push
- `plan_code=free` e contrato de entitlements
- endpoint individual `POST /webhooks/github/{endpoint_id}`
- segredo criptografado com AES-256-GCM e HMAC SHA-256 em tempo constante
- limite de corpo de 1 MiB e validação dos headers GitHub
- associação obrigatória da entrega com repositório e workflow ativos
- processamento vertical da entrega até estado e feed de falhas
- OAuth GitHub com PKCE S256 e `state` de uso único
- workspace Free automático e sessões opacas rotativas
- bootstrap com entitlements server-side
- API de repositórios e workflow com limite Free transacional
- feed de falhas paginado e isolado por workspace
- registro de um dispositivo ativo, logout e exclusão da conta

Ainda fora do escopo desta fase:

- sender FCM e política de retry
- aplicativo Flutter
