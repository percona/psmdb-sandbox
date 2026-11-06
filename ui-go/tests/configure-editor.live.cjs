// Safe exploratory checks against a running UI. No save, profile write or
// deployment request can leave the browser. Set UI_LIVE_SOURCE=1 to serve the
// current local scripts/template over the running server's rendered page.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {chromium} = require('playwright');

(async () => {
  const browser = await chromium.launch({headless: true});
  const errors = [], warnings = [], packages = [], blocked = [];
  const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
  page.setDefaultTimeout(90000);
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if (message.type() === 'warning') warnings.push(message.text()); });
  page.on('dialog', dialog => dialog.dismiss());
  page.on('response', async response => {
    const url = new URL(response.url());
    if (url.pathname !== '/api/package-versions') return;
    try {
      const data = await response.json();
      packages.push({product: url.searchParams.get('product'), channel: url.searchParams.get('channel'), count: data.versions?.length ?? data.major_versions?.length ?? 0});
    } catch {}
  });
  await page.route('**/*', async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (!['GET', 'HEAD'].includes(request.method()) && url.pathname !== '/api/hardware/validate') {
      blocked.push(`${request.method()} ${url.pathname}`);
      return route.fulfill({status: 422, json: {error: 'Live UI test: writes are blocked'}});
    }
    if (process.env.UI_LIVE_SOURCE === '1') {
      if (['app.js', 'configure-editor.js', 'environment-profiles.js', 'style.css'].some(file => url.pathname === `/static/${file}`)) {
        const file = path.basename(url.pathname);
        return route.fulfill({path: path.join('static', file), contentType: file.endsWith('.css') ? 'text/css' : 'application/javascript'});
      }
      if (url.pathname === '/configure/gcp') {
        const response = await route.fetch();
        let body = await response.text();
        const template = fs.readFileSync('templates/configure.html', 'utf8');
        // Replace the non-templated helper functions with current source. Keep
        // server-rendered values, HTML, defaults and real API responses intact.
        for (const [from, until] of [
          ['function packageFieldPrefix(', 'function nextClusterName('],
          ['function refreshOptionConflicts(', 'function toggleBackupSettings('],
          ['function showConfigurationError(', '</script>'],
        ]) {
          const start = template.indexOf(from), end = template.indexOf(until, start);
          const renderedStart = body.indexOf(from), renderedEnd = body.indexOf(until, renderedStart);
          assert.ok(start > 0 && end > start && renderedStart > 0 && renderedEnd > renderedStart);
          const source = template.slice(start, end);
          assert.ok(!source.includes('{{'), 'live overlays must not contain unrendered Go template data');
          body = body.slice(0, renderedStart) + source + body.slice(renderedEnd);
        }
        const starterSection = /<section class="form-section" data-editor-group="basics" id="environment-starting-points">[\s\S]*?<\/section>/;
        body = body.replace(starterSection, template.match(starterSection)[0]);
        return route.fulfill({response, body});
      }
    }
    return route.continue();
  });
  try {
    await page.goto('http://127.0.0.1:5001/configure/gcp?template=replica-set', {waitUntil: 'domcontentloaded'});
    await page.waitForLoadState('networkidle', {timeout: 120000});
    await page.getByRole('button', {name: 'Clusters & replica sets', exact: true}).click();
    const block = page.locator('#replsets-container > .component-block').first();
    await block.getByRole('button', {name: 'Edit', exact: true}).click();
    await block.locator('[name="replset_enable_pmm[]"]').check();
    await block.locator('[name="replset_enable_pbm[]"]').check();
    if (process.env.UI_LIVE_ASSERT === '1') {
      const checkPackages = async () => {
        for (const product of ['pbm', 'pmm_client']) {
          const major = block.locator(`[name="replset_${product}_major[]"]`);
          const minor = block.locator(`[name="replset_${product}_version[]"]`);
          await page.waitForFunction(el => el.getAttribute('aria-busy') !== 'true', await major.elementHandle());
          assert.ok(await major.locator('option').count() > 0);
          if (await major.inputValue()) {
            const choices = await major.locator('option').evaluateAll(options => options.map(option => option.value).filter(Boolean));
            await major.selectOption(choices[choices.length - 1]);
            const selected = await major.inputValue();
            assert.ok((await minor.locator('option').evaluateAll(options => options.map(option => option.value).filter(Boolean))).every(value => value.startsWith(selected + '.')));
          }
          assert.equal(await major.isDisabled(), false);
        }
      };
      await checkPackages();
      const audit = block.locator('[name="replset_enable_audit[]"]');
      await audit.check();
      assert.equal(await block.locator('[data-audit-filter-row]').isVisible(), true);
      await block.locator('[name="replset_audit_filter[]"]').fill('{"atype":"authenticate"}');
      const distro = block.locator('[name="replset_mongodb_distribution[]"]');
      for (const distribution of ['community', 'enterprise', 'psmdb']) {
        await distro.selectOption(distribution);
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await audit.isDisabled(), distribution === 'community');
        if (distribution === 'community') {
          assert.equal(await audit.isChecked(), false);
          assert.equal(await block.locator('[data-audit-filter-row]').isVisible(), false);
        }
        const major = block.locator('[name="replset_mongo_release[]"]');
        assert.ok(await major.locator('option').count() > 0);
        const versions = await major.locator('option').evaluateAll(options => options.map(option => option.value));
        if (distribution === 'community' && versions.includes('9.0')) {
          await major.selectOption('9.0');
          assert.equal(await block.locator('[name="replset_enable_pbm[]"]').isChecked(), false);
          assert.equal(await block.locator('[name="replset_enable_pbm[]"]').isDisabled(), true);
        }
        const preferred = distribution === 'psmdb' ? 'psmdb-80' : '8.0';
        if (versions.includes(preferred)) await major.selectOption(preferred);
      }
      await block.locator('[name="replset_enable_pbm[]"]').check();
      for (const product of ['pbm', 'pmm_client']) {
        const repo = block.locator(`[name="replset_${product}_repo[]"]`);
        for (const channel of ['testing', 'experimental', 'release']) {
          await repo.selectOption(channel);
          await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
          const major = block.locator(`[name="replset_${product}_major[]"]`);
          assert.ok(await major.locator('option').count() > 0, `${product}/${channel} must not appear blank`);
          assert.equal(await major.isDisabled(), false);
        }
      }
      await page.getByRole('button', {name: 'Resources', exact: true}).click();
      await page.locator('#hardware_preset').selectOption('atlas-m30');
      await page.locator('#hardware-details-toggle').click();
      await page.locator('#replsetsvr_type').fill('n2-standard-4');
      assert.equal(await page.locator('#hardware_preset').inputValue(), '', 'manual hardware changes must switch to Custom');
      await page.getByRole('button', {name: 'Basics', exact: true}).click();
      await page.locator('#cloud-network-details-toggle').click();
      await page.locator('[name="use_spot_instances"]').check();
      assert.match(await page.locator('#editor-summary-warnings').textContent(), /Spot instances/);
      await page.locator('[name="use_spot_instances"]').uncheck();
      await page.locator('#environment-profiles-tip').focus();
      await page.waitForFunction(() => getComputedStyle(document.getElementById('environment-profiles-tip'), '::after').opacity === '1');
      await page.setViewportSize({width: 390, height: 844});
      if (!await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)) {
        console.log('Mobile overflow diagnostics:', await page.locator('body *').evaluateAll(elements => elements.filter(el => {
          const box = el.getBoundingClientRect();
          return box.width && box.right > innerWidth + 1;
        }).map(el => ({tag: el.tagName, id: el.id, class: el.className, right: Math.round(el.getBoundingClientRect().right), width: Math.round(el.getBoundingClientRect().width)})).slice(0, 30)));
      }
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile form must not overflow');
      console.log('Live GCP checks passed: agent major/minor cascades, repository channels, all distributions, audit/PBM constraints, hardware presets, Spot warnings, tooltip focus and mobile layout.');
    }
    const state = await block.locator('select').evaluateAll(selects => selects.map(select => ({name: select.name, value: select.value, options: select.options.length, disabled: select.disabled})));
    console.log(JSON.stringify({state, packages, errors, warnings, blocked}, null, 2));
    if (process.env.UI_LIVE_ASSERT === '1') {
      for (const name of ['replset_pbm_major[]', 'replset_pmm_client_major[]']) {
        assert.ok(state.find(field => field.name === name)?.options > 0, `${name} must contain versions or an informative fallback`);
      }
      assert.deepEqual(errors, []);
    }
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
