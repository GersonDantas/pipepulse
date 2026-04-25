import { processEvent } from "../processors/event.processor";

const queue: any[] = [];

export async function enqueueEvent (event: any) {
  queue.push(event);
  processQueue();
}

async function processQueue () {
  while (queue.length > 0) {
    const event = queue.shift();
    await processEvent(event);
  }
}
