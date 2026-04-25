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

- Backend: Node.js + Express + TypeScript
- Banco: In-memory (MVP) → PostgreSQL (futuro)
- Fila: In-memory (MVP) → Redis + BullMQ (futuro)
- Arquitetura: Event-driven

---

## 🧭 Arquitetura

Fluxo principal:

Webhook → Backend → Valida/Salva → Queue → Processor → Atualiza Estado → Notification Service → Mobile

### Regras:

- Backend NÃO contém lógica de negócio
- Processor é responsável pelas decisões
- Eventos são processados de forma assíncrona
- Estado deve ser atualizado antes de qualquer notificação

---

## ⚙️ Processor (Coração do Sistema)

Ordem obrigatória de processamento:

1. Validar timestamp
2. Checar duplicidade (eventId)
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

---

### Conflito de timestamp

Se timestamps forem iguais:

- Aplicar prioridade de status:

failed > success > running

---

### Estado

- Estado do pipeline nunca pode regredir
- Sempre manter o último estado válido

---

### Notificações

- Notificar apenas quando houver mudança relevante
- Evitar ruído (ex: running → running sem mudança significativa)

---

## 📂 Estrutura de Pastas

src/
  controllers/
  services/
  processors/
  repositories/
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

---

## ⚠️ Restrições

- Não usar múltiplas filas no MVP
- Não adicionar complexidade desnecessária
- Não misturar lógica de negócio com infraestrutura
- Não criar arquitetura distribuída prematuramente

---

## 🧠 Diretrizes para IA

- Sempre seguir arquitetura orientada a eventos
- Nunca processar lógica diretamente no webhook
- Sempre manter fluxo: webhook → fila → processor
- Priorizar simplicidade
- Não quebrar regras de consistência (timestamp + idempotência)
- Não sugerir soluções síncronas para processamento
- Evitar criar arquivos desnecessários

---

## 🧩 Decisões Arquiteturais Importantes

- Timestamp é usado como fonte de verdade temporal
- Não usar versionamento externo (não controlamos origem dos eventos)
- Resolver conflitos via prioridade de status
- Fila é usada para desacoplamento e resiliência

---

## 🚀 Evolução Futura (NÃO IMPLEMENTAR AGORA)

- Redis + BullMQ para fila
- PostgreSQL para persistência real
- Suporte a múltiplos usuários
- Particionamento de filas
- Métricas e analytics

---

## 🧹 Otimização de Tokens

- Evitar leitura de arquivos desnecessários
- Não analisar node_modules
- Não expandir logs grandes
- Priorizar este arquivo como fonte principal
- Ser objetivo nas respostas

---

## 📌 Contexto Importante

- Sistema depende de eventos externos (GitHub/GitLab)
- Ordem de chegada dos eventos NÃO é confiável
- Sistema deve ser determinístico e resiliente
- Prioridade: consistência > performance > complexidade

---

## ⚡ Regra de Ouro

"Consistência não é descobrir o que é certo, é definir regras que nunca entram em contradição."
