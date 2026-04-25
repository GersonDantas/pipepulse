import {
  getPipelineState,
  savePipelineState,
} from "../repositories/state.repository";

export async function processEvent (event: any) {
  const current = await getPipelineState(event.pipelineId);

  // 🔁 Idempotência
  if (current?.lastEventId === event.eventId) {
    console.log("⚠️ Evento duplicado");
    return;
  }

  // ⏳ Timestamp antigo
  if (current && new Date(event.timestamp) < new Date(current.timestamp)) {
    console.log("⏳ Evento antigo ignorado");
    return;
  }

  // ⚖️ Timestamp igual → prioridade
  if (current && event.timestamp === current.timestamp) {
    if (getPriority(event.status) <= getPriority(current.status)) {
      console.log("⚖️ Evento com menor prioridade ignorado");
      return;
    }
  }

  // 💾 Atualiza estado
  await savePipelineState({
    pipelineId: event.pipelineId,
    status: event.status,
    timestamp: event.timestamp,
    lastEventId: event.eventId,
  });

  console.log("✅ Estado atualizado:", event.status);

  // 🔔 Notificação
  notify(event);
}

function getPriority (status: string) {
  const map: any = {
    failed: 3,
    success: 2,
    running: 1,
  };
  return map[status] || 0;
}

function notify (event: any) {
  console.log("🔔 Notificação:", event.status);
}
