export const terminal = status => ['SUCCEEDED','FAILED','PARTIAL','CANCELLED'].includes(status);

export function applyTaskEvent(current, event) {
  if (!current || BigInt(event.seq) <= BigInt(current.event_seq || '0')) return current;
  const payload = event.payload || {};
  return {
    ...current,
    event_seq: event.seq,
    status: payload.status && ['QUEUED','RUNNING','RETRY_WAIT','CANCELLING','SUCCEEDED','FAILED','PARTIAL','CANCELLED'].includes(payload.status)
      && event.type !== 'tool_finished' ? payload.status : current.status,
    phase: payload.phase ?? current.phase,
    error_code: payload.error_code || current.error_code,
  };
}

export function mergeEvent(events, event) {
  if (events.some(item => item.seq === event.seq)) return events;
  return [...events,event].sort((a,b) => BigInt(a.seq)<BigInt(b.seq)?-1:1);
}
