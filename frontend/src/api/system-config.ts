import { apiClient } from './client';
import { boolean, count, invalid, record, string } from './admin';

export interface InstanceConfig {
  port: number; debug: boolean; server_name: string;
  disable_library_monitor: boolean; library_monitor_mode: 'realtime' | 'polling';
  db_path: string; actors_dir: string;
  redis_addr: string; redis_db: number; redis_password_configured: boolean;
  alist_url: string; alist_token_configured: boolean;
  ffprobe_path: string; probe_timeout_seconds: number; probe_concurrency: number;
  ffmpeg_enabled: boolean; ffmpeg_path: string; playback_concurrency: number; playback_cache_mb: number;
  server_domains: { name: string; url: string }[];
}
export type ConfigPatch = Partial<Omit<InstanceConfig, 'redis_password_configured' | 'alist_token_configured'>> & {
  redis_password?: string; clear_redis_password?: boolean;
  alist_token?: string; clear_alist_token?: boolean;
};
export interface ConfigState {
  config_path: string; writable: boolean; revision: string; server_id: string;
  config: InstanceConfig; effective: InstanceConfig; restart_fields: string[]; warnings: string[];
}
export const configLabels: Record<string, string> = {
  port: '监听端口', debug: '调试模式', server_name: '服务名称', db_path: '数据库路径', actors_dir: '演员资料目录',
  redis_addr: 'Redis 地址', redis_db: 'Redis 数据库', redis_password: 'Redis 密码',
  disable_library_monitor: '媒体库监控开关', library_monitor_mode: '监控模式', ffprobe_path: 'ffprobe 路径',
  probe_timeout_seconds: '探测超时', probe_concurrency: '探测并发', server_domains: '备用域名',
  ffmpeg_enabled: 'FFmpeg 播放处理', ffmpeg_path: 'FFmpeg 路径', playback_concurrency: '播放处理并发',
  playback_cache_mb: '播放缓存上限',
  alist_url: 'AList 地址', alist_token: 'AList Token', clear_alist_token: '清除 AList Token',
};
export const restartKeys = ['port', 'debug', 'db_path', 'actors_dir', 'redis_addr', 'redis_db', 'redis_password'];
const strings = (value: unknown): string[] => Array.isArray(value) ? value.map(string) : invalid();
function parseConfig(value: unknown): InstanceConfig {
  const v = record(value);
  const mode = string(v.library_monitor_mode);
  if (mode !== 'realtime' && mode !== 'polling') return invalid();
  return {
    port: count(v.port), debug: boolean(v.debug), server_name: string(v.server_name),
    disable_library_monitor: boolean(v.disable_library_monitor), library_monitor_mode: mode,
    db_path: string(v.db_path), actors_dir: string(v.actors_dir), redis_addr: string(v.redis_addr), redis_db: count(v.redis_db),
    redis_password_configured: boolean(v.redis_password_configured), ffprobe_path: string(v.ffprobe_path),
    alist_url: v.alist_url == null ? '' : string(v.alist_url), alist_token_configured: v.alist_token_configured == null ? false : boolean(v.alist_token_configured),
    probe_timeout_seconds: count(v.probe_timeout_seconds), probe_concurrency: count(v.probe_concurrency),
    ffmpeg_enabled: v.ffmpeg_enabled == null ? false : boolean(v.ffmpeg_enabled), ffmpeg_path: v.ffmpeg_path == null ? '' : string(v.ffmpeg_path),
    playback_concurrency: v.playback_concurrency == null ? 2 : count(v.playback_concurrency),
    playback_cache_mb: v.playback_cache_mb == null ? 2048 : count(v.playback_cache_mb),
    server_domains: Array.isArray(v.server_domains) ? v.server_domains.map(item => {
      const d = record(item); return { name: string(d.name), url: string(d.url) };
    }) : invalid(),
  };
}
export function parseConfigState(value: unknown): ConfigState {
  const v = record(value);
  return { config_path: string(v.config_path), writable: boolean(v.writable), revision: string(v.revision), server_id: string(v.server_id),
    config: parseConfig(v.config), effective: parseConfig(v.effective), restart_fields: strings(v.restart_fields), warnings: strings(v.warnings) };
}
export const systemConfigApi = {
  read: async (signal?: AbortSignal) => parseConfigState(await apiClient.request('/api/admin/config', { auth: 'required', signal })),
  save: async (revision: string, config: ConfigPatch) => parseConfigState(await apiClient.request('/api/admin/config', {
    auth: 'required', method: 'PUT', json: { revision, config },
  })),
};
