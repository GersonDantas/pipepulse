# 📘 AGENTS.md

## 🎯 Objetivo do Projeto

Sistema para monitoramento de pipelines (GitHub/GitLab) em tempo real via webhooks, com processamento assíncrono orientado a eventos.

O sistema deve:

- Garantir consistência de estado dos pipelines
- Lidar com eventos fora de ordem e duplicados
- Notificar usuários apenas quando houver mudança relevante
- Ser simples no MVP e evolutivo para escala

---

## 🧱 Stack Tecnológica

- Backend: Go
- HTTP: `net/http`
- Concorrência: goroutines + channels
- Banco: in-memory (MVP) → PostgreSQL (futuro)
- Fila: in-memory com `chan` buffered (MVP) → Redis / fila distribuída (futuro)
- Arquitetura: event-driven

---

## 🧭 Arquitetura

Fluxo principal:

Webhook → Handler → Service → Queue → Processor → Repository → Notification

### Regras:

- Handler recebe a requisição HTTP e faz validação estrutural
- Handler/Service NÃO contém lógica de negócio
- Processor é responsável pelas decisões
- Eventos são processados de forma assíncrona
- Estado deve ser atualizado antes de qualquer notificação
- Nunca processar regra de negócio diretamente no webhook

---

## ⚙️ Processor (Coração do Sistema)

Ordem obrigatória de processamento:

1. Checar duplicidade (`eventId`)
2. Validar timestamp
3. Resolver conflitos (timestamp igual)
4. Atualizar estado
5. Disparar notificação (se relevante)

---

## 🔐 Regras de Negócio Críticas

### Idempotência

- Cada evento possui um `eventId`
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

failed > success > running

---

### Estado

- O estado do pipeline nunca pode regredir
- Sempre manter o último estado válido
- Campos mínimos: `PipelineID`, `Status`, `Timestamp`, `LastEventID`

---

### Notificações

- Notificar apenas quando houver mudança relevante
- Evitar ruído (ex: running → running sem mudança significativa)
- Nunca notificar antes de persistir o estado

---

## 🧵 Concorrência em Go

- A fila do MVP é um `chan models.Event`
- O worker pool pode ser adicionado depois, sem quebrar o processamento determinístico
- Se houver mais de um worker, usar lock por `PipelineID`
- Não atualizar o mesmo pipeline em paralelo sem exclusão mútua

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
- Não adicionar complexidade desnecessária
- Não misturar lógica de negócio com infraestrutura
- Não criar arquitetura distribuída prematuramente
- Não introduzir dependências pesadas sem necessidade

---

## 🧠 Diretrizes para IA

- Sempre seguir arquitetura orientada a eventos
- Nunca processar lógica diretamente no webhook
- Sempre manter fluxo: webhook → fila → processor
- Priorizar simplicidade
- Não quebrar regras de consistência (timestamp + idempotência)
- Não sugerir soluções síncronas para processamento
- Evitar criar arquivos desnecessários
- Manter nomes e organização compatíveis com Go

---

## 🧩 Decisões Arquiteturais Importantes

- Timestamp é usado como fonte de verdade temporal
- Não usar versionamento externo (não controlamos origem dos eventos)
- Resolver conflitos via prioridade de status
- Fila é usada para desacoplamento e resiliência
- Estado persistido deve refletir apenas a última decisão válida

---

## 🚀 Evolução Futura (NÃO IMPLEMENTAR AGORA)

- Redis para fila distribuída
- PostgreSQL para persistência real
- Suporte a múltiplos usuários
- Particionamento de filas
- Métricas e analytics
- Worker pool mais avançado

---

## 🧹 Otimização de Tokens

- Evitar leitura de arquivos desnecessários
- Não analisar `node_modules`
- Não expandir logs grandes
- Priorizar este arquivo como fonte principal
- Ser objetivo nas respostas

---

## 📌 Contexto Importante

- Sistema depende de eventos externos (GitHub/GitLab)
- Ordem de chegada dos eventos não é confiável
- Sistema deve ser determinístico e resiliente
- Prioridade: consistência > performance > complexidade

---

## ⚡ Regra de Ouro

"Consistência não é descobrir o que é certo, é definir regras que nunca entram em contradição."
