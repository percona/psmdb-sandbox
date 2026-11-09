// Progressive enhancement: keep the same form controls and save payload. Hiding a
// panel never disables its fields or discards configuration in another section.
(() => {
  'use strict';

  function countLabel(count, label) {
    return `${count} ${label}${count === 1 ? '' : 's'}`;
  }

  function topologySummary(topology) {
    const number = (key, fallback) => {
      const value = topology[key];
      return value === '' || value == null ? fallback : Number(value);
    };
    let nodes, description;
    if (topology.kind === 'cluster') {
      const shards = number('shards', 2);
      const members = number('members', 2);
      const arbiters = number('arbiters', 1);
      const configs = number('configs', 3);
      const routers = number('routers', 2);
      nodes = shards * (members + arbiters) + configs + routers;
      description = `${countLabel(shards, 'shard')} × ${countLabel(members, 'data node')} + ${countLabel(arbiters, 'arbiter')} · ${countLabel(configs, 'config server')} · ${countLabel(routers, 'router')}`;
    } else {
      const members = number('members', 2);
      const arbiters = number('arbiters', 1);
      nodes = members + arbiters;
      description = `${countLabel(members, 'data node')} · ${countLabel(arbiters, 'arbiter')}`;
    }
    const features = [
      `Monitoring ${topology.monitoring ? 'on' : 'off'}`,
      `Backups ${topology.backups ? 'on' : 'off'}`,
      `TLS ${topology.tls ? 'on' : 'off'}`,
    ];
    if (topology.search) features.push('Search on');
    return { nodes, description, features: features.join(' · ') };
  }

  // Used by the dependency-free Node regression tests.
  if (typeof module !== 'undefined') module.exports = { topologySummary };
  if (typeof document === 'undefined') return;

  document.addEventListener('DOMContentLoaded', () => {
    const form = document.getElementById('config-form');
    if (!form) return;
    const platform = form.dataset.platform;
    const panelsHost = document.getElementById('config-editor-panels');
    const navigation = document.getElementById('config-editor-navigation');
    const nav = document.getElementById('config-editor-nav');
    const errors = document.getElementById('editor-errors');
    const groups = [
      ['profiles', 'Templates'],
      ['basics', 'Basics'], ['mongodb', 'Clusters & replica sets'], ['resources', 'Resources'],
      ['services', 'Optional services'], ['security', 'Security'], ['advanced', 'Advanced'],
    ];
    const panels = new Map();
    const navButtons = new Map();
    const revealedServices = new Set();
    const services = {
      monitoring: { label: 'PMM monitoring', toggle: platform === 'docker' ? 'enable-pmm-docker' : 'enable_pmm', help: 'PMM agents need the monitoring server. Select agents in each MongoDB topology; enabling Docker monitoring enables agents on all current topologies.' },
      clients: { label: 'PMM clients without MongoDB', toggle: 'editor-enable-clients', help: 'Adding a client also enables the required PMM server. Unchecking this service removes its clients.' },
      backups: { label: 'PBM backups', toggle: platform === 'docker' ? 'enable-seaweedfs-docker' : 'enable_backup', help: 'Enabling backups enables PBM agents on all current topologies and configures backup storage. Disable individual agents in MongoDB if needed.' },
      load: { label: 'Load generator', toggle: 'enable_plgm', help: 'Adds a dedicated plgm host. Configure workloads from its Web UI after deployment.' },
      sync: { label: 'ClusterSync', toggle: 'enable_pcsm', help: 'Requires two replica sets or two sharded clusters. Starting replication replaces selected target collections.' },
    };
    const serviceSections = Array.from(form.querySelectorAll('[data-editor-service]'));
    const topologySelector = '#clusters-container > .component-block, #replsets-container > .component-block';

    // Encryption belongs with the other security settings, not the name field.
    const securitySection = document.createElement('section');
    securitySection.className = 'form-section';
    securitySection.dataset.editorGroup = 'security';
    const securityTitle = document.createElement('h2');
    securityTitle.textContent = 'Encryption at rest';
    securitySection.append(securityTitle, document.getElementById('encryption-settings'), document.getElementById('vault-psmdb-warning'));
    const credentialsSection = document.getElementById('credentials-body').closest('section');
    panelsHost.insertBefore(securitySection, credentialsSection);

    for (const [key, label] of groups) {
      const sections = Array.from(form.querySelectorAll(`[data-editor-group="${key}"]`));
      if (!sections.length) continue;
      const panel = document.createElement('div');
      panel.id = `editor-panel-${key}`;
      panel.className = 'editor-panel';
      panel.setAttribute('role', 'region');
      panel.setAttribute('aria-label', label);
      panel.dataset.editorPanel = key;
      panel.append(...sections);
      panelsHost.append(panel);
      panels.set(key, panel);
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'btn btn-outline editor-nav-button';
      button.textContent = label;
      button.setAttribute('aria-controls', panel.id);
      button.addEventListener('click', () => {
        showPanel(key);
        panelsHost.scrollIntoView({ block: 'start' });
      });
      nav.append(button);
      navButtons.set(key, button);
    }

    function showPanel(key) {
      for (const [name, panel] of panels) {
        panel.hidden = name !== key;
        const button = navButtons.get(name);
        if (name === key) button.setAttribute('aria-current', 'page');
        else button.removeAttribute('aria-current');
      }
    }

    const serviceChooser = document.createElement('section');
    serviceChooser.className = 'form-section editor-service-chooser';
    const chooserLabel = document.createElement('label');
    chooserLabel.htmlFor = 'editor-service-choice';
    chooserLabel.textContent = 'Add an optional service';
    const chooser = document.createElement('select');
    chooser.id = 'editor-service-choice';
    chooser.add(new Option('Choose a service…', ''));
    for (const section of serviceSections) {
      const key = section.dataset.editorService;
      chooser.add(new Option(services[key].label, key));
      const help = document.createElement('p');
      help.className = 'field-hint editor-service-help';
      help.textContent = services[key].help;
      section.insertBefore(help, section.children[1] || null);
      if (key === 'clients') {
        // Clients are enabled by the presence of rows, rather than a stored
        // boolean. Reuse their existing add/remove handlers and dependencies.
        const label = document.createElement('label');
        label.className = 'toggle-label';
        const toggle = document.createElement('input');
        toggle.type = 'checkbox';
        toggle.id = services.clients.toggle;
        toggle.addEventListener('input', () => {
          if (toggle.checked) section.querySelector('[onclick="onAddBareClientClick(this)"]').click();
          else section.querySelectorAll('.bare-client-row [onclick="removeBareClientRow(this)"]').forEach(button => button.click());
        });
        label.append(toggle, ' Include PMM clients');
        section.querySelector('.section-header').append(label);
      }
    }
    const chooserHelp = document.createElement('button');
    chooserHelp.type = 'button';
    chooserHelp.id = 'editor-service-tip';
    chooserHelp.className = 'field-tip';
    chooserHelp.textContent = 'ⓘ';
    const chooserHelpText = 'Choosing a service enables it and reveals its settings. Uncheck it to disable it and hide its settings. Search / Vector Search is configured under Clusters & replica sets. TLS, encryption and LDAP are under Security.';
    chooserHelp.dataset.tip = chooserHelpText;
    chooserHelp.setAttribute('aria-label', chooserHelpText);
    chooserLabel.append(' ', chooserHelp);
    const chooserGroup = document.createElement('div');
    chooserGroup.className = 'form-group';
    chooserGroup.append(chooserLabel, chooser);
    serviceChooser.append(chooserGroup);
    panels.get('services').prepend(serviceChooser);
    chooser.addEventListener('change', () => {
      const key = chooser.value;
      if (!key) return;
      chooser.value = '';
      const target = document.getElementById(services[key].toggle);
      if (!target.checked) target.click();
      refresh();
      if (serviceEnabled(key)) target.focus();
      else chooser.focus();
    });

    function serviceEnabled(key) {
      if (key === 'clients') return form.querySelectorAll('#bare-clients-container .bare-client-row').length > 0;
      return !!document.getElementById(services[key].toggle)?.checked;
    }

    function topologyData(block) {
      const prefix = block.parentElement.id === 'clusters-container' ? 'cluster' : 'replset';
      const field = key => block.querySelector(`[name="${prefix}_${key}[]"]`);
      const value = key => field(key)?.value;
      const enabled = key => !!field(key)?.checked;
      let version;
      if (platform === 'docker') {
        const image = field('psmdb_image');
        version = image?.value === '__custom__'
          ? image.parentElement.querySelector('.custom-image-input')?.value
          : image?.selectedOptions[0]?.textContent.trim();
      } else {
        const distribution = { psmdb: 'PSMDB', community: 'Community', enterprise: 'Enterprise' }[value('mongodb_distribution') || 'psmdb'];
        version = `${distribution} ${(value('mongo_release') || '').replace('psmdb-', '').replace(/^(\d)(\d)$/, '$1.$2')}`;
      }
      if (block.querySelector('.editor-topology-essentials select[required]:invalid')) version = 'Choose a distribution, major and minor version';
      return {
        kind: prefix, version: version || 'PSMDB (default image)',
        shards: value('shard_count'), members: value(prefix === 'cluster' ? 'shardsvr_replicas' : 'data_nodes'),
        arbiters: value(prefix === 'cluster' ? 'arbiters_per_replset' : 'arbiters'),
        configs: value('configsvr_count'), routers: value('mongos_count'),
        monitoring: enabled('enable_pmm'), backups: enabled('enable_pbm'), tls: enabled('use_tls'), search: enabled('enable_mongot'),
      };
    }

    function enhanceTopology(block) {
      if (block.querySelector('.editor-topology-summary')) return;
      const body = block.querySelector('.customize-body');
      const prefix = block.parentElement.id === 'clusters-container' ? 'cluster' : 'replset';
      const field = key => block.querySelector(`[name="${prefix}_${key}[]"]`);
      // The CHAOS controls used to be repeated inside each topology despite
      // having environment-wide names. The Resources panel now owns them once.
      if (platform === 'chaos') {
        body.querySelectorAll('.compute-block-body').forEach(compute => {
          compute.previousElementSibling?.remove();
          compute.remove();
        });
      }
      const summary = document.createElement('div');
      summary.className = 'editor-topology-summary';
      for (let i = 0; i < 3; i++) summary.append(document.createElement('p'));
      block.insertBefore(summary, body);
      const essentials = document.createElement('div');
      essentials.className = 'form-row editor-topology-essentials';
      // Keep fields inside .customize-body: package/image helpers use that scope.
      for (const suffix of ['mongo_release', 'mongo_version']) {
        const control = field(suffix);
        if (control) {
          const group = control.closest('.form-group');
          control.id = `editor-${suffix}-${++topologyID}`;
          group.querySelector('label').htmlFor = control.id;
          essentials.append(group);
        }
      }
      const header = block.querySelector('.component-block-header');
      header.classList.add('editor-topology-header');
      const env = field('env_tag')?.closest('.form-group');
      if (env) {
        env.className = 'block-name-field';
        env.querySelector('label').className = 'block-name-label';
        env.querySelector('input').className = 'block-name-input';
        header.insertBefore(env, header.querySelector('.block-header-actions'));
      }
      const topology = document.createElement('fieldset');
      topology.className = 'editor-topology-fields';
      const legend = document.createElement('legend');
      legend.textContent = 'Topology';
      const counts = document.createElement('div');
      counts.className = 'form-row';
      const countFields = prefix === 'cluster'
        ? ['configsvr_count', 'mongos_count', 'shard_count', 'shardsvr_replicas', 'arbiters_per_replset']
        : ['data_nodes', 'arbiters'];
      countFields.forEach(key => counts.append(field(key).closest('.form-group')));
      topology.append(legend, counts);
      const features = block.querySelector('.component-feature-row');
      features.append(field('enable_audit').closest('.form-group'));
      let advanced = body.querySelector('.package-block-body');
      let advancedHeader = advanced?.previousElementSibling;
      if (!advanced) {
        advanced = document.createElement('div');
        advanced.className = 'package-block-body';
        advanced.style.display = 'none';
        advancedHeader = document.createElement('div');
        advancedHeader.className = 'subsection-header';
        const title = document.createElement('span');
        title.className = 'subsection-title';
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'btn btn-outline btn-sm package-toggle-btn';
        button.textContent = '▼ Customize';
        button.addEventListener('click', () => window.togglePackageBlock(button));
        advancedHeader.append(title, button);
      }
      advancedHeader.querySelector('.subsection-title').textContent = 'Advanced Settings & Packages';
      // OS, repository channels, custom images/ports and audit filters stay in
      // the advanced section. Preserve the original nodes and their handlers.
      for (const child of Array.from(body.children)) {
        if ([advanced, advancedHeader].includes(child)) continue;
        if (child.matches('.form-row') && !child.querySelector('input,select,textarea')) child.remove();
        else advanced.append(child);
      }
      body.replaceChildren(essentials, topology, features, advancedHeader, advanced);
      if (platform === 'docker') setupDockerVersions(block, essentials, advanced);
      const toggle = block.querySelector('[onclick="toggleCustomizeBlock(this)"]');
      body.id = `editor-topology-${++topologyID}`;
      toggle.setAttribute('aria-controls', body.id);
    }

    function choice(select, label) {
      select.dataset.requireChoice = 'true';
      select.required = true;
      select.insertBefore(new Option(label, ''), select.firstChild);
      select.value = '';
    }

    function setupDockerVersions(block, essentials, advanced) {
      const image = block.querySelector('[name$="_psmdb_image[]"]');
      const controls = {};
      for (const [key, label] of [['distribution', 'Distribution'], ['major', 'Major version'], ['minor', 'Minor version']]) {
        const group = document.createElement('div');
        group.className = 'form-group';
        const text = document.createElement('label');
        const select = document.createElement('select');
        select.id = `editor-docker-${key}-${++topologyID}`;
        select.dataset.dockerVersion = key;
        text.htmlFor = select.id;
        text.textContent = label;
        group.append(text, select);
        (key === 'distribution' ? advanced : essentials).append(group);
        controls[key] = select;
      }
      const {distribution, major, minor} = controls;
      distribution.add(new Option('Percona Server for MongoDB (Docker supports PSMDB only)', 'psmdb'));
      const tags = () => Array.from(image.options).map(option => option.value).filter(tag => /^\d+\.\d+\./.test(tag));
      const majorOf = tag => /^(\d+\.\d+)/.exec(tag)?.[1];
      const fillMajors = () => {
        major.replaceChildren(...Array.from(new Set(tags().map(majorOf))).map(value => new Option(value, value)));
        major.add(new Option('Custom image (advanced settings)', '__custom__'));
      };
      const fillMinors = () => {
        minor.replaceChildren(...tags().filter(tag => majorOf(tag) === major.value).map(tag => new Option(tag, tag)));
        if (major.value === '__custom__') minor.add(new Option('Use image from advanced settings', '__custom__'));
      };
      fillMajors();
      major.value = majorOf(image.value) || '__custom__';
      fillMinors();
      minor.value = major.value === '__custom__' ? '__custom__' : image.value;
      distribution.addEventListener('change', () => {
        fillMajors();
        choice(major, 'Choose a major version…');
        major.disabled = !distribution.value;
        minor.replaceChildren(new Option('Choose a minor version…', ''));
        minor.disabled = true;
      });
      major.addEventListener('change', () => {
        fillMinors();
        choice(minor, 'Choose a minor version…');
        minor.disabled = !major.value;
      });
      minor.addEventListener('change', () => {
        if (minor.value && minor.value !== '__custom__') {
          image.value = minor.value;
          image.dispatchEvent(new Event('change', { bubbles: true }));
        } else if (minor.value === '__custom__') revealField(image);
      });
      image.addEventListener('change', () => {
        if (!block.dataset.versionSelectionPending) {
          fillMajors();
          major.value = majorOf(image.value) || '__custom__';
          fillMinors();
          minor.value = major.value === '__custom__' ? '__custom__' : image.value;
        }
      });
    }

    function prepareNewTopology(block) {
      enhanceTopology(block);
      block.querySelector('.customize-body').style.display = '';
      refresh();
      block.querySelector('[name$="_name[]"]').focus();
    }

    function revealField(field) {
      if (!field) return;
      const section = field.closest('[data-editor-service]');
      if (section) revealedServices.add(section.dataset.editorService);
      const panel = field.closest('[data-editor-panel]');
      if (panel) showPanel(panel.dataset.editorPanel);
      // Open presentation-only collapsibles, without changing feature toggles.
      for (let ancestor = field.parentElement; ancestor && ancestor !== form; ancestor = ancestor.parentElement) {
        if (ancestor.matches('.customize-body, .package-block-body, .compute-block-body, #credentials-body, #chaos-settings-body, #pmm-cloud-body, #seaweedfs-details-body')) {
          ancestor.style.display = '';
          const toggle = ancestor.matches('.customize-body')
            ? ancestor.closest('.component-block').querySelector('[onclick="toggleCustomizeBlock(this)"]')
            : ancestor.previousElementSibling?.querySelector('button');
          if (toggle) {
            toggle.textContent = '▲ Collapse';
            toggle.setAttribute('aria-expanded', 'true');
          }
        }
      }
      refresh();
      field.scrollIntoView({ block: 'center' });
      field.focus();
    }

    function showError(message, selector) {
      errors.replaceChildren();
      const text = document.createElement('p');
      text.textContent = message;
      errors.append(text);
      const field = typeof selector === 'string' ? form.querySelector(selector) : selector;
      if (field) {
        const link = document.createElement('button');
        link.type = 'button';
        link.className = 'btn btn-outline btn-sm';
        link.textContent = 'Go to setting';
        link.addEventListener('click', () => revealField(field));
        errors.append(link);
        revealField(field);
      }
      errors.hidden = false;
      errors.focus();
    }

    function warning(message, selector) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'editor-warning';
      button.textContent = message;
      button.addEventListener('click', () => revealField(form.querySelector(selector)));
      return button;
    }

    let topologyID = 0;
    let queued = false;
    const observer = new MutationObserver(scheduleRefresh);
    function scheduleRefresh() {
      if (queued) return;
      queued = true;
      queueMicrotask(() => { queued = false; refresh(); });
    }

    function refresh() {
      // Updating summaries mutates the DOM too; do not observe our own writes.
      observer.disconnect();
      let nodes = 0, clusters = 0, replsets = 0, hasTLS = false, hasSearch = false;
      for (const block of form.querySelectorAll(topologySelector)) {
        enhanceTopology(block);
        const data = topologyData(block);
        const summary = topologySummary(data);
        nodes += summary.nodes;
        if (data.kind === 'cluster') clusters++; else replsets++;
        hasTLS ||= data.tls;
        hasSearch ||= data.search;
        const lines = block.querySelector('.editor-topology-summary').children;
        [data.version, summary.description, summary.features].forEach((text, i) => { lines[i].textContent = text; });
        const editing = block.querySelector('.customize-body').style.display !== 'none';
        block.classList.toggle('editor-topology-editing', editing);
        const toggle = block.querySelector('[onclick="toggleCustomizeBlock(this)"]');
        toggle.textContent = editing ? '▲ Collapse' : 'Edit';
        toggle.setAttribute('aria-expanded', String(editing));
        block.querySelector('.component-feature-row').hidden = !editing;
      }
      const enabledServices = [];
      for (const section of serviceSections) {
        const key = section.dataset.editorService;
        const active = serviceEnabled(key);
        if (key === 'clients') document.getElementById(services.clients.toggle).checked = active;
        section.hidden = !active && !revealedServices.has(key);
        if (section.hidden && section.contains(document.activeElement)) chooser.focus();
        chooser.querySelector(`option[value="${key}"]`).disabled = !section.hidden;
        if (active) enabledServices.push(services[key].label);
      }
      if (hasSearch) enabledServices.push('Search');
      document.getElementById('editor-summary-topology').textContent = `${countLabel(clusters, 'cluster')} · ${countLabel(replsets, 'replica set')} · ${countLabel(nodes, 'MongoDB node')} (including arbiters and routers)`;
      const preset = document.getElementById('hardware_preset');
      document.getElementById('editor-summary-hardware').textContent = platform === 'docker'
        ? 'Resources: local Docker containers'
        : `Hardware: ${preset?.value ? preset.selectedOptions[0].textContent : 'Defaults / custom overrides'}`;
      document.getElementById('editor-summary-services').textContent = `Services: ${enabledServices.join(', ') || 'none'}`;
      const warnings = [];
      if (form.querySelector('[name="use_spot_instances"]')?.checked) warnings.push(warning('Spot instances can be reclaimed. Avoid for benchmarks.', '[name="use_spot_instances"]'));
      if (hasTLS) warnings.push(warning('TLS uses preferTLS: plaintext clients remain allowed.', '#enable_ca'));
      if (serviceEnabled('sync')) warnings.push(warning('Starting ClusterSync replaces selected target collections.', '#enable_pcsm'));
      const hardwareErrors = document.getElementById('hardware-errors');
      if (hardwareErrors?.textContent && hardwareErrors.style.display !== 'none') warnings.push(warning(`Hardware: ${hardwareErrors.textContent}`, '#hardware_preset'));
      document.getElementById('editor-summary-warnings').replaceChildren(...warnings);
      observer.observe(form, { childList: true, subtree: true, attributes: true, attributeFilter: ['style', 'disabled'] });
    }

    form.addEventListener('input', scheduleRefresh);
    form.addEventListener('change', event => {
      const section = event.target.closest('[data-editor-service]');
      if (section && event.target.id === services[section.dataset.editorService].toggle && !event.target.checked) {
        revealedServices.delete(section.dataset.editorService);
      }
      scheduleRefresh();
    });
    // Native validation cannot focus a field in a hidden panel. Validate first,
    // reveal the first invalid field, then let the browser show its explanation.
    form.noValidate = true;
    form.addEventListener('submit', event => {
      errors.hidden = true;
      const invalid = Array.from(form.elements).find(field => field.willValidate && !field.validity.valid);
      if (!invalid) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      showError(invalid.validationMessage, invalid);
      invalid.reportValidity();
    }, true);

    if (platform === 'chaos') {
      const settings = document.getElementById('chaos-settings-body');
      settings.style.display = '';
      document.getElementById('chaos-settings-toggle').hidden = true;
      settings.previousElementSibling.querySelector('h2').textContent = 'Network & lifetime';
    }
    window.configurationEditor = { refresh, showError, revealField, prepareNewTopology };
    navigation.hidden = false;
    form.classList.add('config-editor');
    showPanel('profiles');
    refresh();
  });
})();
