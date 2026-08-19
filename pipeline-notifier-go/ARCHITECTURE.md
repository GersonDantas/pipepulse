# Arquitetura do PipePulse Backend

O documento normativo do MVP é [`RELATORIO_MVP.md`](./RELATORIO_MVP.md). Este arquivo resume a arquitetura implementada até a Fase 4.

## Fluxo de dados

```text
GitHub webhook
  -> handler Gin
  -> limite de 1 MiB e headers obrigatórios
  -> resolução do endpoint e segredo criptografado
  -> HMAC SHA-256 sobre o corpo bruto
  -> normalização e associação do workflow
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
| Handler | limitar e preservar o corpo bruto, validar headers e retornar o status apropriado |
| Service | resolver o endpoint, validar HMAC, normalizar e associar o evento |
| Queue | persistir antes de sinalizar e recuperar pendências |
| Processor | decidir duplicidade semântica, ordem temporal, prioridade e criação de falha |
| Repositório | executar leituras e escritas dentro da transação solicitada |
| Retention worker | remover dados expirados sem apagar entregas pendentes |
| Auth service | executar PKCE, código de troca e emissão/rotação de tokens opacos |
| Product service | validar entradas, aplicar entitlements e emitir segredos de uso único |

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

## Autenticação e isolamento

O backend cria `state` e verifier PKCE aleatórios, persiste apenas o hash do `state` e mantém o verifier criptografado. O callback consome o `state` uma única vez, usa o token GitHub somente para consultar `/user` e entrega ao deep link móvel outro código de uso único. O consumo desse código, o upsert de usuário/workspace e a criação da sessão ocorrem na mesma transação.

Access e refresh tokens são valores opacos aleatórios; somente hashes SHA-256 ficam no banco. O access token expira em 15 minutos. O refresh expira em 30 dias e sua rotação substitui atomicamente ambos os hashes. Logout revoga a sessão e a exclusão do usuário remove por cascata workspace, sessões, dispositivo e endpoints.

Todas as rotas de produto usam o workspace obtido da sessão. Consultas e mutações incluem esse workspace no SQL; IDs pertencentes a outro workspace são indistinguíveis de IDs inexistentes e retornam `404`.

## API do produto

O limite Free de três repositórios é verificado dentro de uma transação que bloqueia o workspace, evitando ultrapassagem por requisições concorrentes. Criação e rotação retornam o segredo do webhook somente na resposta atual. Listagens nunca leem nem devolvem o segredo criptografado.

O feed usa cursor opaco composto por timestamp e UUID, ordenado de forma determinística. O registro de dispositivo substitui o dispositivo ativo anterior do usuário e rejeita um token já associado a outra conta.

## Fronteira do webhook

Cada repositório possui um `endpoint_id` público e um segredo próprio armazenado com AES-256-GCM. A API aceita somente `workflow_run` com `X-GitHub-Delivery`, valida `X-Hub-Signature-256` em tempo constante e confirma que os IDs GitHub do payload pertencem ao endpoint e a um workflow ativo. Somente depois dessas verificações a entrega associada é gravada; o `202` confirma essa gravação, não o processamento assíncrono.

## Retenção

- entregas processadas: 7 dias
- falhas do plano Free: 7 dias
- resultados terminais de push: 30 dias
- entregas pendentes: nunca removidas pela rotina de retenção

A referência da outbox para a falha aceita `NULL` após a retenção, enquanto uma chave determinística formada por repositório, workflow run e tentativa preserva a idempotência da notificação durante 30 dias.

## Concorrência

O MVP executa um processor por instância. A transação usa isolamento serializável e locks das linhas de entrega e estado. Uma futura execução com vários workers exigirá claim com lease ou exclusão por workflow antes de aumentar o paralelismo.

## Próxima fase

A Fase 5 adicionará sender FCM, retry, idempotência e tratamento de tokens inválidos sobre a outbox já persistida.
