# Arquitetura do PipePulse Backend

O documento normativo do MVP é [`RELATORIO_MVP.md`](./RELATORIO_MVP.md). Este arquivo resume a arquitetura implementada até a Fase 2.

## Fluxo de dados

```text
GitHub webhook
  -> handler Gin
  -> service de normalização
  -> INSERT webhook_deliveries
  -> HTTP 202
  -> sinal de despertar
  -> worker consulta pendências
  -> processor aplica regras
  -> transação PostgreSQL
       pipeline_states
       pipeline_failures
       notification_deliveries
       webhook_deliveries.processed_at
```

O sinal em memória não transporta o evento e não é fonte de verdade. Ele apenas reduz a latência normal. O worker também drena pendências na inicialização e em polling periódico.

## Fronteiras de responsabilidade

| Componente | Responsabilidade |
| --- | --- |
| Handler | adaptar HTTP e retornar o status apropriado |
| Service | normalizar o payload externo em `models.Event` |
| Queue | persistir antes de sinalizar e recuperar pendências |
| Processor | decidir duplicidade semântica, ordem temporal, prioridade e criação de falha |
| Repositório | executar leituras e escritas dentro da transação solicitada |
| Retention worker | remover dados expirados sem apagar entregas pendentes |

O processor não executa SQL e o repositório não escolhe qual evento vence. Essa separação mantém as regras testáveis sem banco e as garantias atômicas testáveis contra PostgreSQL real.

## Transação do processor

Para cada entrega pendente:

1. A entrega é bloqueada.
2. O estado atual do workflow é lido com lock.
3. O processor ignora evento antigo ou de menor prioridade, quando aplicável.
4. Um evento aceito atualiza `pipeline_states`.
5. Uma falha cria no máximo um registro por run e tentativa.
6. A falha cria no máximo uma outbox por dispositivo ativo.
7. A entrega é marcada como processada.
8. Todas as alterações são confirmadas juntas.

Qualquer erro antes do commit desfaz estado, falha, outbox e conclusão da entrega. O polling poderá tentar a entrega novamente.

## Idempotência e ordem

- `webhook_deliveries.delivery_id` é globalmente único.
- Uma falha é única por repositório, workflow run e tentativa.
- Uma notificação é única por falha e dispositivo, mesmo após a retenção remover a falha original.
- Eventos com timestamp anterior ao estado são ignorados.
- Em timestamps iguais, a prioridade é `failed > success > cancelled > running`.
- Runs diferentes com falha geram falhas distintas, inclusive quando o estado anterior já era `failed`.

## Modelo persistente

A migration inicial cria:

- `users`
- `workspaces`
- `oauth_requests`
- `sessions`
- `repositories`
- `monitored_workflows`
- `pipeline_states`
- `pipeline_failures`
- `devices`
- `webhook_deliveries`
- `notification_deliveries`

As relações compostas impedem associar uma sessão a outro workspace, um dispositivo a outro usuário ou um workflow a outro repositório.

## Retenção

- entregas processadas: 7 dias
- falhas do plano Free: 7 dias
- resultados terminais de push: 30 dias
- entregas pendentes: nunca removidas pela rotina de retenção

A referência da outbox para a falha aceita `NULL` após a retenção, enquanto uma chave UUID imutável preserva a idempotência da notificação durante 30 dias.

## Concorrência

O MVP executa um processor por instância. A transação usa isolamento serializável e locks das linhas de entrega e estado. Uma futura execução com vários workers exigirá claim com lease ou exclusão por workflow antes de aumentar o paralelismo.

## Próxima fase

A Fase 3 adicionará o endpoint `POST /webhooks/github/{endpoint_id}`, segredo individual criptografado, HMAC SHA-256 sobre bytes originais, limite de 1 MiB, validação dos headers GitHub e associação obrigatória com repositório e workflow cadastrados.
