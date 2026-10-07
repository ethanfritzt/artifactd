import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const fixtures = JSON.parse(process.env.ARTIFACTD_BROWSER_FIXTURES || '{}');
let browser;
const failures = [];
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

before(async () => {
  browser = await chromium.launch({
    executablePath: process.env.CHROME_PATH || undefined,
    args: ['--host-resolver-rules=MAP *.artifacts.localhost 127.0.0.1', '--no-proxy-server'],
  });
});
after(async () => {
  await browser?.close();
  assert.deepEqual(failures, [], 'browser errors or CSP violations');
});

async function open(name, options = {}) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, ...options });
  const page = await context.newPage();
  page.on('pageerror', (error) => failures.push(error.message));
  page.on('console', (message) => {
    if (/content security policy|refused to (apply|execute|load)/i.test(message.text())) failures.push(message.text());
  });
  await page.goto(fixtures[name].url);
  await page.locator('.cm-editor').waitFor({ state: 'attached' });
  return page;
}
async function setMode(page, mode) {
  const toggle = page.locator('[data-action="toggle-view"]');
  const editing = await toggle.getAttribute('aria-pressed') === 'true';
  if (editing !== (mode === 'edit')) await toggle.click();
}
async function screenshot(page, name) {
  if (!process.env.ARTIFACTD_SCREENSHOTS) return;
  await mkdir(process.env.ARTIFACTD_SCREENSHOTS, { recursive: true });
  await page.screenshot({ path: process.env.ARTIFACTD_SCREENSHOTS + '/' + name + '.png' });
}
async function edit(page, source) {
  await setMode(page, 'edit');
  await page.locator('#artifactd-md-source').click();
  await page.keyboard.press(mod + '+a');
  await page.keyboard.insertText(source);
}
async function heading(page, text) {
  await page.waitForFunction((text) => document.querySelector('.artifactd-markdown h1')?.textContent === text, text);
}
async function status(page, state) {
  await page.waitForFunction((state) => document.querySelector('.artifactd-md-status').dataset.state === state, state);
}
async function editorText(page) {
  return (await page.locator('.cm-line').allTextContents()).join('\n');
}

// Every case has a separate selected file, preview capability, and browser context.
test('preview-first note workspace, single toggle, draft preservation, and dark mode', async () => {
  const page = await open('views');
  const document = page.locator('.artifactd-markdown');
  assert.ok(await document.isVisible());
  assert.equal(await page.locator('.cm-editor').isVisible(), false);
  assert.equal(await page.locator('[data-action="toggle-view"]').getAttribute('aria-label'), 'Edit document');
  assert.equal(await page.locator('[data-action="split"], [data-width]').count(), 0);
  assert.equal(await document.evaluate((element) => getComputedStyle(element).boxShadow), 'none');
  const source = '# Project notes\n\nA calmer place to read, think, and make a few changes. The document is the workspace—not a sheet of paper inside another application.\n\n## What matters\n\n- Open in **preview** and start reading.\n- Switch to editing without losing your place.\n- Save only when you are ready.\n\n> Keep the interface quiet and the writing in focus.\n\n## Next steps\n\nReview the [project outline](https://example.com) and update the plan.\n\n| Task | Status |\n| --- | --- |\n| Reading experience | Ready |\n| Editor refinements | In progress |\n\n```sh\nartifact preview notes.md --open\n```';
  await edit(page, source);
  await status(page, 'unsaved');
  assert.equal(await page.locator('.cm-gutters').count(), 0);
  assert.ok(await page.locator('.cm-line span').count() > 0, 'syntax highlighting');
  await screenshot(page, 'editor-light');
  await page.keyboard.press(mod + '+e');
  await heading(page, 'Project notes');
  assert.equal(await page.locator('[data-action="toggle-view"]').getAttribute('aria-pressed'), 'false');
  assert.ok(await document.locator('strong').count() > 0);
  await screenshot(page, 'preview-light');
  const previewFont = await document.evaluate((element) => getComputedStyle(element).fontSize);
  const editorFont = await page.locator('.cm-scroller').evaluate((element) => getComputedStyle(element).fontSize);
  assert.equal(editorFont, previewFont);
  assert.equal(await readFile(fixtures.views.path, 'utf8'), '# Saved\n\nOriginal paragraph.\n\n- first item\n- second item');
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
  assert.equal(await document.evaluate((element) => getComputedStyle(element).backgroundColor), 'rgb(38, 38, 40)');
  await screenshot(page, 'preview-dark');
  await page.keyboard.press(mod + '+e');
  assert.ok(await page.locator('.cm-editor').isVisible());
  assert.match(await editorText(page), /Project notes/);
  await screenshot(page, 'editor-dark');
  assert.ok(await page.locator('style[nonce]').count() > 0, 'nonce-scoped generated styles');
  await page.context().close();
});

