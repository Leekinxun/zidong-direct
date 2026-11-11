import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const assetsDir = path.resolve(__dirname, '../dist/assets');

if (!fs.existsSync(assetsDir)) {
  console.error('dist/assets not found');
  process.exit(1);
}

const forbiddenJsPatterns = [
  { name: 'Array.prototype.toSorted (Chrome 110+)', re: /\.toSorted\s*\(/ },
  { name: 'Array.prototype.toReversed (Chrome 110+)', re: /\.toReversed\s*\(/ },
  { name: 'Array.prototype.toSpliced (Chrome 110+)', re: /\.toSpliced\s*\(/ },
  { name: 'Object.groupBy (Chrome 117+)', re: /\bObject\.groupBy\s*\(/ },
  { name: 'Map.groupBy (Chrome 117+)', re: /\bMap\.groupBy\s*\(/ },
  { name: 'Promise.withResolvers (Chrome 119+)', re: /\bPromise\.withResolvers\s*\(/ },
];

const forbiddenCssPatterns = [
  { name: 'color-mix() (Chrome 111+)', re: /\bcolor-mix\s*\(/ },
  { name: 'oklch() (Chrome 111+)', re: /\boklch\s*\(/ },
  { name: 'oklab() (Chrome 111+)', re: /\boklab\s*\(/ },
  { name: '@starting-style (Chrome 117+)', re: /@starting-style\b/ },
];

const violations = [];
for (const entry of fs.readdirSync(assetsDir)) {
  const fullPath = path.join(assetsDir, entry);
  const content = fs.readFileSync(fullPath, 'utf8');
  if (entry.endsWith('.js')) {
    for (const pat of forbiddenJsPatterns) {
      if (pat.re.test(content)) {
        violations.push(`${entry}: ${pat.name}`);
      }
    }
  } else if (entry.endsWith('.css')) {
    for (const pat of forbiddenCssPatterns) {
      if (pat.re.test(content)) {
        violations.push(`${entry}: ${pat.name}`);
      }
    }
  }
}

if (violations.length > 0) {
  console.error('Win7 browser compatibility check failed (Chrome 109 / Firefox 115):');
  for (const v of violations) {
    console.error(' -', v);
  }
  process.exit(1);
}

console.log('Win7 browser compatibility check passed (Chrome 109 / Firefox 115 safe).');
