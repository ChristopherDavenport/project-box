import { chromium } from 'playwright';

const BASE = process.env.BASE ?? 'http://127.0.0.1:8437';
const TOKEN = process.env.TOKEN ?? 'room-smoke-token';
const out = (n) => process.env.SHOT_DIR + '/' + n;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1080, height: 800 } });

// 1. Dashboard: token via query -> cookie, status card, live feed.
await page.goto(`${BASE}/?token=${TOKEN}`);
await page.waitForSelector('text=Factory status', { timeout: 15000 });
await page.waitForSelector('text=live', { timeout: 15000 });
await page.screenshot({ path: out('1-dashboard.png') });
console.log('dashboard: status card + live feed rendered');

// 2. Submit work through the real form (locators pierce shadow DOM).
await page.click('nav >> text=Submit');
await page.waitForSelector('text=Submit work');
// route: the first configured route is preselected by design
await page.locator('jh-input[label="Occurrence key"] input').fill('ui-' + Date.now());
await page.locator('jh-input[label="Task"] textarea').fill('change requested from the control room');
await page.screenshot({ path: out('2-submit-filled.png') });
await page.locator('jh-button[label="Open ingress"]').click();
await page.waitForSelector('text=/Opened|Already open/', { timeout: 15000 });
console.log('submit: ingress opened from the browser');

// 3. The daemon's own tick does the work; the dashboard shows it live.
await page.goto(BASE + '/'); // deep-load: also exercises the SPA fallback
await page.waitForSelector('text=/Landed:/', { timeout: 30000 }); // the landing on the live feed, in words
await page.screenshot({ path: out('3-dashboard-after-work.png') });
console.log('dashboard: the landed work arrived on the live feed');

// 3b. Sessions: the per-unit live view. This box builds with a shell command
// and never feeds the live bus, so the assertion worth making here is the
// HONEST-SILENCE one — a card with no stream must say so rather than render
// as an agent sitting idle. Seeing a timeline needs a real ACP agent, which
// is the dogfood box's job (hack/dogfood), not this smoke's.
await page.click('nav >> text=Sessions');
await page.waitForSelector('text=/No unit is running|in stage|No live stream here/', { timeout: 15000 });
await page.screenshot({ path: out('3b-sessions.png') });
console.log('sessions: the per-unit view rendered');

// 3c. Theme: the dark tokens are a class on <html>, chosen from the header
// and remembered. The first shell loaded the dark sheet under a media query
// and never set the class, so this is the assertion that dark mode exists.
await page.click('button[title="Dark theme"]');
if (!(await page.evaluate(() => document.documentElement.classList.contains('jh-theme-dark')))) throw new Error('dark theme did not apply');
await page.screenshot({ path: out('3c-dark.png') });
await page.reload();
await page.waitForSelector('text=Sessions');
if (!(await page.evaluate(() => document.documentElement.classList.contains('jh-theme-dark')))) throw new Error('dark theme did not persist across reload');
await page.click('button[title="System theme"]');
console.log('theme: dark applied, persisted, and released');

// 3d. The cord: stop claims from the strip, with the reason the record needs,
// and see who pulled it and why without leaving the page.
await page.click('jh-button[label="Stop claims"]');
await page.locator('box-reason jh-input[label="Reason"] input').fill('smoke: pulling the cord');
await page.click('box-reason jh-button[label="Stop"]');
await page.waitForSelector('text=Claims stopped', { timeout: 15000 });
await page.waitForSelector('text=smoke: pulling the cord', { timeout: 15000 });
await page.screenshot({ path: out('3d-stopped.png') });
await page.click('jh-button[label="Resume claims"]');
await page.locator('box-reason jh-input[label="Reason"] input').fill('smoke: releasing');
await page.click('box-reason jh-button[label="Resume"]');
await page.waitForSelector('text=Claims running', { timeout: 15000 });
console.log('claims: stopped and resumed from the strip, each with its reason');

// 4. Hot reload: a second repo appears in the running daemon...
const res = await page.request.post(`${BASE}/v1/reload`, { headers: { Authorization: `Bearer ${TOKEN}` } });
if (!res.ok()) throw new Error('reload failed: ' + await res.text());
console.log('reload:', (await res.json()).reloaded);

// ...and the UI sees the new route with no daemon restart.
await page.goto(BASE + '/submit');
await page.waitForSelector('jh-radio[value="shop-web"]', { timeout: 15000 });
await page.screenshot({ path: out('4-new-route-after-reload.png') });
console.log('submit: the reloaded route is offered in the form');

await browser.close();
console.log('SMOKE PASS');
