import type { ConfigState } from '../../src/api/system-config';
export function configFixture(): ConfigState {
  const config = {
    port: 18080, debug: false, server_name: 'Emby-go', disable_library_monitor: false, library_monitor_mode: 'realtime' as const,
    db_path: '/data/app.db', actors_dir: '', redis_addr: 'localhost:6379', redis_db: 0, redis_password_configured: true,
    ffprobe_path: '', probe_timeout_seconds: 90, probe_concurrency: 2, server_domains: [],
    ffmpeg_enabled: false, ffmpeg_path: '', playback_concurrency: 2, playback_cache_mb: 2048,
  };
  return { config_path: '/etc/emby-go/config.yaml', writable: true, revision: 'rev-1', server_id: 'test-id',
    config, effective: { ...config, actors_dir: '/data/actors' }, restart_fields: [], warnings: [] };
}
