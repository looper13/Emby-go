export type PlaybackStage = 'local_state' | 'task_submitted' | 'source_ready' | 'prepared' | 'player_loaded' | 'first_frame';
let sequence = 0;

// User Timing stays in the browser; names contain no media IDs or private URLs.
export function startPlaybackTiming(): (stage: PlaybackStage) => void {
  if (typeof performance === 'undefined' || typeof performance.mark !== 'function' || typeof performance.measure !== 'function') return () => {};
  const prefix = `r18-play:${++sequence}`, start = `${prefix}:click`;
  performance.mark(start);
  const recorded = new Set<PlaybackStage>();
  return stage => {
    if (recorded.has(stage)) return;
    recorded.add(stage);
    const end = `${prefix}:${stage}`;
    performance.mark(end);
    performance.measure(end, start, end);
  };
}