test('toolbar-free editing retains formatting, undo/redo, search, and keyboard escape', async () => {
  const page = await open('formatting');
  await edit(page, 'hello');
  assert.equal(await page.locator('.artifactd-md-formatting, [data-format]').count(), 0);
  await page.keyboard.press(mod + '+a');
  await page.keyboard.press(mod + '+b');
  assert.equal(await editorText(page), '**hello**');
  await page.keyboard.press(mod + '+z');
  assert.equal(await editorText(page), 'hello');
  await page.keyboard.press(mod + '+Shift+Z');
  assert.equal(await editorText(page), '**hello**');
  await page.keyboard.press(mod + '+f');
  await page.locator('.cm-search input[name="search"]').fill('hello');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.cm-search').count(), 0);
  await edit(page, 'hello');
  await page.keyboard.press(mod + '+a');
  await page.keyboard.press(mod + '+i');
  assert.equal(await editorText(page), '*hello*');
  await edit(page, 'hello');
  await page.keyboard.press(mod + '+End');
  await page.keyboard.type(' typed');
  assert.equal(await editorText(page), 'hello typed');
  await page.keyboard.press(mod + '+Home');
  await page.keyboard.press('Tab');
  assert.match(await editorText(page), /^  hello typed/);
  await page.keyboard.press('Escape');
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#artifactd-md-source').evaluate((element) => document.activeElement === element), false);
  await page.context().close();
});

test('out-of-order draft responses cannot overwrite the latest draft', async () => {
  const page = await open('race');
  let release;
  let firstArrived;
  const started = new Promise((resolve) => { firstArrived = resolve; });
  const held = new Promise((resolve) => { release = resolve; });
  await page.route('**/_artifactd/render', async (route) => {
    const response = await route.fetch();
    if (route.request().postDataJSON().content.includes('Older draft')) {
      firstArrived();
      await held;
    }
    await route.fulfill({ response }).catch(() => {}); // The superseded request may already be aborted.
  });
  await edit(page, '# Older draft');
  await started;
  await edit(page, '# Latest draft');
  await setMode(page, 'preview');
  await heading(page, 'Latest draft');
  release();
  await page.unrouteAll({ behavior: 'wait' });
  assert.equal(await page.locator('.artifactd-markdown h1').textContent(), 'Latest draft');
  await page.context().close();
});

test('keyboard saving preserves view, focus, cursor, scroll, and undo history', async () => {
  const page = await open('save');
  let navigations = 0;
  page.on('framenavigated', () => navigations++);
  await edit(page, '# Saved without reload\n\n' + Array.from({ length: 120 }, (_, i) => 'line ' + i).join('\n\n'));
  await page.keyboard.press(mod + '+End');
  const editor = page.locator('#artifactd-md-source');
  const scroll = await page.locator('.cm-scroller').evaluate((element) => element.scrollTop);
  await page.keyboard.press(mod + '+s');
  await status(page, 'saved');
  assert.equal(navigations, 0);
  assert.ok(await editor.isVisible());
  assert.equal(await editor.evaluate((element) => document.activeElement === element), true);
  assert.equal(await page.locator('.cm-scroller').evaluate((element) => element.scrollTop), scroll);
  await page.keyboard.insertText(' at cursor');
  await status(page, 'unsaved');
  assert.match(await editorText(page), /line 119 at cursor/);
  await page.keyboard.press(mod + '+z');
  await status(page, 'saved');
  await setMode(page, 'preview');
  await heading(page, 'Saved without reload');
  assert.match(await readFile(fixtures.save.path, 'utf8'), /line 119$/);
  await page.locator('.artifactd-markdown').evaluate((element) => { element.scrollTop = 120; });
  await setMode(page, 'edit');
  assert.equal(await page.locator('.cm-scroller').evaluate((element) => element.scrollTop), scroll);
  await setMode(page, 'preview');
  assert.equal(await page.locator('.artifactd-markdown').evaluate((element) => element.scrollTop), 120);
  await page.context().close();
});

test('typing while saving remains an unsaved draft', async () => {
  const page = await open('concurrent');
  let release;
  let arrived;
  const started = new Promise((resolve) => { arrived = resolve; });
  const held = new Promise((resolve) => { release = resolve; });
  await page.route('**/_artifactd/save', async (route) => {
    const response = await route.fetch();
    arrived();
    await held;
    await route.fulfill({ response });
  });
  await edit(page, '# Submitted');
  await page.keyboard.press(mod + '+s');
  await started;
  await page.keyboard.press(mod + '+End');
  await page.keyboard.insertText(' plus later typing');
  release();
  await status(page, 'unsaved');
  assert.equal(await readFile(fixtures.concurrent.path, 'utf8'), '# Submitted');
  await setMode(page, 'preview');
  await heading(page, 'Submitted plus later typing');
  await page.context().close();
});

test('external-change conflicts retain the draft and never overwrite the file', async () => {
  const page = await open('conflict');
  await edit(page, '# My draft');
  await writeFile(fixtures.conflict.path, '# External edit');
  await page.locator('[data-action="save"]').click();
  await status(page, 'error');
  assert.match(await page.locator('.artifactd-md-status').textContent(), /changed since preview opened/);
  assert.equal(await editorText(page), '# My draft');
  assert.equal(await readFile(fixtures.conflict.path, 'utf8'), '# External edit');
  assert.equal(await page.locator('[data-action="save"]').isEnabled(), true);
  await page.context().close();
});

