import { enqueueEvent } from "../queue/event.queue";

export async function handleWebhook (payload: any) {
  const event = {
    eventId: String(payload.workflow_run?.id),
    pipelineId: String(payload.workflow_run?.id),
    status: payload.workflow_run?.conclusion || "running",
    timestamp: payload.workflow_run?.updated_at,
  };

  console.log("📩 Evento recebido:", event);

  await enqueueEvent(event);
}
