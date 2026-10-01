# Fase 5: FCM, outbox e retry

## Objetivo

Uma falha já persistida pelo processor passa a ser enviada pelo sender Firebase HTTP v1. A entrega permanece durável antes do HTTP, com tentativas limitadas, recuperação após reinício e conclusão protegida contra workers obsoletos.

Branch: `feat/mvp-fcm`, criada de `main` em `d2a2ff7`. A Fase 4 consta como aprovada no plano central. A implementação local está pronta para revisão; a publicação do PR depende da confirmação de escrita externa. A fase não está aprovada e a Fase 6 não foi iniciada.

## Escopo incluído

- Interface `Sender` e fake em testes.
- Sender Firebase HTTP v1 com autenticação de conta de serviço e cache/renovação OAuth.
- Worker separado, polling no startup e durante a execução da API.
- Claim persistido antes do envio; reserva durável com os intervalos de retry.
- Finalização com fencing por ID, dispositivo, status e tentativa.
- Retries de 1 minuto, 5 minutos, 30 minutos e 2 horas, até cinco tentativas.
- Respeito a `Retry-After` quando maior que o intervalo local.
- Desativação de `UNREGISTERED` e abandono atômico das notificações do dispositivo.
- Rechecagem de elegibilidade, cancelamento de OAuth/HTTP e encerramento do worker.
- Payload genérico sem nomes, branches, SHA ou URLs na tela bloqueada.

## Fora do escopo

- Aplicativo Flutter, configuração do projeto Firebase e concessão de permissões.
- Push real ou recebimento em aparelho físico nesta execução.
- Deploy, Dockerfile de produção, billing, GitLab e novas rotas HTTP.
- Garantia de exactly-once no provedor remoto e no aparelho. O aceite correspondente permanece pendente.

## Commits

| Hash ou referência | Mensagem | Responsabilidade |
| --- | --- | --- |
| `4852bb0` | `feat(notifications): send durable FCM failure alerts` | sender, worker, persistência, composição, configuração e testes |
| `feat/mvp-fcm` após o commit documental | `docs: document FCM phase validation and acceptance` | README, registro central e este relatório |

O hash exato do commit documental deve ser incluído no corpo do PR ao publicar. O histórico completo pode ser verificado com `git log --oneline origin/main..feat/mvp-fcm`.

## Mudanças por comportamento

1. O processor continua criando estado, falha e outbox na mesma transação, sem fazer HTTP.
2. O sender reserva uma entrega vencida com `FOR UPDATE SKIP LOCKED`. O claim incrementa a tentativa e confirma a transação antes do HTTP.
3. A tentativa possui deadline total de 10 segundos, iniciado antes do claim. A reserva dura pelo menos um minuto e preserva os intervalos de retry também após queda do processo.
4. `sent` representa aceitação pelo FCM. Erros transitórios resultam em `failed`; erros permanentes ou a quinta tentativa resultam em `abandoned`.
5. Um `sending` abandonado por queda é retomado depois da reserva. A quinta tentativa expirada é abandonada, sem uma sexta chamada.
6. Uma conclusão antiga é rejeitada antes de qualquer efeito sobre o dispositivo. Token inválido desativa somente o dispositivo da tentativa e abandona suas entregas pendentes na mesma transação.
7. Um dispositivo inativo ou uma falha removida não gera novo envio. A exclusão pode tornar a outbox órfã; o próximo polling a abandona.
8. As credenciais da conta de serviço ficam somente no backend. O endpoint OAuth é fixado no Google; redirects não são seguidos. Erros e logs contêm códigos controlados, sem resposta bruta do provedor.
9. O payload inclui `notification_id`, `failure_id` e `repository_id`, com identificadores estáveis para integração com o aplicativo futuro.

## Arquivos principais alterados

- `internal/notifications/worker.go`: deadline, polling, envio e decisão de retry.
- `internal/notifications/firebase.go`: credenciais, JWT OAuth, payload HTTP v1 e classificação de erros.
- `internal/repository/notifications.go`: claim, recuperação, elegibilidade e finalização transacional.
- Testes nos mesmos pacotes: sender fake, transporte HTTP controlado e PostgreSQL efêmero.
- `internal/config/config.go` e `cmd/api/main.go`: credenciais obrigatórias, composição e shutdown.
- `go.mod`/`go.sum`: dependência direta `golang.org/x/oauth2 v0.36.0`.

## APIs, migrations e configurações

