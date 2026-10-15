import Hls from 'hls.js';

interface HlsPlayer {
  isDestroy?: boolean;
  on: (event: string, callback: () => void) => void;
  emit: (event: string) => void;
  notice: { show: string };
  option: { autoplay: boolean };
  play: () => Promise<unknown>;
}

// One handler per player, shared by its direct and proxy URLs. Switching URLs
// cancels the old requests/worker; a single destroy listener owns the lifetime.
export function createHlsPlayback() {
  let current: Hls | undefined;
  let registered = false;
  let disposed = false;
  function release() {
    const previous = current;
    current = undefined;
    previous?.destroy();
  }
  return (video: HTMLVideoElement, url: string, art: HlsPlayer) => {
    release();
    // Artplayer defers customType until a microtask; the dialog may already
    // have closed before the first handler invocation registers its listener.
    if (disposed || art.isDestroy) return;
    if (!registered) {
      registered = true;
      art.on('destroy', () => { disposed = true; release(); });
    }
    if (!Hls.isSupported()) {
      if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = url;
      } else {
        art.notice.show = '当前浏览器不支持 HLS 播放，请更换浏览器';
      }
      return;
    }
    const hls = new Hls();
    current = hls;
    let recovered = false;
    hls.on(Hls.Events.MEDIA_ATTACHED, () => {
      // Start after MediaSource is attached. Relying on video.autoplay alone
      // can leave streams whose first buffered timestamp is > 0 paused at 0.
      if (current === hls && art.option.autoplay) void art.play().catch(() => {});
    });
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (current !== hls || !data.fatal) return;
      if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !recovered) {
        recovered = true;
        hls.recoverMediaError();
        return;
      }
      // hls.js has already exhausted its load retries. Feed fatal errors into
      // the existing single proxy fallback/reporting path instead of looping.
      release();
      art.emit('error');
    });
    hls.loadSource(url);
    hls.attachMedia(video);
  };
}
