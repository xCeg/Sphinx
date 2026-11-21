// Renders the README's terminal captures from real sphinx output, and the
// social preview, with the Chromium bundled with Playwright.
//
//   go build -o sphinx ./cmd/sphinx && make site
//   node scripts/screenshots/shoot.js
const { chromium } = require('playwright');
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..', '..');
const out = path.join(root, 'assets');

const palette = { 31: '#ff6b5b', 32: '#7ed491', 33: '#f2c14e', 35: '#d68cf0', 36: '#7cc7d9' };

function ansiToHTML(text) {
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  let html = '';
  let open = 0;
  for (const part of text.split(/(\x1b\[[0-9;]*m)/)) {
    const m = part.match(/^\x1b\[([0-9;]*)m$/);
    if (!m) { html += esc(part); continue; }
    while (open) { html += '</span>'; open--; }
    const codes = m[1].split(';').map(Number).filter((c) => c);
    if (!codes.length) continue;
    const style = codes.map((c) => c === 1 ? 'font-weight:700' : c === 2 ? 'opacity:.55' : palette[c] ? `color:${palette[c]}` : '').filter(Boolean).join(';');
    html += `<span style="${style}">`;
    open++;
  }
  while (open) { html += '</span>'; open--; }
  return html;
}

function run(args, cwd = root) {
  try {
    return execFileSync(path.join(root, 'sphinx'), args, { cwd, env: { ...process.env, FORCE_COLOR: '1', NO_COLOR: '' } }).toString();
  } catch (e) {
    return e.stdout.toString();
  }
}

function terminal(command, output, width) {
  return `<!doctype html><meta charset="utf-8"><style>
  body{margin:0;background:transparent;font:15px/1.5 "DejaVu Sans Mono",monospace;}
  .win{width:${width}px;background:#12151f;color:#e6dfcf;border-radius:12px;overflow:hidden;box-shadow:0 12px 40px #0006;margin:24px}
  .top{background:#1d2740;padding:10px 14px;display:flex;gap:8px;align-items:center}
  .dot{width:12px;height:12px;border-radius:50%;background:#3a4560}
  .title{color:#9aa3b8;font-size:13px;margin-left:10px}
  pre{margin:0;padding:18px 22px;white-space:pre;font:inherit}
  .prompt{color:#c9a04e}
  </style><div class="win"><div class="top"><span class="dot"></span><span class="dot"></span><span class="dot"></span><span class="title">sphinx</span></div>
  <pre><span class="prompt">$</span> ${command}\n${ansiToHTML(output)}</pre></div>`;
}

(async () => {
  const b = await chromium.launch();
  const shots = [
    { file: 'demo.png', cmd: ['cropped-screenshot.png', 'cv.pdf', 'redacted-memo.pdf', 'shared-folder.zip', '--min-severity', 'high'], width: 900 },
    { file: 'reveal.png', cmd: ['reveal', '-o', '/tmp/sphinx-revealed', 'redacted-memo.pdf', 'holiday.jpg'], width: 900 },
  ];
  for (const s of shots) {
    const html = terminal('sphinx ' + s.cmd.join(' ').replace('/tmp/sphinx-revealed', 'revealed'), run(s.cmd, path.join(root, 'site', 'examples')).replace(/\/tmp\/sphinx-revealed/g, 'revealed'), s.width);
    const p = await b.newPage({ deviceScaleFactor: 2, viewport: { width: s.width + 48, height: 400 } });
    await p.setContent(html);
    await p.locator('.win').screenshot({ path: path.join(out, s.file), omitBackground: true });
    await p.close();
  }
  run(['reveal', '-o', '/tmp/sphinx-revealed', 'site/examples/cropped-screenshot.png']);
  fs.copyFileSync('/tmp/sphinx-revealed/cropped-screenshot.uncropped.png', path.join(out, 'uncropped.png'));
  const p = await b.newPage({ viewport: { width: 1280, height: 640 } });
  await p.goto('file://' + path.join(__dirname, 'social.html'));
  await p.waitForTimeout(300);
  await p.screenshot({ path: path.join(out, 'social-preview.png') });
  fs.copyFileSync(path.join(out, 'social-preview.png'), path.join(root, 'site', 'og.png'));
  await b.close();
})();
