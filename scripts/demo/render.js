// Renders the HTML sources of the demo files with the Chromium bundled
// with Playwright: node scripts/demo/render.js OUTDIR
const { chromium } = require('playwright');
const path = require('path');
(async () => {
  const out = process.argv[2];
  const here = __dirname;
  const b = await chromium.launch();
  const shoot = async (html, file, w, h) => {
    const p = await b.newPage({ viewport: { width: w, height: h } });
    await p.goto('file://' + path.join(here, html));
    await p.screenshot({ path: path.join(out, file) });
    await p.close();
  };
  await shoot('screenshot.html', 'screenshot.png', 1080, 2000);
  await shoot('photo.html', 'photo.png', 1600, 1066);
  const p = await b.newPage();
  await p.goto('file://' + path.join(here, 'memo.html'));
  await p.pdf({ path: path.join(out, 'memo.pdf'), format: 'A4', printBackground: true });
  await p.goto('file://' + path.join(here, 'cv.html'));
  await p.pdf({ path: path.join(out, 'cv.pdf'), format: 'A4', printBackground: true });
  await b.close();
})();
