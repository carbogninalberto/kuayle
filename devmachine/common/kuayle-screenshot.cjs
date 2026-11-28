#!/usr/bin/env node
'use strict';

// kuayle-screenshot URL FILE [options]
//
// Takes a screenshot of URL with the image's own Playwright and Chromium
// (in /opt/ms-playwright), whatever Playwright version the project pins, and
// writes it to FILE (PNG, or JPEG for .jpg/.jpeg). Rendering is fixed: viewport,
// device scale factor, locale, time zone, colour scheme, reduced motion and font
// hinting, so the same page gives the same picture on every machine.

const path = require('path');

const usage = `usage: kuayle-screenshot URL FILE [--width N] [--height N] [--scale N]
                         [--full-page] [--wait-for SELECTOR] [--dark] [--timeout MS]

  URL               page to open (http(s)://, file:// or data:)
  FILE              output .png, .jpg or .jpeg (for a pull request, save it in
                    .kuayle/screenshots/ and list it in .kuayle/pull_request.json)
  --width N         viewport width in CSS pixels (default 1280)
  --height N        viewport height in CSS pixels (default 800)
  --scale N         device scale factor (default 1)
  --full-page       capture the whole scrollable page
  --wait-for SEL    wait until SELECTOR is visible before capturing
  --dark            prefer the dark colour scheme
  --timeout MS      navigation and wait timeout (default 30000)`;

function fail(message) {
  process.stderr.write(`kuayle-screenshot: ${message}\n${usage}\n`);
  process.exit(2);
}

function parse(argv) {
  const options = { width: 1280, height: 800, scale: 1, fullPage: false, waitFor: '', dark: false, timeout: 30000 };
  const positional = [];
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) fail(`${arg} needs a value`);
      return argv[++i];
    };
    const number = (name, min, max) => {
      const parsed = Number(value());
      if (!Number.isFinite(parsed) || parsed < min || parsed > max) fail(`${name} must be between ${min} and ${max}`);
      return parsed;
    };
    switch (arg) {
      case '-h':
      case '--help':
        process.stdout.write(`${usage}\n`);
        process.exit(0);
        break;
      case '--width': options.width = Math.round(number('--width', 200, 7680)); break;
      case '--height': options.height = Math.round(number('--height', 200, 4320)); break;
      case '--scale': options.scale = number('--scale', 1, 4); break;
      case '--timeout': options.timeout = Math.round(number('--timeout', 1000, 300000)); break;
      case '--full-page': options.fullPage = true; break;
      case '--dark': options.dark = true; break;
      case '--wait-for': options.waitFor = value(); break;
      default:
        if (arg.startsWith('-')) fail(`unknown option ${arg}`);
        positional.push(arg);
    }
  }
  if (positional.length !== 2) fail('expected URL and FILE');
  const [url, file] = positional;
  if (!/^(https?:|file:|data:)/i.test(url)) fail('URL must start with http://, https://, file:// or data:');
  const extension = path.extname(file).toLowerCase();
  if (!['.png', '.jpg', '.jpeg'].includes(extension)) fail('FILE must end in .png, .jpg or .jpeg');
  return { url, file, type: extension === '.png' ? 'png' : 'jpeg', ...options };
}

// The image installs Playwright globally; a project's own copy (another version,
// with browsers the image does not ship) is never used.
// The image's browsers live in /opt/ms-playwright. It is not the global
// PLAYWRIGHT_BROWSERS_PATH, so a project's own Playwright keeps installing
// its browsers where it always does.
function loadPlaywright() {
  process.env.PLAYWRIGHT_BROWSERS_PATH = process.env.KUAYLE_PLAYWRIGHT_BROWSERS || '/opt/ms-playwright';
  return require(process.env.KUAYLE_PLAYWRIGHT_MODULE || '/usr/local/lib/node_modules/playwright');
}

async function main() {
  const options = parse(process.argv.slice(2));
  const { chromium } = loadPlaywright();
  const browser = await chromium.launch({
    args: ['--font-render-hinting=none', '--disable-lcd-text', '--disable-gpu', '--hide-scrollbars'],
  });
  try {
    const context = await browser.newContext({
      viewport: { width: options.width, height: options.height },
      deviceScaleFactor: options.scale,
      locale: 'en-US',
      timezoneId: 'UTC',
      colorScheme: options.dark ? 'dark' : 'light',
      reducedMotion: 'reduce',
    });
    const page = await context.newPage();
    await page.goto(options.url, { waitUntil: 'networkidle', timeout: options.timeout });
    if (options.waitFor) {
      await page.locator(options.waitFor).first().waitFor({ state: 'visible', timeout: options.timeout });
    }
    await page.evaluate(() => document.fonts && document.fonts.ready);
    await page.screenshot({ path: options.file, type: options.type, fullPage: options.fullPage, animations: 'disabled', caret: 'hide' });
  } finally {
    await browser.close();
  }
  process.stdout.write(`${options.file}\n`);
}

main().catch((error) => {
  process.stderr.write(`kuayle-screenshot: ${error && error.message ? error.message : error}\n`);
  process.exit(1);
});
