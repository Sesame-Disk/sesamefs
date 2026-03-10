#!/usr/bin/env node
const sharp = require(require('path').join(__dirname, '..', 'mobile-frontend', 'node_modules', 'sharp'));
const path = require('path');

const sizes = [72, 96, 128, 144, 152, 192, 384, 512];
const inputPath = path.join(__dirname, '..', 'mobile-frontend', 'public', 'logo.png');
const outputDir = path.join(__dirname, '..', 'mobile-frontend', 'public', 'icons');

async function generate() {
  for (const size of sizes) {
    const outputPath = path.join(outputDir, `icon-${size}x${size}.png`);
    await sharp(inputPath)
      .resize(size, size, { fit: 'contain', background: { r: 245, g: 245, b: 245, alpha: 1 } })
      .png()
      .toFile(outputPath);
    console.log(`Generated ${size}x${size}`);
  }
}

generate().catch(err => { console.error(err); process.exit(1); });
