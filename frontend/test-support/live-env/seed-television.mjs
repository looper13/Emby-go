import fs from 'node:fs/promises';
import path from 'node:path';

// Files belong to startLiveEnvironment's owned temporary directory only.
export async function seedTelevision(live) {
  const dirs = Object.fromEntries(['tv', 'mixed', 'empty'].map(type => [type, path.join(live.root, type)]));
  for (const dir of Object.values(dirs)) await fs.mkdir(dir);
  const poster = path.join(live.root, 'media', 'L005', 'poster.png');
  const backdrop = path.join(live.root, 'media', 'L005', 'fanart.png');
  for (const [type, title] of [['tv', 'Television Fixture'], ['mixed', 'Mixed Fixture']]) {
    const root = path.join(dirs[type], title);
    await fs.mkdir(root);
    await fs.writeFile(path.join(root, 'tvshow.nfo'), `<tvshow><title>${title}</title><plot>Local television validation fixture.</plot><year>2026</year><genre>Drama</genre></tvshow>`);
    await fs.copyFile(poster, path.join(root, 'poster.png'));
    for (const season of [0, 1, 2, 10]) {
      const dir = path.join(root, season === 0 ? 'Specials' : `Season ${season}`);
      await fs.mkdir(dir);
      await fs.copyFile(poster, path.join(dir, 'poster.png'));
      await fs.copyFile(backdrop, path.join(dir, 'fanart.png'));
      for (const episode of [1, 2]) {
        const name = `Fixture.S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`;
        await fs.writeFile(path.join(dir, `${name}.strm`), `${live.mediaURL}/sample.mp4`);
        await fs.writeFile(path.join(dir, `${name}.nfo`), `<episodedetails><title>${episode === 1 ? 'Zebra' : 'Alpha'} ${season}</title><season>${season}</season><episode>${episode}</episode><runtime>1</runtime></episodedetails>`);
      }
    }
  }
  await fs.writeFile(path.join(dirs.mixed, 'Film.strm'), `${live.mediaURL}/sample.mp4`);
  await fs.writeFile(path.join(dirs.mixed, 'Film.nfo'), '<movie><title>Mixed Film</title><runtime>1</runtime></movie>');
  return dirs;
}
