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
await page.waitForSelector('text=index/landings', { timeout: 30000 }); // the landing on the live feed
await page.screenshot({ path: out('3-dashboard-after-work.png') });
console.log('dashboard: the landed work arrived on the live feed');

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
