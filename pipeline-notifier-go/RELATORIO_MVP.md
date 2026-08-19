# PipePulse: plano central do MVP

## Autoridade deste documento

Este é o plano central e a fonte de verdade para concluir o MVP do PipePulse.

Antes de implementar, revisar ou testar qualquer parte do MVP, pessoas e agentes devem ler este documento. Ele concentra:

- decisões de produto e monetização
- escopo e critérios de aceite
- arquitetura alvo
- contratos públicos
- disciplina TDD
- fases de implementação
- fluxo de branches, commits e Pull Requests
- relatório obrigatório de cada PR
- testes necessários para aprovação

Quando houver conflito entre este documento e descrições antigas do protótipo em `README.md`, `ARCHITECTURE.md` ou `AGENTS.md`, este plano prevalece para o MVP público. Alterações de escopo ou arquitetura precisam atualizar primeiro este documento.

## Governança do plano central

Este é um documento vivo. Ele registra não apenas o destino do MVP, mas também o ponto atual da execução. Uma pessoa ou LLM que assumir trabalho no projeto deve:

1. Ler este documento por completo antes de alterar código, infraestrutura ou produto.
2. Consultar o quadro de execução e o registro de PRs.
3. Confirmar que a fase anterior foi aprovada pelo usuário quando existir dependência.
4. Trabalhar somente no escopo da fase e branch autorizadas.
5. Atualizar o relatório do PR e as evidências TDD durante a execução.
6. Parar quando o PR estiver pronto para revisão e testes do usuário.
7. Atualizar este quadro após aprovação ou mudança de decisão.

Somente o usuário pode considerar um PR aprovado. Check verde, review automatizado, push ou abertura do PR não substituem essa aprovação.

Estados permitidos:

- `pendente`: ainda não iniciada
- `em andamento`: branch ativa, sem PR pronto para revisão
- `em revisão`: PR entregue ao usuário com relatório e roteiro de testes
- `aprovada`: revisão e testes aceitos explicitamente pelo usuário
- `bloqueada`: existe impedimento registrado que exige decisão ou dependência externa

### Quadro de execução