Nenhuma rota ou migration nova. São reutilizados `devices` e `notification_deliveries`, incluindo o índice de unicidade por falha/dispositivo e os estados `pending`, `sending`, `sent`, `failed` e `abandoned`.

Nova configuração obrigatória: `FCM_CREDENTIALS_JSON_B64`, contendo JSON da conta de serviço Firebase em base64. A ausência, base64 inválido, JSON inválido ou chave RSA inválida impedem a inicialização. O `token_uri`, quando informado, deve ser `https://oauth2.googleapis.com/token`. A conta deve pertencer ao projeto Firebase usado pelo aparelho e ter permissão para enviar mensagens FCM.

`WORKER_POLL_INTERVAL` também governa o sender, sem nova opção. `DATA_ENCRYPTION_KEY` continua criptografando os tokens. Não imprimir credenciais nem tokens em logs ou anexos de revisão.

## Evidências TDD

### Red

| Ciclo | Teste e falha observada |
| --- | --- |
| Worker e política de retry | `TestWorker...`: símbolos `NewWorker`, `Notification` e `NotificationResult` ausentes antes da implementação |
| Claim e recuperação | `TestNotificationOutboxClaimAndRecovery`: `ClaimNotification` e `FinishNotification` ausentes |
| Firebase HTTP v1 | `TestFirebase...`: tipo `Firebase` ausente, após instalar a dependência necessária |
| Configuração obrigatória | `TestLoadRequiresFCMCredentials`: `error = <nil>` para credenciais ausentes, base64 inválido e JSON inválido |
| Erro APNs | `third_party_auth_error`: retornava `fcm_auth_failed` transitório em vez de erro permanente |
| Deadline OAuth | `TestFirebaseOAuthSharesAttemptDeadline`: `OAuth ignored the attempt deadline`, demorando 10,13 segundos para um contexto de 20 milissegundos |
| Queda durante retry | `crash_recovery_preserves_retry_intervals`: `attempt 2 lease delay = 1m0s, want 5m0s` |

Os primeiros ciclos tiveram falha de compilação pela ausência das interfaces. As falhas de cache, rede e portas locais ocorridas durante a preparação não foram consideradas Red. Os ciclos posteriores provaram diferenças de comportamento com asserções executadas.

### Green

Todos os testes acima passaram após as respectivas implementações mínimas. O deadline OAuth foi propagado explicitamente no transporte, pois `oauth2/jwt` usa `PostForm` sem anexar o contexto da tentativa. O teste cancelado passou a concluir em aproximadamente 20 milissegundos.

O teste vertical demonstrou processor, outbox, sender fake e conclusão persistida. Redelivery do mesmo run não cria outra notificação; outro run cria uma entrega distinta. Reiniciar o worker não reenvia resultados `sent`.

### Refactor

Tipos compartilhados ficaram no repositório; a decisão de retry ficou no worker, e o HTTP/JWT ficou no adaptador Firebase. A criptografia existente foi reutilizada. O código foi formatado e os pacotes afetados e a suíte foram reexecutados. Nenhum refactor de código anterior foi necessário.

## Testes automatizados

Executados no checkout local em 2026-10-01:

