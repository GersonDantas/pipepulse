# 📘 AGENTS.md

## 📍 Fonte central obrigatória

Antes de implementar, revisar ou testar qualquer parte do MVP, leia `pipeline-notifier-go/RELATORIO_MVP.md` por completo.

Esse relatório é a fonte de verdade para decisões de produto, monetização, arquitetura alvo, escopo, TDD, branches, Pull Requests, testes e aceite. Este arquivo complementa o relatório com invariantes de engenharia. Quando houver conflito entre os dois, `pipeline-notifier-go/RELATORIO_MVP.md` prevalece para o MVP público.

Qualquer mudança de escopo ou arquitetura deve atualizar primeiro o plano central.

## 🎯 Objetivo do Projeto

Sistema privado para monitoramento de pipelines GitHub em tempo real via webhooks, com processamento assíncrono orientado a eventos. GitLab permanece como evolução futura.

O sistema deve:

- Garantir consistência de estado dos pipelines
- Lidar com eventos fora de ordem e duplicados
- Notificar usuários apenas quando houver mudança relevante
- Ser simples no MVP e evolutivo para escala

---

## 🧱 Stack Tecnológica

- Backend: Go
- HTTP Framework: `gin`
- Concorrência: goroutines + channels
- Banco: in-memory no protótipo atual → PostgreSQL no MVP público
- Fila: PostgreSQL como fonte durável + `chan` como sinal de despertar
- Aplicativo: Flutter/Dart para Android, mantendo o build iOS funcional
- Arquitetura: event-driven

---

## 🧭 Arquitetura

Fluxo principal:

Webhook → Handler → Persistência da entrega → Sinal → Processor → Estado/Falha/Outbox → Notification

### Regras:

- Handler recebe a requisição HTTP e faz validação estrutural
- Handler usa `gin.Context` apenas como adaptador HTTP
- Handler/Service NÃO contém lógica de negócio
- Processor é responsável pelas decisões
- Eventos são processados de forma assíncrona
- A entrega válida deve ser persistida antes da resposta `202`
- Estado deve ser atualizado antes de qualquer notificação
- Nunca processar regra de negócio diretamente no webhook

---

## ⚙️ Processor (Coração do Sistema)

Ordem obrigatória de processamento:

1. Checar duplicidade (`X-GitHub-Delivery`)
2. Validar timestamp
3. Resolver conflitos (timestamp igual)
4. Atualizar estado
5. Criar falha única por run e tentativa
6. Criar outbox de notificação (se relevante)
7. Marcar a entrega como processada

---

## 🔐 Regras de Negócio Críticas

### Idempotência

- Cada evento possui um `DeliveryID`, originado de `X-GitHub-Delivery`
- Eventos duplicados devem ser ignorados

---

### Consistência temporal

- Eventos antigos devem ser ignorados:

if (event.timestamp < current.timestamp) → IGNORA

- Timestamps devem ser normalizados antes da comparação
- Se o MVP usar string, manter formato RFC3339/UTC

---

### Conflito de timestamp

Se timestamps forem iguais:

- Aplicar prioridade de status:

failed > success > cancelled > running

---

### Estado

- O estado do pipeline nunca pode regredir
- Sempre manter o último estado válido
- Identificar no mínimo repositório, workflow, run, tentativa, status, conclusão, timestamp e última entrega

---

### Notificações

- Notificar uma vez por falha relevante, workflow run, tentativa e dispositivo
- Runs distintos com falha geram notificações distintas, mesmo quando o estado anterior já era `failed`
- Estado, falha e outbox devem ser persistidos antes de qualquer tentativa de push

---

## 🧵 Concorrência em Go

- O protótipo atual usa `chan models.Event`; no MVP, o channel será apenas um sinal de despertar
- PostgreSQL preserva entregas pendentes quando o processo reinicia ou um sinal é perdido
- Usar um processor por instância no MVP
- Se houver mais de um worker no futuro, usar exclusão mútua por workflow

---

## 📂 Estrutura de Pastas

cmd/
  api/
    main.go

internal/
  handlers/
  services/
  processor/
  repository/
  queue/
  models/
  utils/

---

## 📏 Convenções de Código

- Código simples e legível
- Funções pequenas e com responsabilidade única
- Nomeação clara e descritiva
- Evitar abstração prematura no MVP
- Evitar múltiplos bancos no início
- Preferir fluxo direto e tipos simples

---

## ⚠️ Restrições

- Não usar múltiplas filas no MVP
- Não introduzir Redis ou broker externo no MVP
- Não adicionar complexidade desnecessária
- Não misturar lógica de negócio com infraestrutura
- Não criar arquitetura distribuída prematuramente
- Não introduzir dependências pesadas sem necessidade

---

## 🧠 Diretrizes para IA

- Ler `pipeline-notifier-go/RELATORIO_MVP.md` antes de qualquer ação do MVP
- Sempre seguir arquitetura orientada a eventos
- Nunca processar lógica diretamente no webhook
- Sempre persistir a entrega antes de sinalizar o processor
- Priorizar simplicidade
- Não quebrar regras de consistência (timestamp + idempotência)
- Não sugerir soluções síncronas para processamento
- Evitar criar arquivos desnecessários
- Manter nomes e organização compatíveis com Go
- Executar toda criação ou alteração de comportamento em ciclos TDD `Red, Green, Refactor`
- Criar e fazer checkout de uma branch adequada antes de editar uma nova fase
- Produzir o relatório obrigatório de cada PR, com commits, mudanças, evidências TDD e roteiro de testes do usuário
- Parar após cada PR para revisão e aprovação explícita; não fazer merge automático nem iniciar fase dependente

---

## 🧩 Decisões Arquiteturais Importantes

- Timestamp é usado como fonte de verdade temporal
- Não usar versionamento externo (não controlamos origem dos eventos)
- Resolver conflitos via prioridade de status
- PostgreSQL garante durabilidade; o channel desacopla e desperta o processamento
- Estado persistido deve refletir apenas a última decisão válida

---

## 🚀 Evolução Futura (NÃO IMPLEMENTAR AGORA)

- Redis para fila distribuída
- GitLab
- Equipes, membros e permissões
- Publicação iOS
- Assinaturas Pro e billing
- Patrocínio e anúncios após validação
- Particionamento de filas
- Métricas e analytics
- Worker pool mais avançado

---

## 🧹 Otimização de Tokens

- Evitar leitura de arquivos desnecessários
- Não analisar `node_modules`
- Não expandir logs grandes
- Priorizar `pipeline-notifier-go/RELATORIO_MVP.md` como fonte central
- Usar este arquivo para invariantes complementares
- Ser objetivo nas respostas

---

## 📌 Contexto Importante

- O MVP depende de eventos externos do GitHub
- Ordem de chegada dos eventos não é confiável
- Sistema deve ser determinístico e resiliente
- Prioridade: consistência > confiabilidade > segurança > simplicidade > performance

---

## ⚡ Regra de Ouro

"Consistência não é descobrir o que é certo, é definir regras que nunca entram em contradição."
