const db: Record<string, any> = {};

export async function getPipelineState (id: string) {
  return db[id];
}

export async function savePipelineState (data: any) {
  db[data.pipelineId] = data;
}
