// Regenerate the raster app icons from public/favicon.svg.
//
//   npm run icons
//
// Produces the PNG sizes used by Android installs, iOS home screens and the
// legacy 32px favicon, then wraps the 32px PNG in a single-entry .ico.
import sharp from 'sharp';
import { writeFile } from 'node:fs/promises';

const source = 'public/favicon.svg';

const targets = [
  [32, 'public/favicon-32.png'],
  [180, 'public/apple-touch-icon.png'],
  [192, 'public/icon-192.png'],
  [512, 'public/icon-512.png'],
];

for (const [size, out] of targets) {
  await sharp(source, { density: 384 })
    .resize(size, size, { fit: 'contain', background: { r: 11, g: 15, b: 20, alpha: 0 } })
    .png({ compressionLevel: 9 })
    .toFile(out);
  console.log(`wrote ${out} (${size}x${size})`);
}

// ICO container with one 32x32 PNG-compressed entry (supported by every
// browser that still asks for /favicon.ico).
const png32 = await sharp(source, { density: 384 })
  .resize(32, 32, { fit: 'contain', background: { r: 11, g: 15, b: 20, alpha: 0 } })
  .png({ compressionLevel: 9 })
  .toBuffer();

const header = Buffer.alloc(6);
header.writeUInt16LE(0, 0); // reserved
header.writeUInt16LE(1, 2); // type: icon
header.writeUInt16LE(1, 4); // image count

const entry = Buffer.alloc(16);
entry.writeUInt8(32, 0); // width
entry.writeUInt8(32, 1); // height
entry.writeUInt8(0, 2); // palette colours
entry.writeUInt8(0, 3); // reserved
entry.writeUInt16LE(1, 4); // colour planes
entry.writeUInt16LE(32, 6); // bits per pixel
entry.writeUInt32LE(png32.length, 8); // payload size
entry.writeUInt32LE(22, 12); // payload offset

await writeFile('public/favicon.ico', Buffer.concat([header, entry, png32]));
console.log('wrote public/favicon.ico (32x32)');
