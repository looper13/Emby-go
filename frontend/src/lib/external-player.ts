export const externalPlayers = [
  { id: 'potplayer', name: 'PotPlayer' },
  { id: 'vlc', name: 'VLC' },
  { id: 'mpv', name: 'MPV' },
] as const;
export type ExternalPlayer = typeof externalPlayers[number]['id'];

export function externalPlayerURL(player: ExternalPlayer, raw: string): string {
  // Pass a single network URL, never command-line options or account credentials.
  if (!externalPlayers.some(entry => entry.id === player) || !/^https?:\/\//iu.test(raw) || /[\s"<>`\u0000-\u001f\u007f]/u.test(raw)) {
    throw new Error('外部播放地址无效');
  }
  const url = new URL(raw);
  if (!url.hostname) throw new Error('外部播放地址无效');
  return `${player}://${url.href}`;
}