| Comando ou verificação | Resultado |
| --- | --- |
| `go test ./...` | verde |
| `go test -race ./...` | verde |
| `go vet ./...` | verde |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...` | exit 0; nenhum símbolo vulnerável alcançado; quatro vulnerabilidades em módulos requeridos sem chamadas pelo projeto |
| `PIPEPULSE_INTEGRATION=1 go test -race ./internal/database ./internal/repository ./internal/handlers -count=1` | verde, PostgreSQL 16 efêmero |
| `PIPEPULSE_INTEGRATION=1 go test -race ./internal/repository -run TestNotification -count=1` | verde após a correção final dos intervalos de recuperação |
| `go build ./cmd/api` | verde |
| Smoke do binário com PostgreSQL efêmero e conta de serviço sintética | migrations aplicadas, live/ready `200`, SIGTERM com exit `0`, sem push enviado |
| `git diff --check` | verde |

Os testes de integração convencionais exigem Docker e `PIPEPULSE_INTEGRATION=1`; sem essa variável são ignorados. Para a validação local, o cache Go foi direcionado a uma pasta temporária e foi concedido acesso a portas locais e Docker. Isso não muda a configuração do produto.

O Dockerfile e o smoke da imagem de produção pertencem à Fase 7. Nesta fase foi validado o binário contra um banco descartável.

## Roteiro para revisão e testes do usuário

### Pré-requisitos

- Go compatível com `go.mod`, Docker e PostgreSQL de teste.
- Para o aceite remoto: projeto Firebase com FCM HTTP v1 habilitado, conta de serviço e token de um aparelho de teste do mesmo projeto.
- As configurações obrigatórias do README e da Fase 4, incluindo OAuth GitHub.
- Sessão autenticada, um repositório/workflow de teste cadastrado e seu segredo de webhook.

O aplicativo PipePulse ainda será criado na Fase 6. Para validar recebimento nesta fase, usar um cliente Firebase de teste sob controle do proprietário, com permissão de notificações. Não usar dispositivos ou repositórios de produção para o roteiro.

### Dados de teste

- IDs reais do repositório e workflow cadastrados.
- Run e tentativa de teste; `updated_at` atual e superior ao estado anterior.
- Dois IDs de entrega diferentes para o mesmo run e um terceiro para outro run.
- Token FCM real apenas no ambiente de teste. Um texto aleatório pode retornar `INVALID_ARGUMENT` e não serve como prova de `UNREGISTERED`.

### Passos

1. Executar os comandos da tabela de testes. Confirmar que a integração executou, sem `SKIP`.
2. Iniciar a API com as credenciais do ambiente Firebase de teste. Consultar `/health/live` e `/health/ready`.
3. Registrar o dispositivo com `PUT /v1/device`, bearer token e corpo `{"token":"<token-fcm-de-teste>","platform":"android"}`. Confirmar resposta `204`.
4. Criar `event.json` com os IDs cadastrados e um payload `workflow_run` concluído com `conclusion: failure` e timestamp atual. Enviar para o endpoint do repositório com HMAC correto.
5. Consultar estado, feed e a consulta SQL abaixo. Confirmar recebimento no aparelho e navegação usando os IDs do payload no cliente de teste.
6. Reenviar o mesmo evento com o mesmo delivery ID, depois outro delivery ID com o mesmo run/tentativa. Confirmar uma única falha/outbox. Enviar outro run com timestamp posterior e confirmar uma nova falha/outbox.
7. Encerrar e reiniciar a API. Confirmar que resultados `sent` não são enviados novamente. Os testes automatizados cobrem também queda antes da conclusão, recuperação e fencing.
8. Executar os testes de erros FCM e retry. Se houver condição transitória real controlada no ambiente de teste, observar `failed` e a próxima tentativa. Não alterar permissões do projeto produtivo para provocar erro.
9. Desativar o dispositivo com `DELETE /v1/device` e enviar uma nova falha. Confirmar que não há novo envio. Repetir com remoção do repositório em dados descartáveis, somente se desejado.
10. Registrar explicitamente os resultados remotos, incluindo aparelho offline, reencontro de notificações, duplicidades e eventual colapso de mensagens.

Exemplo de corpo, substituindo IDs e timestamp por valores do ambiente de teste:

```json
{
  "repository": {"id": 10},
  "workflow_run": {
    "id": 300,
    "workflow_id": 20,
    "run_attempt": 1,
    "status": "completed",
    "conclusion": "failure",
    "updated_at": "2026-10-01T15:00:00Z",
    "head_branch": "main",
    "head_sha": "abc123",
    "html_url": "https://github.com/acme/api/actions/runs/300"
  }
}
```

Com `WEBHOOK_SECRET`, `WEBHOOK_URL` e `DELIVERY_ID` definidos no shell do ambiente de teste, gerar a assinatura sobre os bytes exatos de `event.json`:

```bash
SIGNATURE=$(python3 - <<'PY'
import hashlib, hmac, os
with open('event.json', 'rb') as payload:
    print('sha256=' + hmac.new(os.environ['WEBHOOK_SECRET'].encode(), payload.read(), hashlib.sha256).hexdigest())
PY
)
curl -i "$WEBHOOK_URL" \
  -H 'Content-Type: application/json' \
  -H "X-Hub-Signature-256: $SIGNATURE" \
  -H "X-GitHub-Delivery: $DELIVERY_ID" \
  -H 'X-GitHub-Event: workflow_run' \
  --data-binary @event.json
```

Consulta segura para verificar estado da outbox, sem ler tokens:

```sql
SELECT id, pipeline_failure_id, device_id, failure_deduplication_key,
       status, attempt_count, next_attempt_at, last_attempt_at, sent_at, last_error_code
