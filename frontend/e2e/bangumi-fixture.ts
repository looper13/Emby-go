import { DatabaseSync } from 'node:sqlite';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

// Only call with the owned live fixture root. No production archive or API is used.
export async function seedBangumi(root: string, subject: Record<string, unknown>, episode: Record<string, unknown>) {
  const dir = path.join(root, 'anime-catalog');
  await mkdir(dir, { recursive: true });
  const db = new DatabaseSync(path.join(dir, 'catalog-fixture.db'));
  try {
    db.exec(`CREATE TABLE subjects(id INTEGER PRIMARY KEY,name TEXT,original_name TEXT,date TEXT,year INTEGER,season INTEGER,metadata TEXT);
      CREATE TABLE episodes(id INTEGER PRIMARY KEY,subject_id INTEGER,sort REAL,metadata TEXT);
      CREATE TABLE people(kind TEXT,id INTEGER,name TEXT,summary TEXT);
      CREATE TABLE credits(subject_id INTEGER,kind TEXT,person_id INTEGER,role INTEGER,ordinal INTEGER);`);
    const record = { ...subject, id: subject.bangumi_id, year: 2026, season: 4 };
    db.prepare('INSERT INTO subjects VALUES(?,?,?,?,?,?,?)').run(subject.bangumi_id as number, subject.name as string, subject.original_name as string, subject.date as string, 2026, 4, JSON.stringify(record));
    const ep = { ...episode, id: episode.bangumi_id, subject_id: subject.bangumi_id, ep: 1, active: true };
    db.prepare('INSERT INTO episodes VALUES(?,?,?,?)').run(episode.bangumi_id as number, subject.bangumi_id as number, 1, JSON.stringify(ep));
    for (const kind of ['character','person']) {
      db.prepare('INSERT INTO people VALUES(?,?,?,?)').run(kind, 7, kind === 'character' ? '测试角色' : '测试制作人员', '用于检查头像类型隔离的本地数据。');
      db.prepare('INSERT INTO credits VALUES(?,?,?,?,?)').run(subject.bangumi_id as number, kind, 7, 1, 1);
    }
  } finally { db.close(); }
  await writeFile(path.join(dir, 'current.json'), JSON.stringify({
    file: 'catalog-fixture.db', imported_at: new Date().toISOString(), subjects: 1, episodes: 1, characters: 1, persons: 1,
    manifest: { name: 'dump-fixture.zip', browser_download_url: 'https://github.com/bangumi/Archive/releases/download/archive/dump-fixture.zip', size: 1000, digest: `sha256:${'a'.repeat(64)}`, created_at: '2026-10-14T00:00:00Z' },
  }));
  const artwork = new DatabaseSync(path.join(root, 'anime-images', 'images.db'));
  const stillURL = 'https://image.tmdb.org/t/p/w780/fixture.png';
  const keys = [
    ['subject', subject.bangumi_id, 'common', 'poster.png'], ['subject', subject.bangumi_id, 'large', 'poster.png'],
    ['character', 7, 'medium', 'poster.png'], ['person', 7, 'medium', 'poster.png'],
    ['episode', episode.id, `tmdb_${createHash('sha256').update(stillURL).digest('hex')}`, 'fanart.png'],
  ] as const;
  try {
    artwork.exec('PRAGMA busy_timeout=5000');
    for (const [kind, id, variant, file] of keys) {
      const bytes = await readFile(path.join(root, 'media','L005', file));
      const hash = createHash('sha256').update(bytes).digest('hex');
      const files = path.join(root, 'anime-images', 'files', kind, String(id));
      await mkdir(files, { recursive: true }); await writeFile(path.join(files,hash), bytes);
      artwork.prepare('INSERT INTO images VALUES(?,?,?,?,?,?,?,?,?,?,?)').run(`${kind}:${id}:${variant}`, 'ready', 'fixture', hash, 'image/png', bytes.length, 320, 480, Math.floor(Date.now()/1000), 0, '');
    }
  } finally { artwork.close(); }
  const business = new DatabaseSync(path.join(root,'emby-go.db'));
  try {
    business.exec('PRAGMA busy_timeout=5000');
    business.prepare('INSERT INTO anime_tmdb_series VALUES(?,?,?,?)').run(subject.id as number, 55, '本地剧照验收剧集', 1);
    const mapping = { episode_id: episode.id, subject_id: subject.id, series_id: 55, season: 0, number: 3, tmdb_id: 555, name: '特别篇', revision: 1, stills: ['/fixture.png'] };
    business.prepare('INSERT INTO anime_tmdb_episodes VALUES(?,?,?,?,?,?,?)').run(episode.id as number, subject.id as number,55,0,3,JSON.stringify(mapping),1);
  } finally { business.close(); }
}
