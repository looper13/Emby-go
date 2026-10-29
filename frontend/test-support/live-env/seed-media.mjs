import fs from 'node:fs/promises';
import path from 'node:path';
import { deflateSync } from 'node:zlib';

const xml = text => text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
const crc32 = buffer => {
  let crc = 0xffffffff;
  for (const byte of buffer) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
};
const chunk = (type, data) => {
  const label = Buffer.from(type);
  const size = Buffer.alloc(4);
  const crc = Buffer.alloc(4);
  size.writeUInt32BE(data.length);
  crc.writeUInt32BE(crc32(Buffer.concat([label, data])));
  return Buffer.concat([size, label, data, crc]);
};

function poster(width, height, seed) {
  const rows = Buffer.alloc(height * (1 + width * 3));
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const offset = y * (1 + width * 3) + 1 + x * 3;
    const band = Math.floor(x / 32) + Math.floor(y / 64);
    rows[offset] = (seed * 31 + band * 25) % 180 + 40;
    rows[offset + 1] = (seed * 17 + band * 31) % 160 + 50;
    rows[offset + 2] = (seed * 13 + band * 21) % 170 + 40;
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))]);
}

export async function seedMedia(root, mediaURL, count = 307) {
  if (!Number.isInteger(count) || count < 307 || count > 10000) throw new Error('Fixture count must be 307 through 10000');
  if ((await fs.readdir(root)).length) throw new Error('Seed target must be a new empty directory');
  for (let i = 1; i <= count; i++) {
    const number = String(i).padStart(3, '0');
    const code = `ABF-${number}`;
    const dir = path.join(root, `L${number}`);
    await fs.mkdir(dir);
    const source = [304, 305].includes(i) ? `ftp://127.0.0.1/fixture/${code}.mp4` : `${mediaURL}/${i === 306 ? 'failure' : i === 299 ? 'redirect' : i === 298 ? 'delay' : 'sample'}.mp4`;
    await fs.writeFile(path.join(dir, `${code}.strm`), `${source}\n`);
    if (i >= 301 && i <= 303) continue;
    const title = i === 42 ? `${code} \u300a\u7279\u6b8a\u7b26\u53f7\u300b&"'<>` : i % 10 === 3 || i === 5 || i === 55 ? `${code} \u6df1\u591c\u7684\u6d4b\u8bd5\u5f71\u7247` : `${code} Sample Movie`;
    const rare = [3, 5, 43, 55, 83, 123, 163, 203, 243, 283].includes(i);
    await fs.writeFile(path.join(dir, `${code}.nfo`), `<?xml version="1.0" encoding="UTF-8"?>\n<movie><num>${code}</num><title>${xml(title)}</title><originaltitle>${xml(title)}</originaltitle><plot>Isolated fixture ${code}. \u4e2d\u6587\u5267\u60c5\u4e0e\u7f3a\u56fe\u5206\u652f\u3002</plot><year>${2015 + i % 10}</year><premiered>2020-01-01</premiered><runtime>${60 + i % 90}</runtime><rating>7.5</rating><genre>Drama</genre><tag>Tag-A</tag><studio>Studio A</studio><actor><name>Actor One</name></actor>${rare ? '<tag>Rare series</tag><actor><name>\u6f14\u5458\u4e59</name></actor>' : ''}</movie>\n`);
    if ([5, 55, 88, 142, 217, 299].includes(i) || (i > 307 && i % 100 === 0)) {
      await fs.writeFile(path.join(dir, 'poster.png'), poster(320, 480, i));
      await fs.writeFile(path.join(dir, 'fanart.png'), poster(960, 540, i));
    }
  }
}