test('mobile uses separate views and contains wide tables/code', async () => {
  const page = await open('mobile', { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  assert.equal(await page.locator('[data-action="split"], [data-width]').count(), 0);
  const columns = Array.from({ length: 20 }, (_, i) => 'Column ' + i);
  const source = '# Mobile\n\n|' + columns.join('|') + '|\n|' + columns.map(() => '---').join('|') + '|\n|' + columns.join('|') + '|\n\n```\n' + 'code'.repeat(500) + '\n```';
  await edit(page, source);
  await setMode(page, 'preview');
  await heading(page, 'Mobile');
  assert.ok(await page.locator('.artifactd-markdown table').isVisible());
  assert.equal(await page.locator('.artifactd-markdown table').evaluate((element) => element.scrollWidth > element.clientWidth), true);
  assert.equal(await page.locator('.artifactd-markdown th').first().evaluate((element) => getComputedStyle(element).overflowWrap), 'normal');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await screenshot(page, 'preview-mobile');
  await page.setViewportSize({ width: 1440, height: 1000 });
  await setMode(page, 'edit');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('body.artifactd-md-editing').waitFor();
  assert.ok(await page.locator('.cm-editor').isVisible());
  assert.equal(await page.locator('.artifactd-markdown').isVisible(), false);
  await screenshot(page, 'editor-mobile');
  await page.context().close();
});

test('printing and DOCX keep the saved baseline and restore the draft', async () => {
  const page = await open('print');
  await edit(page, '# New saved baseline');
  await page.keyboard.press(mod + '+s');
  await status(page, 'saved');
  await edit(page, '# Unsaved printing draft');
  await setMode(page, 'preview');
  await heading(page, 'Unsaved printing draft');
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  await heading(page, 'New saved baseline');
  await page.emulateMedia({ media: 'print' });
  assert.equal(await page.locator('.cm-editor').isVisible(), false);
  assert.equal(await page.locator('.artifactd-md-shell').isVisible(), false);
  assert.ok(await page.locator('.artifactd-markdown').isVisible());
  assert.equal(await page.locator('.artifactd-markdown').evaluate((element) => getComputedStyle(element).boxShadow), 'none');
  await page.emulateMedia({ media: 'screen' });
  await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
  await heading(page, 'Unsaved printing draft');
  assert.equal(await editorText(page), '# Unsaved printing draft');
  const exportResponse = await page.request.get(fixtures.print.url + '/_artifactd/document.docx');
  assert.equal(exportResponse.status(), 200);
  assert.match(exportResponse.headers()['content-type'], /wordprocessingml/);
  assert.equal(await readFile(fixtures.print.path, 'utf8'), '# New saved baseline');
  await page.context().close();
});

test('large documents stay editable with virtualized lines', async () => {
  const page = await open('large');
  await setMode(page, 'edit');
  assert.ok(await page.locator('.cm-line').count() < 200, 'editor virtualizes large documents');
  await page.locator('#artifactd-md-source').click();
  await page.keyboard.press(mod + '+End');
  await page.keyboard.insertText('\n# End of large draft');
  await status(page, 'unsaved');
  await page.keyboard.press(mod + '+s');
  await status(page, 'saved');
  assert.match(await readFile(fixtures.large.path, 'utf8'), /# End of large draft$/);
  await page.context().close();
});

test('render and save failures keep edits, and unsafe draft HTML stays disabled', async () => {
  const page = await open('errors');
  await page.route('**/_artifactd/render', (route) => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"Test render failure"}' }));
  await edit(page, '# Safe draft\n\n<script>window.compromised=true</script>\n\n[bad](javascript:alert(1))');
  await setMode(page, 'preview');
  await status(page, 'error');
  assert.match(await page.locator('.artifactd-md-status').textContent(), /Test render failure/);
  await page.unroute('**/_artifactd/render');
  await setMode(page, 'edit');
  await setMode(page, 'preview');
  await heading(page, 'Safe draft');
  assert.equal(await page.evaluate(() => window.compromised), undefined);
  assert.equal(await page.locator('.artifactd-markdown script, .artifactd-markdown a[href^="javascript:"]').count(), 0);
  await page.route('**/_artifactd/save', (route) => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"Test save failure"}' }));
  await page.locator('[data-action="save"]').click();
  await status(page, 'error');
  assert.match(await page.locator('.artifactd-md-status').textContent(), /Test save failure/);
  await setMode(page, 'edit');
  assert.match(await editorText(page), /Safe draft/);
  await page.context().close();
});

test('leaving with unsaved changes warns without discarding the draft', async () => {
  const page = await open('navigation');
  await edit(page, '# Keep my draft');
  let warned = false;
  page.on('dialog', async (dialog) => {
    warned = dialog.type() === 'beforeunload';
    await dialog.dismiss();
  });
  await page.reload({ timeout: 1000 }).catch(() => {}); // Dismissing beforeunload intentionally cancels navigation.
  assert.equal(warned, true);
  assert.equal(await editorText(page), '# Keep my draft');
  await page.context().close();
});
