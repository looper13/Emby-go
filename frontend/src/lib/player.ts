export interface PlayerOptions { title: string; url: string; type: string; proxyURL?: string }
export interface PlayerHandle { url: string; muted: boolean; video?: HTMLVideoElement; on: (name: string, callback: () => void) => void; play: () => Promise<unknown>; destroy: () => void }
export type PlayerFactory = (stage: HTMLElement, options: PlayerOptions) => PlayerHandle;
export function playbackErrorMessage(code: number | undefined, url: string, protocol = location.protocol) {
  if (protocol === 'https:' && /^http:/i.test(url)) return '播放失败：HTTPS 页面无法加载此 HTTP 视频地址，请使用 HTTPS 源';
  if (code === 2) return '视频加载失败，请检查网络或源地址是否失效';
  if (code === 3) return '视频解码失败，浏览器可能不支持该编码';
  if (code === 4) return '视频源不可用或格式不受支持，请检查源地址和浏览器兼容性';
  return '播放失败，请检查网络、源地址或视频格式';
}
export function managePlayer(stage: HTMLElement, options: PlayerOptions, factory: PlayerFactory, report: (message: string) => void) {
  const player = factory(stage, options); let active = true; let usedProxy = false; let reported = false;
  player.on('error', () => {
    if (!active) return;
    if (!options.proxyURL || usedProxy) {
      if (!reported) report(playbackErrorMessage(player.video?.error?.code, usedProxy ? options.proxyURL! : options.url));
      reported = true; return;
    }
    usedProxy = true; player.url = options.proxyURL; player.muted = true;
    try { void player.play().catch(() => {}); } catch { /* Manual playback remains available. */ }
  });
  return { player, destroy() { if (!active) return; active = false; try { player.destroy(); } catch { /* Dispose the lifetime even if vendor cleanup fails. */ } } };
}
export async function loadPlayerFactory(): Promise<PlayerFactory> {
  const module = await import('../vendor/artplayer.min.js');
  const Constructor = (module.default ?? (window as unknown as { Artplayer: unknown }).Artplayer) as new (options: Record<string, unknown>) => PlayerHandle;
  return (stage, options) => new Constructor({ container: stage, url: options.url, type: options.type, title: options.title,
    autoplay: true, volume: 1, playbackRate: true, aspectRatio: true, fullscreen: true, fullscreenWeb: true,
    setting: true, hotkey: true, pip: true, theme: '#e0a44b', lang: 'zh-cn' });
}