FROM notification_deliveries
ORDER BY created_at DESC;
```

### Resultado esperado por passo

| Passo | Resultado |
| --- | --- |
| 1 | checks verdes e integração realmente executada |
| 2 | inicialização válida e ambas as verificações de saúde `200` |
| 3 | um dispositivo ativo com token criptografado |
| 4 | webhook `202`, estado/falha/outbox persistidos antes do envio |
| 5 | `sent` após aceitação FCM; resultado físico registrado separadamente |
| 6 | mesma falha/run sem outra outbox; outro run com nova outbox |
| 7 | resultados concluídos não reenviados; pendentes recuperados |
| 8 | retry agendado, no máximo cinco tentativas; `Retry-After` respeitado |
| 9 | nenhum novo envio para dispositivo desativado ou repositório removido |
| 10 | evidência de comportamento físico, incluindo limitações offline |

### Como identificar regressão

Falhas antigas alterando o estado, nova outbox para o mesmo run/tentativa, sexta tentativa, duplicação por dois claims simultâneos, dispositivo novo desativado por resultado obsoleto, trabalho preso em `sending` após expiração, token em log ou API que não encerra indicam regressão.

### Como coletar logs

Filtrar `notification attempt completed` por `notification_id`. Os campos esperados são `attempt`, `status` e `error_code`; erros de claim/finalização usam mensagens genéricas. Não anexar variáveis de ambiente, tokens, credenciais ou dump integral do banco.

## Riscos e limitações

- Não há confirmação de recebimento físico nem validação Firebase real nesta execução.
- Um FCM aceito antes de uma queda ou perda da resposta pode ser reenviado após recuperação. A outbox evita duplicação local, sem tornar a chamada externa exactly-once.
- Mensagens de notificação FCM são collapsible; offline, falhas diferentes podem ser substituídas no transporte. Android `tag` e `apns-collapse-id` não comprovam uma entrega por falha. O feed persistente mantém os registros para consulta.
- Um push já em voo pode chegar depois da desativação ou exclusão. A rechecagem impede novos envios detectavelmente inelegíveis, sem revogar mensagens já aceitas pelo provedor.
- Queda durante a quinta tentativa termina em abandono após a reserva; entrega física pode ter ocorrido ou não.
- A quinta tentativa encerra também falhas transitórias persistentes. Não há replay administrativo nesta fase.
- O polling introduz latência de até o intervalo configurado, além do HTTP. Há um sender sequencial por instância, adequado ao MVP.
- O critério central de exatamente um push continua sem aceite e requer decisão explícita do usuário antes de declarar o MVP concluído. Este relatório não altera unilateralmente esse critério.

Referências técnicas: [FCM HTTP v1](https://firebase.google.com/docs/cloud-messaging/send/v1-api), [códigos de erro](https://firebase.google.com/docs/cloud-messaging/error-codes), [mensagens collapsible](https://firebase.google.com/docs/cloud-messaging/customize-messages/collapsible-message-types).

## Deploy e rollback

Ainda sem deploy. Antes de publicar, configurar a conta de serviço e testar o envio remoto. A nova variável é obrigatória; a API não inicia sem credenciais estruturalmente válidas. Nenhuma migration nova é exigida.

Rollback: restaurar o binário da Fase 4. Ele continua persistindo a outbox, mas não envia pushes. O banco permanece compatível. Reservas `sending` são retomadas quando esta fase for reaplicada e o intervalo expirar. Não apagar ou resetar outbox para simular rollback, pois isso pode repetir envios.

## Checklist de aceite

- [x] Fase 4 aprovada e branch adequada criada de `main` atualizada.
- [x] Interface/fake, sender HTTP v1, worker separado e configuração implementados.
- [x] Claims, fencing, concorrência e recuperação validados em PostgreSQL.
- [x] Retry limitado, intervalos e token inválido cobertos por testes.
- [x] Shutdown e deadline OAuth/HTTP validados.
- [x] Checks automatizados e smoke local verdes.
- [x] Nenhum segredo ou payload privado no log do sender.
- [ ] Push Firebase real validado.
- [ ] Recebimento físico e comportamento offline registrados.
- [ ] Decisão explícita sobre o aceite de entrega remota.
- [ ] Publicação da branch e abertura do PR autorizadas.
- [ ] PR revisado e Fase 5 aprovada pelo usuário.