| Fase | Entrega | Estado | Branch | PR | Dependência para avançar |
| ---: | --- | --- | --- | --- | --- |
| 0 | Plano central e alinhamento documental | aprovada | `main` após merge | [#1](https://github.com/GersonDantas/pipepulse/pull/1) | concluída |
| 1 | Fundação Go | aprovada | `main` após merge | [#2](https://github.com/GersonDantas/pipepulse/pull/2) | concluída |
| 2 | PostgreSQL e persistência durável | aprovada | `main` após merge | [#3](https://github.com/GersonDantas/pipepulse/pull/3) | concluída |
| 3 | Webhook GitHub vertical | aprovada | `feat/mvp-github-webhook` | [#4](https://github.com/GersonDantas/pipepulse/pull/4) | concluída |
| 4 | Autenticação e API do produto | pendente | `feat/mvp-auth-api` | não aberto | Fase 3 aprovada |
| 5 | FCM, outbox e retry | pendente | `feat/mvp-fcm` | não aberto | Fase 4 aprovada |
| 6 | Aplicativo Flutter Android/iOS | pendente | `feat/mvp-flutter-app` | não aberto | Fases 4 e 5 aprovadas |
| 7 | Operação privada e alpha | pendente | `chore/mvp-private-alpha-release` | não aberto | Fases 1 a 6 aprovadas |
| 8 | Beta com patrocinador | pendente | `feat/beta-sponsorship` | não aberto | alpha validado e autorização do usuário |
| 9 | Assinatura Pro | pendente | `feat/pro-subscriptions` | não aberto | demanda paga validada e autorização do usuário |

Fases independentes podem ser preparadas em paralelo somente com autorização explícita. Dependências continuam impedindo merge, lançamento ou início antecipado por decisão unilateral da implementação.

### Registro de PRs

Atualizar uma linha ao abrir, revisar, aprovar ou rejeitar cada PR. O relatório detalhado permanece no próprio PR; este registro funciona como índice central.

| Fase | PR | Branch | Estado | Commits | Testes | Aprovação do usuário | Observações |
| ---: | --- | --- | --- | ---: | --- | --- | --- |
| 0 | [#1](https://github.com/GersonDantas/pipepulse/pull/1) | `docs/mvp-central-plan` | aprovada | 3 | `git diff --check`, `go test ./...` e `go vet ./...` verdes | aprovada explicitamente pelo usuário | plano central criado, documentos antigos alinhados e Fase 1 liberada |
| 1 | [#2](https://github.com/GersonDantas/pipepulse/pull/2) | `feat/mvp-backend-foundation` | aprovada | 6 | `go test ./...`, `go test -race ./...` e `go vet ./...` verdes | aprovada explicitamente pelo usuário em 2026-07-22 | configuração, composição explícita, evento GitHub normalizado, fila drenável e logging estruturado incorporados à `main` |
| 2 | [#3](https://github.com/GersonDantas/pipepulse/pull/3) | `feat/mvp-postgres` | aprovada | 4 | `go test ./...`, `go test -race ./...`, `go vet ./...`, `govulncheck ./...` e integração PostgreSQL verdes | aprovada explicitamente pelo usuário em 2026-08-18 | schema completo, transação atômica, recuperação, retenção e entitlements Free incorporados à `main` |
| 3 | [#4](https://github.com/GersonDantas/pipepulse/pull/4) | `feat/mvp-github-webhook` | aprovada | 9 | `go test ./...`, `go test -race ./...`, `go vet ./...`, `govulncheck ./...` e integração PostgreSQL verdes | aprovada explicitamente pelo usuário em 2026-08-19 | endpoint seguro, associação persistente, busca indexada e fluxo até o feed aprovados; merge pendente |

Nenhuma linha pode ser marcada como `aprovada` sem a confirmação explícita do usuário. Quando um PR for reprovado ou exigir correções, manter o mesmo registro e anotar a nova rodada de testes.

### Decisões vigentes

| Decisão | Escolha atual | Consequência |
| --- | --- | --- |
| Código | privado durante a validação | remover premissas de open source e self-host público |
| Aplicativo | Flutter/Dart | Android primeiro, com build iOS funcional desde o início |
| Backend | Go com Gin | preservar o núcleo atual e evoluir por fases |
| Persistência do MVP | PostgreSQL | channel atua somente como sinal, sem ser fonte durável |
| Plano inicial | Free | núcleo útil sem cobrança e sem anúncios no alpha |
| Monetização beta | patrocínio direto e discreto | não integrar AdMob inicialmente |
| Monetização futura | Pro e Team | liberar somente após validar demanda e funcionalidades pagas |
| Desenvolvimento | TDD `Red, Green, Refactor` | toda mudança de comportamento começa por teste vermelho válido |
| Integração | um PR revisável por fase relevante | parar após cada PR para revisão e testes do usuário |

Uma mudança nessas decisões exige atualização deste quadro, justificativa no PR e aprovação do usuário antes de alterar a implementação.

## Objetivo

Entregar um produto privado, gratuito e confiável para acompanhar pipelines do GitHub. O primeiro lançamento será no Android, usando uma aplicação Flutter cuja versão iOS permanecerá compilável desde o início.

O MVP estará concluído quando uma pessoa conseguir:

1. Entrar com GitHub.
2. Receber um workspace automaticamente.
3. Cadastrar repositório e workflow principal.
4. Configurar URL e segredo do webhook no GitHub.
5. Consultar o último estado válido do pipeline.
6. Visualizar falhas recentes.
7. Receber exatamente um push por falha relevante.
8. Continuar usando o produto após reinícios da API sem perda de estado ou trabalho pendente.

Prioridade geral:

```text
consistência > confiabilidade > segurança > simplicidade > performance
```

## Estado atual

O repositório contém um protótipo do núcleo Go:

- API HTTP com Gin
- webhook GitHub simplificado
- channel buffered em memória
- worker assíncrono
- processor com regras temporais básicas
- repository em memória protegido por mutex
- testes de handlers, services, processor e router

O protótipo ainda não representa um produto publicável. Permanecem ausentes ou incompletos:

- PostgreSQL e migrations
- persistência durável de entregas
- HMAC real
- identificação correta de entrega, repositório, workflow e run
- autenticação GitHub
- usuários e workspaces
- cadastro de repositórios
- feed persistente
- dispositivos e FCM
- outbox e retry de push
- aplicativo Flutter
- deploy e operação

Problemas conhecidos do protótipo:

- `main.go` lê `port` em vez de `PORT`
- a assinatura apenas precisa estar presente e não é validada
- `workflow_run.id` é usado incorretamente como evento e pipeline
- o GitHub envia `failure`, enquanto o domínio antigo espera `failed`
- o estado desaparece em reinícios
- a notificação atual é apenas um log

## Estratégia comercial

### Posicionamento

- O código será privado durante a validação do produto.
- O alpha será gratuito, sem anúncios e sem cobrança.
- O plano gratuito continuará resolvendo o problema principal depois do alpha.
- Patrocínio direto e discreto será experimentado durante a beta.
- AdMob e publicidade comportamental não fazem parte do MVP.
- A assinatura Pro será introduzida somente depois que o núcleo estiver confiável e houver evidência de demanda.
- A remoção de anúncios será um benefício adicional do Pro, não sua proposta principal.
- Self-host público, Apache-2.0 e documentação de instalação para terceiros estão fora do plano atual.

### Planos previstos

| Recurso | Free | Pro futuro | Team futuro |
| --- | ---: | ---: | ---: |
| Repositórios | 3 | 25 | 100+ |
| Workflows por repositório | 1 | 5 | configurável |
| Dispositivos | 1 | 3 | por membro |
| Push de falha | sim | sim | sim |
| Alerta de recuperação | não | sim | sim |
| Histórico | 7 dias | 90 dias | 365 dias |
| Filtros por branch | não | sim | sim |
| Horários silenciosos | não | sim | sim |
| Email, Slack e Discord | não | sim | sim |
| Exportação | não | CSV e JSON | CSV, JSON e API |
| Membros e permissões | não | não | sim |
| Reconhecimento de incidentes | não | não | sim |
| Auditoria | não | não | sim |
| Patrocínio | beta | não | não |

O backend é a única fonte de verdade dos limites e funcionalidades. O app não libera recursos usando decisões locais.

No MVP, o workspace terá `plan_code=free`, e o bootstrap retornará os entitlements:

```json
{
  "plan": "free",
  "entitlements": {
    "max_repositories": 3,
    "max_workflows_per_repository": 1,
    "max_devices": 1,
    "history_days": 7,
    "sponsor_enabled": false,
    "recovery_alerts": false,
    "integrations": false,
    "exports": false
  }
}
```

Durante o alpha, `sponsor_enabled` permanecerá falso. O preço da assinatura será definido apenas depois da beta, com dados de uso e retenção.

## Escopo do MVP

### Inclui

- backend Go com API pública HTTPS
- PostgreSQL como persistência primária
- migrations versionadas
- autenticação via GitHub
- criação automática de workspace
- plano Free e entitlements server-side
- cadastro manual de repositório e workflow
- segredo individual por repositório
- HMAC SHA-256 real
- persistência durável antes do `202`
- processamento assíncrono e determinístico
- estado persistente por workflow
- feed de falhas dos últimos 7 dias
- FCM e push de falha
- retry e idempotência de notificação
- aplicativo Flutter para Android
- projeto e build iOS mantidos funcionais
- deploy privado de baixo custo
- política de privacidade, termos e exclusão de conta
- teste fechado gratuito no Google Play

### Não inclui

- publicação do iOS
- painel web
- múltiplos dispositivos no plano Free
- membros, equipes e permissões
- vários workflows liberados no plano Free
- alerta de recuperação
- integrações externas
- patrocínio ativo no alpha
- AdMob
- billing ou assinatura ativa
- GitHub App
- configuração automática do webhook
- Redis ou fila distribuída
- analytics avançado
- código aberto ou self-host público

## Arquitetura alvo

Fluxo principal:

```text
GitHub webhook
  -> Handler HTTP
  -> validação estrutural e HMAC
  -> persistência da entrega
  -> sinal para worker
  -> Processor
  -> transação de estado, falha e outbox
  -> sender FCM
  -> aplicativo Flutter
```

Decisões:

- Backend em Go com Gin.
- PostgreSQL via `pgxpool`.
- Migrations SQL com Goose.
- Logging JSON com `slog`.
- Um processador de pipelines por instância.
- Um sender separado para notificações pendentes.
- PostgreSQL é a fonte durável de entregas e notificações.
- O channel é apenas um sinal de despertar; perder o sinal não perde o evento.
- O worker consulta periodicamente entregas pendentes.
- Handler e service não contêm decisões de negócio.
- Estado, falha e outbox são persistidos na mesma transação.
- O estado é salvo antes de qualquer tentativa de push.
- Graceful shutdown interrompe novas entradas e preserva trabalhos pendentes.

## Contratos da API

### Saúde

- `GET /health/live`
- `GET /health/ready`

### Autenticação

- `POST /v1/auth/github/start`
- `GET /v1/auth/github/callback`
- `POST /v1/auth/github/exchange`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`

OAuth usará Authorization Code com PKCE `S256` e `state` de uso único. O token GitHub será usado somente para consultar `/user` e será descartado em seguida.

- access token: 15 minutos
- refresh token: 30 dias
- refresh token rotacionado a cada uso
- somente hashes dos tokens persistidos

### Produto

- `GET /v1/bootstrap`
- `POST /v1/repositories`
- `GET /v1/repositories`
- `GET /v1/repositories/{id}`
- `POST /v1/repositories/{id}/rotate-webhook-secret`
- `DELETE /v1/repositories/{id}`
- `GET /v1/failures?cursor=&limit=`
- `PUT /v1/device`
- `DELETE /v1/device`
- `DELETE /v1/account`

O feed terá limite padrão 20 e máximo 50. Todas as rotas `/v1`, exceto autenticação, exigem bearer token e aplicam o workspace da sessão. Recurso pertencente a outro workspace responde `404`.

Erro padrão:

```json
{
  "error": {
    "code": "entitlement_required",
    "message": "repository limit reached",
    "feature": "max_repositories"
  }
}
```

### Webhook

- `POST /webhooks/github/{endpoint_id}`

Exigir:

- `X-Hub-Signature-256`
- `X-GitHub-Delivery`
- `X-GitHub-Event: workflow_run`

O corpo será limitado a 1 MiB. A entrega válida será persistida antes da resposta `202`.

### Páginas legais

- `GET /privacy`
- `GET /delete-account`

## Modelo persistente

Criar migrations para:

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

`monitored_workflows` será separado de `repositories` desde o MVP. O plano Free limitará a criação a um workflow, mas o futuro Pro não exigirá remodelar o banco.

Restrições mínimas:

- GitHub user ID único
- um workspace por usuário
- um dispositivo ativo por usuário no Free
- repositório único por workspace, owner e nome
- delivery ID globalmente único
- falha única por repositório, workflow run e tentativa
- notificação única por falha e dispositivo

No MVP, somente `workspaces.plan_code` será necessário para monetização. Tabelas de assinatura serão criadas quando billing entrar de fato.

## Evento e regras de negócio

Evento interno:

- `DeliveryID`
- `RepositoryID`
- `WorkflowID`
- `WorkflowRunID`
- `RunAttempt`
- `Status`
- `Conclusion`
- `Timestamp`
- branch, SHA e URL da execução

Normalização:

- `created` ou conclusão nula: `running`, sem alerta
- `success`, `neutral` e `skipped`: `success`, sem alerta
- `cancelled` e `stale`: `cancelled`, sem alerta
- `failure`, `timed_out`, `startup_failure` e `action_required`: `failed`, com alerta

Prioridade em timestamp igual:

```text
failed > success > cancelled > running
```

Ordem do processor:

1. Identificar duplicidade por `X-GitHub-Delivery`.
2. Validar timestamp.
3. Resolver empate por prioridade.
4. Persistir estado.
5. Criar falha única por run e tentativa.
6. Criar outbox somente para falha relevante.
7. Marcar entrega como processada.

Runs diferentes com falha geram alertas distintos mesmo quando o estado anterior já era `failed`. Evento antigo ou duplicado não atualiza estado, feed ou push.

## Segurança e configuração

Segredo do webhook e token FCM serão criptografados com AES-256-GCM usando `DATA_ENCRYPTION_KEY`.

Regras:

- segredo gerado com `crypto/rand`
- segredo exibido somente na criação ou rotação
- nenhum segredo em logs
- payload integral não deve aparecer em logs
- tokens de sessão persistidos por hash
- isolamento obrigatório por workspace
- HMAC calculado sobre os bytes originais
- comparação de assinatura em tempo constante

Configurações obrigatórias:

- `PORT`
- `DATABASE_URL`
- `APP_BASE_URL`
- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `DATA_ENCRYPTION_KEY`
- `FCM_CREDENTIALS_JSON_B64`
- `MOBILE_OAUTH_REDIRECT_URI`
- `SUPPORT_EMAIL`
- `ENVIRONMENT`
- `LOG_LEVEL`

Retenção:

- entregas de webhook: 7 dias
- falhas do Free: 7 dias
- resultados de push: 30 dias

## Aplicativo Flutter

Configuração:

- Flutter e Dart
- package e bundle ID `com.pipepulse.app`
- Android `minSdk 26`
- Android `targetSdk 36`
- iOS mínimo 15
- Android e iOS criados no primeiro commit do app
- versão Flutter estável fixada no projeto e CI
- API definida por `--dart-define`
- nenhum segredo no app
- sem Node.js
- sem codegen no MVP

Bibliotecas e estrutura:

- Riverpod sem codegen para estado e dependências
- GoRouter para navegação e links
- Dio para HTTP e refresh sincronizado
- `flutter_secure_storage` para tokens
- `firebase_core` e `firebase_messaging` para push
- Material 3 com adaptações Cupertino
- Platform Channels isolados somente quando não houver plugin adequado
- organização por funcionalidades: auth, repositories, failures, notifications e settings

Telas:

1. Login GitHub.
2. Bootstrap do workspace.
3. Lista de repositórios e pipelines.
4. Feed de falhas.
5. Cadastro de repositório e workflow.
6. Exibição única da URL e segredo.
7. Configuração de notificações.
8. Rotação do segredo.
9. Exclusão do repositório.
10. Logout e exclusão da conta.

O app mostrará os limites do Free, mas não exibirá paywall no alpha. O build iOS permanecerá verde, sem publicação no primeiro ciclo.

## TDD obrigatório

Toda criação ou alteração de comportamento deve seguir ciclos pequenos de `Red, Green, Refactor`.

### Red

- Escrever o menor teste para um comportamento.
- Executar o teste mais estreito.
- Confirmar que falhou pela ausência do comportamento esperado.
- Não aceitar falhas ambientais ou não relacionadas como Red válido.

### Green

- Implementar somente o mínimo necessário.
- Executar novamente o teste estreito.
- Executar os testes relacionados.

### Refactor

- Melhorar nomes, responsabilidades e duplicações.
- Não adicionar comportamento novo.
- Reexecutar pacote e suíte relevante.

Regras para pessoas e agentes:

- não implementar funcionalidades diferentes em lote
- não acumular testes vermelhos de áreas distintas
- não enfraquecer asserções para obter Green
- começar bugs por teste de regressão
- proteger comportamento existente antes de refactors
- informar o Red, o Green e o Refactor de cada ciclo
- terminar cada commit com testes verdes
- terminar cada fase com todas as suítes verdes

Migrations começam por teste de integração vermelho. Docker, migrations de deploy e infraestrutura usam smoke tests reproduzíveis como Red.

## Fluxo Git e Pull Requests

A branch principal é `main`. Não fazer commits diretamente nela.

### Branches

Antes de qualquer alteração de uma fase:

1. Verificar `git status` e preservar mudanças do usuário.
2. Atualizar referências remotas.
3. Fazer checkout de `main`.
4. Atualizar somente por fast-forward.
5. Criar uma branch da fase.
6. Fazer checkout da branch antes de editar.

Exemplo:

```bash
git fetch origin
git switch main
git merge --ff-only origin/main
git switch -c feat/mvp-postgres
```

Branches planejadas:

- `docs/mvp-central-plan`
- `feat/mvp-backend-foundation`
- `feat/mvp-postgres`
- `feat/mvp-github-webhook`
- `feat/mvp-auth-api`
- `feat/mvp-fcm`
- `feat/mvp-flutter-app`
- `chore/mvp-private-alpha-release`
- `feat/beta-sponsorship`
- `feat/pro-subscriptions`

Uma branch adicional deve ser criada quando a mudança tiver escopo independente, risco próprio ou precisar de revisão isolada. Não criar branch por teste individual.

### Commits

- Um commit representa uma unidade funcional revisável.
- O commit acontece depois de Green e Refactor.
- Não criar commit com suíte quebrada.
- Não misturar fases ou alterações não relacionadas.
- Fazer stage somente dos arquivos da unidade atual.
- Preservar alterações preexistentes do usuário.

Exemplos:

```text
feat(webhook): validate GitHub HMAC signature
test(processor): cover consecutive workflow failures
refactor(repository): isolate pipeline state transaction
```

### Gate obrigatório de cada PR

Cada fase relevante terá seu próprio PR para `main`.

Ao concluir uma branch:

1. Executar todos os testes automatizados aplicáveis.
2. Fazer push da branch.
3. Abrir um PR draft para `main`.
4. Preencher o relatório completo do PR.
5. Disponibilizar roteiro reproduzível para revisão e testes do usuário.
6. Marcar pronto para revisão somente com checks verdes.
7. Parar e aguardar a revisão do usuário.
8. Não fazer merge automático.
9. Não iniciar uma fase dependente sem aprovação explícita.

## Relatório obrigatório de cada PR

Cada PR, sem exceção, deve apresentar um relatório para revisão e testes do usuário. O relatório global do último PR não substitui os relatórios individuais.

Template obrigatório:

```markdown
## Objetivo

## Escopo incluído

## Fora do escopo

## Commits
| Hash | Mensagem | Responsabilidade |
| --- | --- | --- |

## Mudanças por comportamento

## Arquivos principais alterados

## APIs, migrations e configurações

## Evidências TDD
### Red
### Green
### Refactor

## Testes automatizados
| Comando | Resultado |
| --- | --- |

## Roteiro para revisão e testes do usuário
### Pré-requisitos
### Dados de teste
### Passos
### Resultado esperado por passo
### Como identificar regressão
### Como coletar logs

## Riscos e limitações

## Deploy e rollback

## Checklist de aceite
```

Para PR de backend, incluir exemplos de requisição, payload ou assinatura, consultas de verificação no PostgreSQL e logs esperados.

Para PR Flutter, incluir APK ou instrução de build, dispositivo e API recomendados, telas afetadas, roteiro visual e resultados esperados.

O último PR do MVP também conterá uma consolidação de todos os PRs, commits, migrations, contratos, testes end-to-end, riscos aceitos e artefatos de publicação.

## Roadmap de implementação

### Fase 0: alinhamento

- versionar este plano central
- atualizar `AGENTS.md`, `ARCHITECTURE.md` e README para apontar para ele
- registrar Flutter, private-first, Free e TDD
- remover decisões antigas de Kotlin, open source e self-host
- manter a baseline atual verde

### Fase 1: fundação Go

- corrigir `PORT`
- configuração por ambiente
- composição explícita de dependências
- contextos e timeouts
- graceful shutdown
- logging estruturado
- novo modelo de evento
- mapeamento real dos status GitHub

### Fase 2: PostgreSQL

- pool e migrations
- repositories persistentes
- schema completo do MVP
- transações do processor
- recuperação de entregas pendentes
- retenção
- plano Free e entitlements

### Fase 3: webhook vertical

- endpoint por repositório
- segredo individual
- corpo bruto e limite
- HMAC real
- headers GitHub
- persistência antes do `202`
- associação com repositório e workflow
- processor até estado e feed

### Fase 4: autenticação e API

- OAuth GitHub
- workspace automático
- sessões rotativas
- bootstrap com entitlements
- cadastro e listagem
- limites Free
- feed paginado
- dispositivo
- exclusão de conta

### Fase 5: FCM

- interface e fake
- outbox
- retry
- idempotência
- tratamento de token inválido
- integração Firebase real

Retries:

1. 1 minuto.
2. 5 minutos.
3. 30 minutos.
4. 2 horas.
5. Encerrar após cinco tentativas.

### Fase 6: Flutter

- scaffold Android e iOS
- arquitetura por funcionalidades
- autenticação
- repositórios e workflows
- feed
- armazenamento seguro
- FCM e deep links
- configurações e exclusão
- testes unitários, widgets e integração

### Fase 7: operação privada e alpha

- Dockerfile interno
- migrations pré-deploy
- Render e Neon
- backups e restauração
- HTTPS e domínio
- política de privacidade e termos
- AAB assinado e Play App Signing
- Data Safety
- teste interno e fechado

### Fase 8: beta com patrocinador

Executar somente depois de observar uso recorrente:

- patrocinador direto, sem AdMob inicialmente
- conteúdo hospedado no próprio domínio
- exibição somente no Free
- card no final do feed ou configurações
- nenhuma publicidade em login, onboarding, webhook ou abertura de falha
- nenhum Advertising ID
- métricas agregadas de impressão e clique
- desligamento remoto

### Fase 9: assinatura Pro

Executar somente depois de validar demanda por pelo menos duas funcionalidades pagas:

- compras Android e iOS
- verificação server-side
- `subscriptions` e `billing_events`
- notificações das lojas
- restauração de compras
- entitlements server-side
- remoção do patrocínio
- histórico de 90 dias
- múltiplos workflows e dispositivos
- recuperação, filtros e integrações
- preço definido com dados da beta

## Testes obrigatórios

### Backend

```bash
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Também validar:

- migrations em PostgreSQL efêmero
- webhook até outbox
- atomicidade e rollback
- concorrência e duplicidade
- recuperação após reinício
- isolamento entre workspaces
- retenção e entitlements
- build e smoke test Docker

### Flutter

```bash
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
flutter test integration_test
flutter build appbundle --release
flutter build ipa --release --no-codesign
```

Validar Android em APIs 26 e 36, smoke test iOS em simulador e push em Android físico.

## Critérios de aceite do MVP

1. Usuário novo entra com GitHub.
2. Workspace é criado automaticamente.
3. Bootstrap retorna Free e entitlements.
4. Usuário cadastra repositório e workflow.
5. Quarto repositório é rejeitado pelo limite Free.
6. URL e segredo aparecem uma única vez.
7. Evento `created` atualiza para `running` sem push.
8. Evento `failure` persiste estado, cria feed e gera exatamente um push.
9. Redelivery não duplica feed ou push.
10. Evento antigo não regride estado.
11. Novo run com falha gera novo alerta.
12. Reinício mantém estado e retoma trabalho pendente.
13. Sucesso posterior atualiza sem alerta de recuperação.
14. Rotação invalida o segredo anterior.
15. Outro usuário não acessa os dados.
16. Exclusão remove conta, sessões, dispositivo e endpoints.
17. Android instala pelo teste fechado.
18. Build iOS permanece verde.
19. Todos os PRs possuem relatório e roteiro de testes aprovados pelo usuário.

O MVP somente será considerado concluído quando todos os critérios passarem no ambiente hospedado e no aplicativo instalado pelo Google Play.

## Pré-requisitos externos

Antes da publicação, o proprietário deverá fornecer:

- domínio
- GitHub OAuth App
- projeto Firebase
- credenciais FCM
- instância Render
- banco Neon
- conta Google Play Console
- email de suporte
- política e termos aprovados
- lista de testers

Essas dependências não autorizam ampliar o escopo técnico sem atualizar este plano.
