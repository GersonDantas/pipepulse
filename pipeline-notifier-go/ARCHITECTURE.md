# Pipeline Notifier em Go

Guia de arquitetura e implementacao do MVP.

## Objetivo

Construir um backend capaz de receber eventos de pipelines, processar esses eventos de forma assincrona e manter um estado consistente mesmo quando os eventos chegam duplicados ou fora de ordem.

O sistema deve priorizar consistencia antes de performance e complexidade.

## Visao Geral

Fluxo principal:

```text
Webhook -> Handler -> Service -> Queue -> Processor -> Repository -> Notification
```

Responsabilidades:

| Camada | Responsabilidade |
| --- | --- |
| Handler | Recebe a requisicao HTTP e valida a estrutura de entrada |
| Service | Converte o payload externo em um evento interno |
| Queue | Desacopla a entrada HTTP do processamento |
| Processor | Aplica regras de negocio e decide se o estado muda |
| Repository | Armazena o ultimo estado valido do pipeline |
| Notification | Notifica apenas quando houver mudanca relevante |

## Conceitos Fundamentais

### Arquitetura orientada a eventos

O webhook nao processa regra de negocio diretamente. Ele recebe um evento externo, transforma esse dado em um evento interno e coloca esse evento em uma fila.

O processamento acontece depois, de forma assincrona, no processor.

### Desacoplamento

A fila evita que o tempo de resposta do webhook dependa do processamento completo do pipeline.

No MVP, essa fila e um channel buffered:

```go
eventChannel := make(chan models.Event, 100)
```

### Concorrencia em Go

O worker roda em uma goroutine:

```go
go func() {
    for event := range eventChannel {
        processor.ProcessEvent(event)
    }
}()
```

No MVP, um worker unico mantem o processamento simples e deterministico. Um worker pool pode ser adicionado depois, desde que exista exclusao mutua por `PipelineID`.

## Modelo de Evento

Evento interno minimo:

```go
type Event struct {
    EventID    string
    PipelineID string
    Status     string
    Timestamp  string
}
```

Esse modelo representa a entrada normalizada que o processor deve receber, independentemente da origem do webhook.

## Estado como Fonte da Verdade

Estado minimo persistido:

```go
type State struct {
    PipelineID  string
    Status      string
    Timestamp   string
    LastEventID string
}
```

O estado representa a ultima decisao valida do sistema para um pipeline.

Regras:

- O estado nunca deve regredir.
- O estado deve ser atualizado antes de qualquer notificacao.
- `Timestamp` deve ser tratado como fonte de verdade temporal.
- `LastEventID` e usado para idempotencia.

## Regras do Processor

Ordem obrigatoria:

1. Checar duplicidade por `EventID`.
2. Validar se o timestamp do evento nao e antigo.
3. Resolver conflito quando timestamps forem iguais.
4. Atualizar o estado.
5. Disparar notificacao se a mudanca for relevante.

## Idempotencia

Problema: o mesmo webhook pode ser entregue mais de uma vez.

Regra:

```go
if current != nil && current.LastEventID == event.EventID {
    return
}
```

Resultado: eventos duplicados nao alteram o estado e nao geram notificacao.

## Eventos Fora de Ordem

Problema: a ordem de chegada dos webhooks nao e confiavel.

Exemplo:

```text
10:00 -> failed
09:59 -> running
```

Regra:

```go
if current != nil && event.Timestamp < current.Timestamp {
    return
}
```

Resultado: eventos antigos sao ignorados.

No MVP, o timestamp pode ser comparado como string se estiver normalizado em RFC3339/UTC.

## Conflito de Timestamp

Problema: dois eventos podem chegar com o mesmo timestamp e status diferentes.

Regra de prioridade:

```text
failed > success > running
```

Implementacao:

```go
func getPriority(status string) int {
    switch status {
    case "failed":
        return 3
    case "success":
        return 2
    case "running":
        return 1
    default:
        return 0
    }
}
```

Se o novo evento tiver prioridade menor ou igual ao estado atual, ele deve ser ignorado.

## Notificacoes

Notificacoes nao devem ser disparadas para todo evento recebido.

Regra:

- `running -> running`: nao notifica.
- `running -> failed`: notifica.
- Evento duplicado: nao notifica.
- Evento antigo: nao notifica.

Importante: a notificacao deve acontecer somente depois de salvar o novo estado valido.

## Concorrencia e Lock por Pipeline

Com um worker unico, o MVP evita atualizacao paralela do mesmo pipeline.

Se o sistema evoluir para multiplos workers, sera necessario usar lock por `PipelineID` para impedir duas atualizacoes simultaneas do mesmo estado.

Exemplo de lock por chave:

```go
var locks = make(map[string]*sync.Mutex)
var globalLock sync.Mutex

func getLock(key string) *sync.Mutex {
    globalLock.Lock()
    defer globalLock.Unlock()

    if locks[key] == nil {
        locks[key] = &sync.Mutex{}
    }

    return locks[key]
}
```

Uso:

```go
lock := getLock(event.PipelineID)
lock.Lock()
defer lock.Unlock()
```

Essa evolucao deve ser adicionada apenas quando houver mais de um worker processando eventos.

## Testes Manuais

### Evento simples

```bash
curl -X POST http://localhost:3000/webhook/github \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_run": {
      "id": "123",
      "conclusion": "failed",
      "updated_at": "2026-01-01T10:00:00Z"
    }
  }'
```

Resultado esperado: estado atualizado e notificacao disparada.

### Duplicidade

Enviar o mesmo evento duas vezes.

Resultado esperado:

```text
Evento duplicado
```

### Evento antigo

Enviar um evento com timestamp menor que o estado atual.

Resultado esperado:

```text
Evento antigo
```

### Conflito de timestamp

Enviar dois eventos com o mesmo timestamp e status diferentes.

Resultado esperado: o status de maior prioridade prevalece.

## Aprendizados

### Concorrencia precisa ser controlada

Concorrencia sem regra de exclusao pode gerar estado incorreto. Concorrencia com chave de exclusao por pipeline permite escalar sem perder consistencia.

### Ordem de chegada nao e fonte de verdade

Eventos externos podem chegar duplicados, atrasados ou fora de ordem. O timestamp normalizado e o estado atual devem guiar a decisao.

### Sistemas distribuidos precisam ser deterministicos

O objetivo nao e descobrir a verdade absoluta do mundo externo. O objetivo e definir regras que sempre produzam a mesma decisao para o mesmo conjunto de eventos.

### Go simplifica o MVP

Channels e goroutines permitem criar uma fila e um worker assincrono sem dependencias externas no inicio do projeto.

## Evolucao Futura

Nao implementar no MVP sem necessidade real:

- PostgreSQL para persistencia real.
- Redis ou fila distribuida.
- Worker pool com lock por pipeline.
- Locks distribuidos.
- Particionamento por pipeline.
- Metricas e analytics.
- Dashboard.

## Regra de Ouro

Consistencia nao e descobrir o que e certo. Consistencia e definir regras que nunca entram em contradicao.
