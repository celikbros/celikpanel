import { createRequire } from 'node:module';
const require = createRequire(process.argv[2] + '/package.json');
const puppeteer = require('puppeteer-core');
const browser = await puppeteer.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true,
  args: ['--no-first-run', '--no-default-browser-check'], userDataDir: process.argv[2] + '/../vis-profile' });
const a = await browser.newPage();
await a.goto('data:text/html,<p>a</p>');
await a.evaluate(() => { window.__ev = []; document.addEventListener('visibilitychange', () => window.__ev.push(document.visibilityState + '@' + Date.now())); });
const vis = () => a.evaluate(() => [document.visibilityState, document.hasFocus(), window.__ev.join(',')]);
console.log('start', await vis());
const b = await browser.newPage(); await b.goto('data:text/html,<p>b</p>'); await b.bringToFront();
await new Promise(r => setTimeout(r, 500)); console.log('after b front', await vis());
await a.bringToFront(); await new Promise(r => setTimeout(r, 500)); console.log('after a front', await vis());
const s = await a.createCDPSession();
const { windowId } = await s.send('Browser.getWindowForTarget');
try { await s.send('Browser.setWindowBounds', { windowId, bounds: { windowState: 'minimized' } }); await new Promise(r => setTimeout(r, 500)); console.log('minimized', await vis()); } catch (e) { console.log('min err', e.message); }
try { await s.send('Browser.setWindowBounds', { windowId, bounds: { windowState: 'normal' } }); await new Promise(r => setTimeout(r, 500)); console.log('normal', await vis()); } catch (e) { console.log('normal err', e.message); }
const { windowId: wb } = await (await b.createCDPSession()).send('Browser.getWindowForTarget');
console.log('windows', windowId, wb, (await browser.version()));
await browser.close();
