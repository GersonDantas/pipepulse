# Pipeline Notifier

Backend em Go para receber eventos de pipelines via webhook, processar esses eventos de forma assincrona e manter um estado consistente mesmo quando eventos chegam duplicados ou fora de ordem.

Camada HTTP do MVP: `gin`.

## Objetivo

O projeto implementa um MVP de monitoramento de pipelines com foco em consistencia de estado.

Ele recebe eventos externos, coloca esses eventos em uma fila interna e deixa o processor decidir se o estado do pipeline deve ser atualizado e se uma notificacao deve ser disparada.

## Arquitetura

Fluxo principal:

```text
Webhook -> Handler -> Service -> Queue -> Processor -> Repository -> Notification
```

Responsabilidades:

| Camada | Papel |
| --- | --- |
| Handler | Recebe a requisicao HTTP com Gin e valida o payload |
| Service | Converte o webhook externo em evento interno |
| Queue | Desacopla entrada HTTP do processamento |
| Processor | Aplica regras de negocio |
| Repository | Mantem o ultimo estado valido em memoria |
| Notification | Notifica somente apos persistir o novo estado |

## Regras de Negocio

O processor segue esta ordem:

1. Ignorar evento duplicado pelo `EventID`.
2. Ignorar evento antigo pelo `Timestamp`.
3. Resolver conflitos com mesmo timestamp por prioridade de status.
4. Salvar o novo estado.
5. Notificar quando houver mudanca relevante.

Prioridade de status:

```text
failed > success > running
```

## Estrutura

```text
cmd/
  api/
    main.go
internal/
  handlers/
  models/
  processor/
  queue/
  repository/
  services/
```

## Como Rodar

Entre no diretorio do app:

```bash
cd pipeline-notifier-go
```

Rode a API:

```bash
go run ./cmd/api
```

Servidor:

```text
http://localhost:3000
```

## Endpoint

### `POST /webhook/github`

Exemplo de payload:

```json
{
  "workflow_run": {
    "id": 123,
    "conclusion": "failed",
    "updated_at": "2026-01-01T10:00:00Z"
  }
}
```

Quando `conclusion` estiver vazio ou nulo, o evento e tratado como `running`.

Exemplo com `curl`:

```bash
curl -X POST http://localhost:3000/webhook/github \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_run": {
      "id": 123,
      "conclusion": "failed",
      "updated_at": "2026-01-01T10:00:00Z"
    }
  }'
```

## Testes

```bash
go test ./...
```

## Estado Atual do MVP

- API HTTP para webhook do GitHub.
- Roteamento e binding HTTP com Gin.
- Fila in-memory com channel buffered.
- Worker assincrono com goroutine.
- Repository in-memory.
- Idempotencia por `EventID`.
- Controle temporal por `Timestamp`.
- Resolucao de conflito por prioridade de status.

## Limitacoes Conhecidas

- O estado e perdido ao reiniciar o processo.
- O timestamp e comparado como string e deve estar em RFC3339/UTC.
- O MVP usa um worker unico.
- Notificacao ainda e representada por log no console.
- Ainda nao ha persistencia real, fila distribuida ou dashboard.

## Documentacao Tecnica

Leia [ARCHITECTURE.md](./ARCHITECTURE.md) para o guia completo de arquitetura, decisoes e evolucoes futuras.
