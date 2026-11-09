// Run through TestConfigureEditorBrowser; all HTML and APIs are intercepted.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    for (const platform of ['docker', 'aws', 'gcp', 'azure', 'chaos']) {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      const page = await context.newPage();
      const errors = [];
      let payload;
      let profilePayload;
      let profiles = [];
      let deleteRequests = 0;
      let failDelete = false;
      let failProfileList = false;
      let holdPmmRequests = false;
      let failExperimental = true;
      let dismissNextDialog = false;
      let latestPSMDB = false;
      const heldPmmRequests = [];
      page.on('pageerror', error => errors.push(error.message));
      const handleDialog = dialog => {
        if (dismissNextDialog) {
          dismissNextDialog = false;
          return dialog.dismiss();
        }
        return dialog.accept();
      };
      page.on('dialog', handleDialog);
      await context.route('**/*', async route => {
        const url = new URL(route.request().url());
        if (url.pathname === '/') return route.fulfill({ contentType: 'text/html', path: path.join(process.argv[2], 'index.html') });
        if (url.pathname.startsWith('/configure/')) {
          let body = fs.readFileSync(path.join(process.argv[2], `${platform}.html`), 'utf8');
          if (url.searchParams.has('legacy')) body = body.replace('<script src="/static/configure-editor.js"></script>', '');
          return route.fulfill({ contentType: 'text/html', body });
        }
        if (url.pathname.startsWith('/static/')) {
          const file = path.basename(url.pathname);
          if (!['app.js', 'configure-editor.js', 'environment-profiles.js', 'style.css'].includes(file)) return route.abort();
          return route.fulfill({ path: path.join('static', file), contentType: file.endsWith('.css') ? 'text/css' : 'application/javascript' });
        }
        if (url.pathname === '/api/environment') {
          payload = route.request().postDataJSON();
          return route.fulfill({ status: 422, json: { error: 'Test-only save intercepted' } });
        }
        if (url.pathname === '/api/environment-profiles') {
          if (route.request().method() === 'POST') {
            profilePayload = route.request().postDataJSON();
            const profile = { ...profilePayload, id: 'testprofile' };
            profiles.push(profile);
            return route.fulfill({ status: 201, json: profile });
          }
          if (failProfileList) return route.fulfill({ status: 500, json: { error: 'Profile list unavailable' } });
          return route.fulfill({ json: { profiles } });
        }
        if (url.pathname === '/api/environment-profiles/testprofile') {
          assert.equal(route.request().method(), 'DELETE');
          deleteRequests++;
          if (failDelete) return route.fulfill({ status: 500, json: { error: 'Could not delete environment profile' } });
          profiles = [];
          return route.fulfill({ json: { ok: true } });
        }
        if (url.pathname.startsWith('/api/regions/')) return route.fulfill({ json: { regions: ['us-east-1', 'us-central1', 'eastus'] } });
        if (url.pathname.startsWith('/api/images/')) return route.fulfill({ json: { images: {
          'Debian 12': [{id: 'debian-test', name: 'Debian'}],
          'Ubuntu 24.04 LTS': [{id: 'ubuntu24-test', name: 'Ubuntu 24.04'}],
        } } });
        if (url.pathname === '/api/package-versions') {
          const product = url.searchParams.get('product');
          const channel = url.searchParams.get('channel');
          if (product === 'pmm_client' && holdPmmRequests) {
            return new Promise(resolve => heldPmmRequests.push(async () => {
              await route.fulfill({json: {versions: ['3.10.0', '3.9.0', '2.44.0']}});
              resolve();
            }));
          }
          if (product === 'pmm_client' && channel === 'testing') return route.fulfill({json: {versions: []}});
          if (product === 'pmm_client' && channel === 'experimental') return failExperimental
            ? route.fulfill({status: 503, json: {error: 'Repository temporarily unavailable'}})
            : route.fulfill({json: {versions: ['3.11.0-rc1']}});
          if (product === 'pmm_client') return route.fulfill({json: {versions: ['3.10.0', '3.9.0', '2.44.0']}});
          if (product === 'pbm') return route.fulfill({json: {versions: ['2.11.0', '2.10.0', '1.9.0']}});
          const official = ['community', 'enterprise'].includes(url.searchParams.get('distribution'));
          if (!official && latestPSMDB) return route.fulfill({json: {
            major_versions: ['psmdb-83', 'psmdb-80'],
            minor_versions: {'psmdb-83': ['8.3.2-1', '8.3.1-1'], 'psmdb-80': ['8.0.12-4']},
          }});
          return route.fulfill({ json: {
            versions: ['8.0.12-4'], major_versions: official ? ['8.0', '7.0'] : ['psmdb-80'],
            minor_versions: official ? {'8.0': ['8.0.12'], '7.0': ['7.0.18']} : {'psmdb-80': ['8.0.12-4']},
          } });
        }
        if (url.pathname === '/api/pmm-server-image-tags') return route.fulfill({ json: { tags: ['3.10.0'] } });
        return route.fulfill({ json: {} });
      });

      async function load(legacy = false) {
        await page.goto(`http://sandbox.test/configure/${platform}${legacy ? '?legacy' : ''}`);
        await page.waitForLoadState('networkidle');
        assert.deepEqual(errors, [], `${platform}: JavaScript errors`);
      }
      async function save() {
        payload = undefined;
        await page.evaluate(() => document.getElementById('config-form').requestSubmit());
        await page.waitForFunction(() => !document.querySelector('#config-form [type="submit"]').disabled);
        // Wait for the intercepted request, rather than a live backend response.
        await page.waitForFunction(() => document.body.textContent.includes('Test-only save intercepted'));
        assert.ok(payload, `${platform}: save must submit a payload`);
        return structuredClone(payload);
      }
      async function panel(name) {
        await page.locator(`#config-editor-nav button[aria-controls="editor-panel-${name}"]`).click();
      }

      await load(true);
      // The legacy alert does not write an error message into the DOM.
      await page.evaluate(() => { window.alert = message => { document.getElementById('editor-errors').textContent = message; }; });
      const legacyPayload = await save();
      await load();
      assert.equal(await page.locator('.customize-body, .package-block-body, .compute-block-body').evaluateAll(elements => elements.every(el => el.style.display === 'none')), true, 'customize and advanced panels must start collapsed');
      assert.equal(await page.locator('#editor-panel-profiles').isVisible(), true, 'Templates must be the default when opening the configuration editor');
      assert.equal(await page.locator('#editor-panel-basics').isVisible(), false);
      assert.equal(await page.getByRole('button', { name: 'Templates', exact: true }).getAttribute('aria-current'), 'page');
      assert.equal(await page.getByRole('button', { name: 'Templates', exact: true }).count(), 1);
      assert.equal(await page.locator('#editor-panel-profiles #environment-starting-points').isVisible(), true);
      assert.equal(await page.getByText('Starting points are optional.', { exact: false }).count(), 0);
      assert.equal(await page.getByRole('button', { name: 'Use starting point', exact: true }).count(), 0);
      assert.equal(await page.locator('#enable-environment-profile').isChecked(), false);
      assert.equal(await page.locator('#environment-profile-name').isVisible(), false);
      assert.equal(await page.locator('#save-environment-profile').isVisible(), false);
      await page.locator('#enable-environment-profile').check();
      assert.equal(await page.locator('#environment-profile-name').isVisible(), true);
      assert.equal(await page.locator('#save-environment-profile').isVisible(), true);
      assert.equal(await page.locator('#environment-profile-name').evaluate(el => el === document.activeElement), true);
      await page.locator('#enable-environment-profile').uncheck();
      assert.equal(await page.locator('#environment-profile-name').isVisible(), false);
      assert.equal(await page.locator('#save-environment-profile').isVisible(), false);
      assert.equal(await page.locator('#environment-starter').inputValue(), '');
      assert.equal(await page.locator('#environment-profile-to-delete, #delete-environment-profile').count(), 0, 'profile management belongs in home Settings');
      assert.equal(await page.locator('#environment-starter').getAttribute('required'), null);
      assert.equal(await page.locator('#environment-starting-points p').filter({hasText: 'Profiles are shared by this sandbox UI'}).count(), 0, 'profile help belongs in a tooltip, not a paragraph');
      await page.locator('#environment-profiles-tip').focus();
      await page.waitForFunction(() => getComputedStyle(document.getElementById('environment-profiles-tip'), '::after').opacity === '1');
      await panel('basics');
      assert.equal(await page.locator('#config-form').evaluate(el => getComputedStyle(el).display), 'grid');
      const navBox = await page.locator('#config-editor-navigation').boundingBox();
      const panelBox = await page.locator('#config-editor-panels').boundingBox();
      assert.ok(panelBox.x > navBox.x + navBox.width, 'desktop navigation must be beside the editor');
      assert.equal(await page.locator('.editor-panel:visible').count(), 1);
      assert.equal(await page.locator('#editor-panel-basics').isVisible(), true);
      assert.equal(await page.locator('[data-editor-service]:visible').count(), 0);
      assert.equal(await page.locator('#editor-summary-topology').textContent(), '1 cluster · 1 replica set · 14 MongoDB nodes (including arbiters and routers)');
      if (['aws', 'gcp'].includes(platform)) assert.equal(await page.locator('#machine_image').inputValue(), 'ubuntu24-test');
      const preservedImages = await page.evaluate(() => {
        const select = document.createElement('select');
        const groups = {'Ubuntu 24.04 LTS': [{id: 'ubuntu24-test'}], 'Debian 12': [{id: 'debian-test'}]};
        return ['debian-test', 'saved-unlisted-image', '__custom__'].map(value => {
          renderImageOptions(select, groups, value);
          return select.value;
        });
      });
      assert.deepEqual(preservedImages, ['debian-test', 'saved-unlisted-image', '__custom__']);
      if (platform === 'gcp') {
        const imageOptions = await page.evaluate(() => {
          const select = document.createElement('select');
          const id = 'https://www.googleapis.com/compute/v1/projects/ubuntu-os-cloud/global/images/ubuntu-2404-test';
          const groups = {'Ubuntu 24.04 LTS': [{id, name: 'ubuntu-2404-test'}]};
          const options = [];
          for (const images of [groups, {'Ubuntu 24.04 LTS': [{id}]}, {}, {'Debian 12': [{id: 'debian-test'}]}]) {
            renderImageOptions(select, images, id);
            options.push({value: select.value, label: select.selectedOptions[0].textContent});
          }
          return {id, options};
        });
        assert.deepEqual(imageOptions.options, Array(4).fill({value: imageOptions.id, label: 'ubuntu-2404-test'}));
      }
      await panel('security');
      const securityHeadings = await page.locator('#editor-panel-security > section h2').allTextContents();
      assert.match(securityHeadings.at(-1), /^Credentials/);
      assert.equal(securityHeadings.includes('LDAP'), true);
      assert.equal(await page.getByLabel('Configure LDAP authentication', {exact: false}).count(), 1);
      assert.equal(await page.getByLabel('Enable encryption at rest (PSMDB)', {exact: false}).count(), 1);
      assert.equal(await page.getByText('Default MongoDB credentials are in use. Test environments only.', {exact: true}).count(), 0);
      assert.deepEqual(await save(), legacyPayload, `${platform}: organizing the form must not change its save payload`);

      await panel('mongodb');
      assert.equal(await page.getByRole('button', { name: 'Clusters & replica sets', exact: true }).count(), 1);
      const cluster = page.locator('#clusters-container > .component-block').first();
      assert.equal(await cluster.locator('.editor-topology-summary').isVisible(), true);
      assert.equal(await cluster.locator('.component-feature-row').isVisible(), false);
      await cluster.getByRole('button', { name: 'Edit', exact: true }).click();
      assert.equal(await cluster.locator('.package-block-body').isVisible(), false, 'editing a topology must not automatically expand advanced settings');
      assert.equal(await cluster.locator('.component-feature-row').isVisible(), true);
      const essential = platform === 'docker' ? '[data-docker-version="distribution"]' : '[name="cluster_mongodb_distribution[]"]';
      assert.equal(await cluster.locator(essential).isVisible(), false, 'distribution belongs in advanced settings');
      assert.equal(await cluster.locator('.package-block-body').locator(essential).count(), 1);
      const version = platform === 'docker' ? '[data-docker-version="minor"]' : '[name="cluster_mongo_version[]"]';
      assert.equal(await cluster.locator(version).isVisible(), true);
      assert.equal(await cluster.locator('.component-block-header [name="cluster_env_tag[]"]').isVisible(), true);
      assert.equal(await cluster.locator('.component-feature-row [name="cluster_enable_audit[]"]').isVisible(), true);
      assert.equal(await cluster.locator('fieldset').getByText('Topology', { exact: true }).count(), 1);
      assert.equal(await cluster.locator('fieldset [name="cluster_configsvr_count[]"]').isVisible(), true);
      if (platform !== 'docker') assert.equal(await cluster.locator('.package-block-body [name="cluster_mongo_repo[]"]').count(), 1);
      if (platform === 'chaos') {
        assert.equal(await cluster.locator('.package-block-body [name="cluster_os_image[]"]').count(), 1);
        assert.equal(await page.locator('[name="arbiter_cpu_cores"]').count(), 1, 'CHAOS compute fields must not be duplicated');
      }
      await cluster.locator('[name="cluster_shard_count[]"]').fill('3');
      await cluster.getByRole('button', { name: '▲ Collapse', exact: true }).first().click();
      await page.waitForFunction(() => document.getElementById('editor-summary-topology').textContent.includes('17 MongoDB nodes'));
      assert.match(await cluster.locator('.editor-topology-summary').textContent(), /3 shards/);

      // Dynamically added blocks receive the same controls and live summaries.
      latestPSMDB = true;
      await page.getByRole('button', { name: '+ Add Replica Set', exact: true }).click();
      assert.deepEqual(errors, [], `${platform}: adding a topology`);
      assert.equal(await page.locator('#replsets-container > .component-block').count(), 2);
      const replset = page.locator('#replsets-container > .component-block').last();
      const distro = replset.locator(platform === 'docker' ? '[data-docker-version="distribution"]' : '[name="replset_mongodb_distribution[]"]');
      const major = replset.locator(platform === 'docker' ? '[data-docker-version="major"]' : '[name="replset_mongo_release[]"]');
      const minor = replset.locator(platform === 'docker' ? '[data-docker-version="minor"]' : '[name="replset_mongo_version[]"]');
      await page.waitForLoadState('networkidle');
      assert.equal(await distro.inputValue(), 'psmdb');
      await page.waitForFunction(el => !el.disabled, await major.elementHandle());
      assert.equal(await major.isDisabled(), false);
      assert.equal(await minor.isDisabled(), false);
      assert.equal(await major.inputValue(), platform === 'docker' ? '8.0' : 'psmdb-83');
      assert.equal(await minor.inputValue(), platform === 'docker' ? '8.0.12-4' : '8.3.2-1');
      assert.equal(await distro.isVisible(), false);
      await replset.locator('.package-toggle-btn').click();
      const audit = replset.locator('[name="replset_enable_audit[]"]');
      await audit.check();
      if (platform !== 'docker') {
        await distro.selectOption('community');
        await page.waitForFunction(el => !el.disabled, await major.elementHandle());
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await major.inputValue(), '8.0');
        assert.equal(await minor.inputValue(), '8.0.12');
        assert.equal(await audit.isDisabled(), true);
        assert.equal(await audit.isChecked(), false, 'Community must turn off unsupported audit logging');
        await distro.selectOption('psmdb');
        await page.waitForFunction(el => !el.disabled, await major.elementHandle());
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await major.inputValue(), 'psmdb-80', 'switching distributions preserves the corresponding major when available');
        assert.equal(await minor.inputValue(), '8.0.12-4');
        assert.equal(await audit.isDisabled(), false, 'switching back to PSMDB restores audit availability');
      }
      await replset.locator('[name="replset_arbiters[]"]').fill('0');
      await replset.locator('[name="replset_use_tls[]"]').check();
      await page.waitForFunction(() => document.getElementById('enable_ca').checked);
      await page.waitForFunction(() => document.getElementById('editor-summary-warnings').textContent.includes('preferTLS'));
      await replset.getByRole('button', { name: 'Remove', exact: true }).click();
      latestPSMDB = false;
      assert.equal(await page.locator('#replsets-container > .component-block').count(), 1);

      await panel('services');
      assert.equal(await page.getByRole('button', { name: 'Optional services', exact: true }).count(), 1);
      assert.equal(await page.locator('.editor-service-chooser p').count(), 0, 'service guidance belongs in a tooltip, not a paragraph');
      await page.locator('#editor-service-tip').focus();
      await page.waitForFunction(() => getComputedStyle(document.getElementById('editor-service-tip'), '::after').opacity === '1');
      assert.match(await page.locator('#editor-service-tip').getAttribute('data-tip'), /Choosing a service enables it and reveals its settings/);
      assert.equal(await page.getByRole('button', { name: 'Hide unused service' }).count(), 0);
      await page.locator('#editor-service-choice').selectOption('monitoring');
      const monitoring = page.locator('[data-editor-service="monitoring"]');
      assert.equal(await monitoring.isVisible(), true);
      const monitoringToggle = page.locator(platform === 'docker' ? '#enable-pmm-docker' : '#enable_pmm');
      assert.equal(await monitoringToggle.evaluate(el => el === document.activeElement), true, 'the revealed service toggle must receive focus');
      assert.equal(await monitoringToggle.isChecked(), true, 'selecting a service must enable it');
      await page.waitForFunction(() => document.getElementById('editor-summary-services').textContent.includes('PMM monitoring'));
      await monitoringToggle.uncheck();
      assert.equal(await monitoring.isVisible(), false);
      assert.equal(await page.locator('#editor-service-choice').evaluate(el => el === document.activeElement), true, 'hiding the disabled service must return focus to the chooser');
      assert.equal(await page.locator('#editor-service-choice option[value="monitoring"]').isDisabled(), false);
      await page.locator('#editor-service-choice').selectOption('monitoring');
      assert.equal(await monitoring.isVisible(), true, 'a disabled service can be selected again');
      assert.equal(await monitoringToggle.isChecked(), true);
      await monitoringToggle.uncheck();
      {
        await page.locator('#editor-service-choice').selectOption('clients');
        assert.equal(await page.locator('#editor-enable-clients').isChecked(), true);
        assert.equal(await page.locator('#bare-clients-container .bare-client-row').count(), 1);
        assert.equal(await monitoringToggle.isChecked(), true, 'selecting clients must enable their PMM dependency');
        const clientsPayload = await save();
        assert.equal(Object.keys(clientsPayload.config.pmm_bare_clients).length, 1);
        dismissNextDialog = true;
        await monitoringToggle.click();
        assert.equal(await monitoringToggle.isChecked(), true, 'canceling dependency removal must keep the service enabled');
        assert.equal(await monitoring.isVisible(), true);
        assert.equal(await page.locator('#bare-clients-container .bare-client-row').count(), 1);
        await page.locator('#editor-enable-clients').uncheck();
        assert.equal(await page.locator('[data-editor-service="clients"]').isVisible(), false);
        assert.equal(await page.locator('#bare-clients-container .bare-client-row').count(), 0);
        assert.equal(await page.locator('#editor-service-choice option[value="clients"]').isDisabled(), false);
        await page.locator('#editor-service-choice').selectOption('clients');
        await page.locator('#bare-clients-container [onclick="removeBareClientRow(this)"]').click();
        assert.equal(await page.locator('[data-editor-service="clients"]').isVisible(), false, 'removing the last client hides the service');
        assert.equal(await page.locator('#editor-enable-clients').isChecked(), false);
        if (await monitoringToggle.isChecked()) await monitoringToggle.uncheck();
      }
      for (const [key, toggleID] of [
        ['backups', platform === 'docker' ? '#enable-seaweedfs-docker' : '#enable_backup'],
        ['sync', '#enable_pcsm'],
      ]) {
        await page.locator('#editor-service-choice').selectOption(key);
        assert.equal(await page.locator(toggleID).isChecked(), true, `${key}: selecting a service checks its control`);
        assert.equal(await page.locator(`[data-editor-service="${key}"]`).isVisible(), true);
        if (key === 'backups') assert.equal(await cluster.locator('[name="cluster_enable_pbm[]"]').isChecked(), true, 'backup dependencies must still be enabled');
        await page.locator(toggleID).uncheck();
        assert.equal(await page.locator(`[data-editor-service="${key}"]`).isVisible(), false);
        assert.equal(await page.locator(`#editor-service-choice option[value="${key}"]`).isDisabled(), false);
      }
      await page.locator('#editor-service-choice').selectOption('load');
      assert.equal(await page.locator('#enable_plgm').isChecked(), true);
      await panel('basics');
      const modifiedPayload = await save();
      assert.equal(modifiedPayload.config.enable_plgm, true, 'hidden panel values must survive saving');
      assert.equal(modifiedPayload.config.clusters.cl01.shard_count, 3);

      payload = undefined;
      await panel('profiles');
      await page.locator('#enable-environment-profile').check();
      await page.locator('#environment-profile-name').fill('Reusable test lab');
      await page.locator('#save-environment-profile').click();
      await page.waitForFunction(() => document.getElementById('environment-profile-status').textContent.includes('saved without credentials'));
      assert.equal(payload, undefined, 'saving a profile must not save an environment');
      assert.equal(profilePayload.name, 'Reusable test lab');
      assert.equal(profilePayload.platform, platform);
      assert.equal(profilePayload.config.clusters.cl01.shard_count, 3);
      assert.equal(await page.locator('#environment-starter').inputValue(), 'profile:testprofile');
      const beforeDeleteURL = page.url();
      const beforeDeleteForm = await page.locator('#config-form').evaluate(form => Array.from(new FormData(form).entries()));
      const settingsPage = await context.newPage();
      settingsPage.on('dialog', handleDialog);
      settingsPage.on('pageerror', error => errors.push(error.message));
      await settingsPage.goto('http://sandbox.test/');
      await settingsPage.getByRole('button', { name: 'Settings', exact: true }).click();
      await settingsPage.locator('.settings-tab[data-tab="profiles"]').click();
      await settingsPage.waitForFunction(() => !document.getElementById('environment-profile-to-delete').disabled);
      assert.equal(await settingsPage.locator('#environment-profile-to-delete option[value="testprofile"]').textContent(), `Reusable test lab (${platform.toUpperCase()})`);
      assert.equal(await settingsPage.locator('#delete-environment-profile').isDisabled(), true);
      await settingsPage.locator('#environment-profile-to-delete').selectOption('testprofile');
      dismissNextDialog = true;
      await settingsPage.locator('#delete-environment-profile').click();
      assert.equal(deleteRequests, 0, 'cancel must not delete a profile');
      assert.equal(profiles.length, 1);
      failDelete = true;
      await settingsPage.locator('#delete-environment-profile').click();
      await settingsPage.waitForFunction(() => document.getElementById('settings-profile-status').textContent === 'Could not delete environment profile');
      assert.equal(profiles.length, 1, 'failed deletion must preserve the profile');
      assert.equal(await settingsPage.locator('#delete-environment-profile').isEnabled(), true, 'failed deletion can be retried');
      failDelete = false;
      await settingsPage.locator('#delete-environment-profile').click();
      await settingsPage.waitForFunction(() => document.getElementById('settings-profile-status').textContent === 'Profile deleted.');
      assert.equal(profiles.length, 0);
      assert.equal(await settingsPage.locator('#environment-profile-to-delete').isDisabled(), true);
      assert.equal(await settingsPage.locator('#delete-environment-profile').isDisabled(), true);
      assert.equal(page.url(), beforeDeleteURL, 'deletion must not navigate');
      assert.deepEqual(await page.locator('#config-form').evaluate(form => Array.from(new FormData(form).entries())), beforeDeleteForm, 'deletion must preserve unsaved configuration');
      await settingsPage.getByRole('button', { name: 'Cancel', exact: true }).click();
      profiles = [{ id: 'otherprofile', name: 'Other platform lab', platform: platform === 'chaos' ? 'docker' : 'chaos' }];
      await settingsPage.goto('http://sandbox.test/?settings=profiles');
      await settingsPage.waitForFunction(() => !document.getElementById('environment-profile-to-delete').disabled);
      assert.equal(await settingsPage.locator('#environment-profile-to-delete option[value="otherprofile"]').count(), 1, 'Settings must list profiles across platforms');
      failProfileList = true;
      await settingsPage.locator('.settings-tab[data-tab="global"]').click();
      await settingsPage.locator('.settings-tab[data-tab="profiles"]').click();
      await settingsPage.waitForFunction(() => document.getElementById('settings-profile-status').textContent.includes('Profile list unavailable'));
      assert.equal(await settingsPage.locator('#delete-environment-profile').isDisabled(), true);
      failProfileList = false;
      profiles = [];
      await settingsPage.locator('.settings-tab[data-tab="global"]').click();
      await settingsPage.locator('.settings-tab[data-tab="profiles"]').click();
      await settingsPage.waitForFunction(() => document.getElementById('settings-profile-status').textContent === 'No saved profiles.');
      assert.equal(await settingsPage.locator('#environment-profile-to-delete').isDisabled(), true);
      assert.equal(await settingsPage.locator('#delete-environment-profile').isDisabled(), true);
      await settingsPage.close();

      if (platform !== 'docker') {
        await panel('mongodb');
        await cluster.getByRole('button', { name: 'Edit', exact: true }).click();
        const pmmMajor = cluster.locator('[name="cluster_pmm_client_major[]"]');
        const pmmMinor = cluster.locator('[name="cluster_pmm_client_version[]"]');
        const pmmRepo = cluster.locator('[name="cluster_pmm_client_repo[]"]');
        await cluster.locator('[name="cluster_enable_pmm[]"]').check();
        await cluster.locator('[name="cluster_enable_pbm[]"]').check();
        // Open advanced settings if the saved/default configuration collapsed it.
        if (!await pmmMajor.isVisible()) await cluster.locator('.package-toggle-btn').click();
        assert.deepEqual(await pmmMajor.locator('option').evaluateAll(options => options.map(option => option.value)), ['3', '2']);
        const pbmMajor = cluster.locator('[name="cluster_pbm_major[]"]');
        const pbmMinor = cluster.locator('[name="cluster_pbm_version[]"]');
        assert.deepEqual(await pbmMajor.locator('option').evaluateAll(options => options.map(option => option.value)), ['2', '1']);
        await pbmMajor.selectOption('1');
        assert.equal(await pbmMinor.inputValue(), '1.9.0');
        await pmmMajor.selectOption('2');
        await pmmMinor.selectOption('2.44.0');
        await pmmRepo.dispatchEvent('change');
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await pmmMajor.inputValue(), '2', 'refresh must preserve a pinned older major');
        assert.equal(await pmmMinor.inputValue(), '2.44.0');
        await pmmRepo.selectOption('testing');
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.match(await pmmMajor.locator('option').first().textContent(), /No versions found/);
        assert.equal(await pmmMajor.isDisabled(), false, 'empty metadata must not strand a disabled selector');
        payload = undefined;
        await page.locator('#config-form [type="submit"]').click();
        assert.equal(payload, undefined, 'an enabled component with no packages must not silently lose its version pin');
        assert.match(await page.locator('#editor-errors').textContent(), /No package versions/);
        await pmmRepo.selectOption('release');
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        await pmmMajor.selectOption('2');
        await pmmMinor.selectOption('2.44.0');
        await pmmRepo.selectOption('experimental');
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await pmmMinor.inputValue(), '2.44.0', 'failed requests must retain the existing pin');
        assert.match(await pmmMajor.locator('..').locator('.package-version-status').textContent(), /Could not refresh/);
        assert.equal(await pmmMajor.isDisabled(), false);
        failExperimental = false;
        await pmmMajor.locator('..').getByRole('button', {name: 'Retry', exact: true}).click();
        await page.waitForFunction(el => el.dataset.versionLoadState === 'ready', await pmmMajor.elementHandle());
        assert.equal(await pmmMinor.inputValue(), '3.11.0-rc1', 'retry must use the selected channel, not fall back to release');
        await pmmRepo.selectOption('release');
        await page.locator('#versions-loading-modal').waitFor({state: 'hidden'});
        assert.equal(await pmmMajor.locator('..').locator('.package-version-status').isVisible(), false, 'successful retry clears the error');
        await panel('basics');
      }

      // A hidden, collapsed invalid field must be revealed before native focus.
      await page.evaluate(() => {
        const field = document.querySelector('[name="cluster_configsvr_count[]"]');
        field.value = '99';
        field.dispatchEvent(new Event('input', { bubbles: true }));
      });
      payload = undefined;
      await page.locator('#config-form [type="submit"]').click();
      assert.equal(payload, undefined, 'invalid form must not save');
      assert.equal(await page.locator('[name="cluster_configsvr_count[]"]').isVisible(), true);
      assert.equal(await page.locator('#editor-errors').isVisible(), true);
      await page.locator('#editor-errors').getByRole('button', { name: 'Go to setting' }).click();
      assert.equal(await page.locator('[name="cluster_configsvr_count[]"]').evaluate(el => el === document.activeElement), true);
      assert.deepEqual(errors, [], `${platform}: JavaScript errors after editing`);

      if (platform === 'gcp') {
        // Slow initial .component-block loads overlap a user .customize-body
        // channel change. The stale response must not restore release versions
        // or leave the newest dropdown disabled.
        holdPmmRequests = true;
        await page.goto(`http://sandbox.test/configure/${platform}`, {waitUntil: 'domcontentloaded'});
        await page.waitForFunction(() => document.querySelector('[name="replset_pmm_client_major[]"]')?.getAttribute('aria-busy') === 'true');
        const major = page.locator('[name="replset_pmm_client_major[]"]');
        assert.ok(await major.locator('option').count() > 0, 'slow loads must show cached majors or loading text, never a blank dropdown');
        payload = undefined;
        await page.locator('#config-form [type="submit"]').click();
        assert.equal(payload, undefined);
        assert.match(await page.locator('#editor-errors').textContent(), /still loading/);
        holdPmmRequests = false;
        await page.evaluate(() => {
          const repo = document.querySelector('[name="replset_pmm_client_repo[]"]');
          repo.value = 'testing';
          repo.dispatchEvent(new Event('change', {bubbles: true}));
        });
        await page.waitForFunction(() => document.querySelector('[name="replset_pmm_client_major[]"]')?.dataset.versionLoadState === 'empty');
        await Promise.all(heldPmmRequests.splice(0).map(release => release()));
        await page.waitForLoadState('networkidle');
        assert.equal(await major.isDisabled(), false);
        assert.match(await major.locator('option').first().textContent(), /No versions found/);
        assert.equal(await major.inputValue(), '', 'late release responses must not overwrite the testing channel');
      }

      // Narrow screens retain navigation and avoid horizontal overflow.
      await panel('basics');
      if (await page.locator('#cloud-network-details-toggle').count() && !await page.locator('#cloud-network-details').isVisible()) await page.locator('#cloud-network-details-toggle').click();
      await page.setViewportSize({ width: 390, height: 844 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, `${platform}: mobile overflow`);
      await panel('profiles');
      await page.locator('#environment-profiles-tip').focus();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, `${platform}: profile tooltip mobile overflow`);
      await page.locator('#environment-starter').selectOption('template:replica-set');
      await page.waitForURL(`**/configure/${platform}?template=replica-set`);
      await page.waitForLoadState('networkidle');
      await panel('profiles');
      assert.equal(await page.locator('#environment-starter').inputValue(), 'template:replica-set');
      profiles = [{ id: 'testprofile', name: 'Saved test lab', platform, config: {} }];
      await page.reload();
      await page.waitForLoadState('networkidle');
      await panel('profiles');
      await page.locator('#environment-starter').selectOption('profile:testprofile');
      await page.waitForURL(`**/configure/${platform}?profile=testprofile`);
      await page.waitForLoadState('networkidle');
      await panel('profiles');
      assert.equal(await page.locator('#environment-starter').inputValue(), 'profile:testprofile');
      assert.equal(await page.locator('#delete-environment-profile').count(), 0);
      console.log(`${platform}: navigation, version selection, topology layout, payload parity, profiles, templates, dependencies, validation and mobile layout passed`);
      await context.close();
    }
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
