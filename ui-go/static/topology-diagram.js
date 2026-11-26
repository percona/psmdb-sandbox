// Live topology diagram for the configuration editor. The graph model is pure
// data so it can be tested in Node; rendering is dependency-free inline SVG.
(() => {
  'use strict';

  // Larger topologies collapse into a "+N more" placeholder to stay readable.
  const LIMITS = { members: 5, shards: 3, routers: 4, clients: 4 };
  const DEFAULTS = {
    cluster: { shards: 2, members: 2, arbiters: 1, configs: 3, routers: 2 },
    replset: { members: 2, arbiters: 1 },
  };
  const FAMILIES = [
    ['data', 'Data path'], ['monitoring', 'Monitoring'], ['backup', 'Backups'], ['security', 'Security'],
  ];
  const FRAMES = { docker: 'Docker network', aws: 'AWS VPC', gcp: 'GCP VPC', azure: 'Azure VNet', chaos: 'CHAOS private cloud' };
  const ROLE_LABELS = { primary: 'Primary', secondary: 'Secondary', arbiter: 'Arbiter', mongos: 'mongos' };

  function count(value, fallback) {
    if (value === '' || value == null) return fallback;
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? Math.floor(number) : fallback;
  }

  function plural(n, word) {
    return `${n} ${word}${n === 1 ? '' : 's'}`;
  }

  function storageNode(platform, mode) {
    if (platform === 'docker') return { label: 'SeaweedFS', sub: 'S3 storage container', kind: 'service' };
    if (mode === 'existing') return { label: 'Existing bucket', sub: 'S3-compatible storage', kind: 'bucket' };
    if (platform === 'chaos') return { label: 'SeaweedFS', sub: 'S3 storage VM', kind: 'service' };
    const bucket = { aws: 'S3 bucket', gcp: 'GCS bucket', azure: 'Blob container' }[platform] || 'Backup bucket';
    return { label: bucket, sub: 'created for PBM', kind: 'bucket' };
  }

  // Each node lists its main process followed by the optional components
  // deployed beside it. PMM runs on every MongoDB node, PBM on config servers
  // and data-bearing nodes, and mongot beside data-bearing shard/replica set
  // nodes. Docker runs the agents and mongot as sidecar containers.
  function processesFor(role, set, t, platform) {
    const sidecar = platform === 'docker';
    const processes = [{ name: role === 'mongos' ? 'mongos' : 'mongod', family: 'data', main: true }];
    if (t.monitoring) processes.push({ name: 'pmm-agent', family: 'monitoring', sidecar });
    if (t.backups && role !== 'arbiter' && role !== 'mongos') processes.push({ name: 'pbm-agent', family: 'backup', sidecar });
    if (t.search && set !== 'config' && (role === 'primary' || role === 'secondary')) processes.push({ name: 'mongot', family: 'data', sidecar });
    if (t.toolkit && !sidecar) processes.push({ name: 'Percona Toolkit', family: 'tool', sidecar: false });
    return processes;
  }

  function members(prefix, roles, set, t, platform) {
    let shown = roles;
    let hidden = 0;
    if (roles.length > LIMITS.members) {
      shown = roles.slice(0, LIMITS.members - 1);
      hidden = roles.length - shown.length;
    }
    const nodes = shown.map((role, i) => ({ id: `${prefix}/${i}`, kind: role, label: ROLE_LABELS[role], processes: processesFor(role, set, t, platform) }));
    if (hidden) nodes.push({ id: `${prefix}/more`, kind: 'more', label: `+${hidden} more`, processes: [] });
    return nodes;
  }

  function replicaRoles(data, arbiters) {
    const roles = [];
    for (let i = 0; i < data; i++) roles.push(i === 0 ? 'primary' : 'secondary');
    for (let i = 0; i < arbiters; i++) roles.push('arbiter');
    return roles;
  }

  // state: { platform, topologies: [...topologyData], services: {...} }
  function buildTopologyGraph(state) {
    const platform = state.platform || 'docker';
    const svc = state.services || {};
    const host = platform === 'docker' ? 'container' : 'VM';
    const graph = { platform, frame: FRAMES[platform] || 'Network', topologies: [], clients: [], services: [], edges: [] };
    const link = (from, to, family, label, refs, quiet = false, extra = {}) => {
      graph.edges.push({ from, to, family, label, refs: Array.from(new Set(refs)), quiet, ...extra });
    };
    const byName = new Map();

    const pmm = !!svc.monitoring;
    const backups = !!svc.backups;
    const ca = !!svc.ca;
    const caOnPmm = ca && pmm && platform !== 'docker' && svc.caPlacement === 'pmm';
    const caID = caOnPmm ? 'svc:monitoring' : 'svc:ca';

    if (svc.load) graph.clients.push({ id: 'svc:load', ref: 'svc:load', kind: 'service', label: 'plgm', sub: `load generator ${host}`, refs: ['svc:load'] });
    if (svc.sync) graph.clients.push({ id: 'svc:sync', ref: 'svc:sync', kind: 'service', label: 'PCSM', sub: `ClusterSync ${host}`, refs: ['svc:sync'] });
    const clients = count(svc.clients, 0);
    for (let i = 0; i < Math.min(clients, LIMITS.clients); i++) {
      const more = i === LIMITS.clients - 1 && clients > LIMITS.clients;
      graph.clients.push({
        id: `svc:clients/${i}`, ref: 'svc:clients', kind: more ? 'more' : 'service',
        label: more ? `+${clients - i} PMM clients` : 'PMM client', sub: more ? '' : `${host} without MongoDB`, refs: ['svc:clients'],
      });
    }

    if (pmm) graph.services.push({ id: 'svc:monitoring', ref: 'svc:monitoring', kind: 'service', label: 'PMM server', sub: caOnPmm ? `${host}, also hosts the CA` : host, refs: caOnPmm ? ['svc:monitoring', 'svc:ca'] : ['svc:monitoring'] });
    if (backups) graph.services.push({ id: 'svc:backups', ref: 'svc:backups', ...storageNode(platform, svc.backupMode), refs: ['svc:backups'] });
    if (ca && !caOnPmm) graph.services.push({ id: 'svc:ca', ref: 'svc:ca', kind: 'service', label: 'Certificate authority', sub: platform === 'docker' ? 'offline signing container' : host, refs: ['svc:ca'] });
    if (svc.ldap) graph.services.push({ id: 'svc:ldap', ref: 'svc:ldap', kind: 'service', label: 'OpenLDAP', sub: host, refs: ['svc:ldap'] });

    const kindIndex = { cluster: 0, replset: 0 };
    for (const t of state.topologies || []) {
      const kind = t.kind === 'cluster' ? 'cluster' : 'replset';
      const ref = t.ref || `${kind}:${kindIndex[kind]}`;
      kindIndex[kind]++;
      const name = (t.name || '').trim() || `${kind === 'cluster' ? 'cluster' : 'replica set'} ${kindIndex[kind]}`;
      const d = DEFAULTS[kind];
      const data = count(t.members, d.members);
      const arbiters = count(t.arbiters, d.arbiters);
      const refs = [ref];
      const topology = {
        id: ref, ref, kind, name, refs,
        label: kind === 'cluster' ? `Sharded cluster ${name}` : `Replica set ${name}`,
        subtitle: [t.version, t.tls && 'TLS', t.audit && 'audit log'].filter(Boolean).join(' · '),
        routers: null, sets: [],
      };
      byName.set(`${kind}:${name}`, ref);

      if (kind === 'cluster') {
        const shards = count(t.shards, d.shards);
        const configs = count(t.configs, d.configs);
        const routers = count(t.routers, d.routers);
        const routerRoles = Array(routers).fill('mongos');
        let shownRouters = members(`${ref}/mongos`, routerRoles, 'mongos', t, platform);
        if (routers > LIMITS.routers) {
          shownRouters = members(`${ref}/mongos`, routerRoles.slice(0, LIMITS.routers - 1), 'mongos', t, platform);
          shownRouters.push({ id: `${ref}/mongos/more`, kind: 'more', label: `+${routers - LIMITS.routers + 1} more`, processes: [] });
        }
        topology.routers = { id: `${ref}/mongos`, label: plural(routers, 'mongos router'), members: shownRouters };
        topology.sets.push({ id: `${ref}/cfg`, label: 'Config servers', config: true, members: members(`${ref}/cfg`, replicaRoles(configs, 0), 'config', t, platform) });
        const shownShards = shards > LIMITS.shards ? LIMITS.shards - 1 : shards;
        for (let i = 0; i < shownShards; i++) {
          topology.sets.push({ id: `${ref}/shard${i}`, label: `Shard ${i + 1}`, members: members(`${ref}/shard${i}`, replicaRoles(data, arbiters), 'shard', t, platform) });
        }
        if (shards > shownShards) {
          topology.sets.push({ id: `${ref}/more-shards`, label: `+${shards - shownShards} more shards`, more: true, members: [] });
        }
        if (routers) {
          for (const set of topology.sets) link(topology.routers.id, set.id, 'data', set.config ? 'cluster metadata' : 'routed queries', refs, true);
        }
        topology.summary = `${plural(routers, 'mongos router')}, ${plural(configs, 'config server')}, ${plural(shards, 'shard')} of ${plural(data, 'data node')} and ${plural(arbiters, 'arbiter')}`;
      } else {
        topology.sets.push({ id: `${ref}/rs`, label: '', members: members(`${ref}/rs`, replicaRoles(data, arbiters), 'replset', t, platform) });
        topology.summary = `${plural(data, 'data node')} and ${plural(arbiters, 'arbiter')}`;
      }

      for (const set of topology.sets) {
        const primary = set.members.find(member => member.kind === 'primary');
        if (!primary) continue;
        for (const member of set.members) {
          if (member.kind === 'secondary' || (member.kind === 'more' && data > 1)) link(primary.id, member.id, 'data', 'replication', refs, true, { below: true });
        }
      }

      if (pmm && t.monitoring) link(ref, 'svc:monitoring', 'monitoring', 'metrics', [ref, 'svc:monitoring']);
      if (backups && t.backups) link(ref, 'svc:backups', 'backup', 'backups + oplog', [ref, 'svc:backups']);
      const psmdb = platform === 'docker' || !t.distribution || t.distribution === 'psmdb';
      if (svc.vault && psmdb) {
        const vault = `svc:vault/${ref}`;
        graph.services.push({ id: vault, ref: 'svc:vault', kind: 'service', label: `Vault (${name})`, sub: `key server ${host}`, refs: ['svc:vault', ref] });
        link(ref, vault, 'security', 'master key', [ref, 'svc:vault']);
      }
      if (ca && t.tls) link(caID, ref, 'security', 'x.509 certificates', [ref, 'svc:ca', caID]);
      if (svc.ldap) link(ref, 'svc:ldap', 'security', 'LDAP authentication', [ref, 'svc:ldap']);
      if (svc.load) link('svc:load', topology.routers ? topology.routers.id : ref, 'data', 'workload', [ref, 'svc:load']);
      graph.topologies.push(topology);
    }

    if (svc.sync) {
      const source = byName.get(svc.syncSource || '');
      const target = byName.get(svc.syncTarget || '');
      if (source) link(source, 'svc:sync', 'data', 'clone + change stream', [source, 'svc:sync']);
      if (target && target !== source) link('svc:sync', target, 'data', 'apply writes', [target, 'svc:sync']);
    }
    if (pmm) {
      for (const client of graph.clients) {
        if (client.ref === 'svc:clients') link(client.id, 'svc:monitoring', 'monitoring', 'metrics', ['svc:clients', 'svc:monitoring']);
      }
    }
    // A highlighted topology keeps the services it talks to in focus.
    for (const node of [...graph.clients, ...graph.services]) {
      const refs = new Set(node.refs);
      for (const edge of graph.edges) {
        if (edge.from === node.id || edge.to === node.id) edge.refs.forEach(ref => refs.add(ref));
      }
      node.refs = Array.from(refs);
    }
    return graph;
  }

  function labelsByID(graph) {
    const labels = new Map();
    for (const t of graph.topologies) {
      labels.set(t.id, t.label);
      if (t.routers) labels.set(t.routers.id, `${t.name} mongos routers`);
      for (const set of t.sets) {
        labels.set(set.id, set.label ? `${t.name} ${set.label.toLowerCase()}` : t.label);
        for (const member of set.members) labels.set(member.id, `${t.name} ${member.label.toLowerCase()}`);
      }
    }
    for (const node of [...graph.clients, ...graph.services]) labels.set(node.id, node.label);
    return labels;
  }

  function processText(process) {
    return `${process.name}${process.sidecar ? ' (sidecar container)' : ''}`;
  }

  // One line per set and process combination, e.g. "rs01 primary, secondary: mongod + pmm-agent".
  function describeProcesses(t) {
    const lines = [];
    for (const set of [t.routers, ...t.sets].filter(Boolean)) {
      const groups = new Map();
      for (const member of set.members) {
        if (!member.processes.length) continue;
        const key = member.processes.map(processText).join(' + ');
        if (!groups.has(key)) groups.set(key, new Set());
        groups.get(key).add(member.label.toLowerCase());
      }
      const name = set === t.routers ? `${t.name} routers` : set.label ? `${t.name} ${set.label.toLowerCase()}` : t.name;
      for (const [processes, roles] of groups) lines.push(`${name} ${Array.from(roles).join(', ')}: ${processes}`);
    }
    return lines;
  }

  // Plain-text equivalent for screen readers and tests.
  function describeTopologyGraph(graph) {
    const lines = [];
    for (const t of graph.topologies) lines.push(`${t.label}: ${t.summary}${t.subtitle ? ` (${t.subtitle})` : ''}`);
    for (const t of graph.topologies) lines.push(...describeProcesses(t));
    for (const node of [...graph.clients, ...graph.services]) lines.push(`${node.label}${node.sub ? ` (${node.sub})` : ''}`);
    const labels = labelsByID(graph);
    for (const edge of graph.edges) {
      if (edge.quiet) continue;
      lines.push(`${labels.get(edge.from)} → ${labels.get(edge.to)}: ${edge.label}`);
    }
    return lines;
  }

  const SIZE = {
    nodeW: 108, nodeHead: 18, procH: 13, procGap: 2, nodeMinH: 36, serviceW: 136, serviceH: 46, gap: 10, pad: 10,
    setHead: 20, arc: 16, groupHead: 40, innerGap: 28, rowGap: 64, bandGap: 32,
    margin: 12, frameHead: 26, maxRow: 1500, moreShardW: 120,
  };

  function rowWidth(n, w) {
    return n ? n * w + (n - 1) * SIZE.gap : 0;
  }

  // Nodes grow with the number of processes they list.
  function nodeHeight(rows) {
    return Math.max(SIZE.nodeMinH, SIZE.nodeHead + rows * (SIZE.procH + SIZE.procGap) - SIZE.procGap + 5);
  }

  function setRows(set) {
    return Math.max(0, ...set.members.map(member => member.processes.length));
  }

  // rows lets sets placed side by side share a height.
  function setSize(set, rows = setRows(set)) {
    const h = SIZE.setHead + nodeHeight(rows) + SIZE.arc + SIZE.pad;
    if (set.more) return { w: SIZE.moreShardW, h };
    return { w: Math.max(rowWidth(set.members.length, SIZE.nodeW), 80) + 2 * SIZE.pad, h };
  }

  // Config servers sit beside the mongos routers; shards form the row below.
  // Sets in a row share a height. The router row leaves room for the arrow
  // between routers and config servers.
  function topologyRows(t) {
    const top = [t.routers, ...t.sets.filter(set => set.config)].filter(Boolean);
    const bottom = t.sets.filter(set => !set.config);
    return [top, bottom].filter(row => row.length).map(sets => {
      const rows = Math.max(0, ...sets.map(setRows));
      const gap = t.routers && sets.includes(t.routers) ? SIZE.innerGap : SIZE.gap;
      const sizes = sets.map(set => setSize(set, rows));
      return {
        sets, rows, gap,
        w: sizes.reduce((sum, s) => sum + s.w, 0) + gap * (sizes.length - 1),
        h: Math.max(...sizes.map(s => s.h)),
      };
    });
  }

  function topologySize(t) {
    const rows = topologyRows(t);
    return {
      w: Math.max(...rows.map(row => row.w), 220) + 2 * SIZE.pad,
      h: SIZE.groupHead + rows.reduce((sum, row) => sum + row.h, 0) + SIZE.innerGap * Math.max(rows.length - 1, 0) + SIZE.pad,
    };
  }

  function placeRow(boxes, set, x, y, rows = setRows(set)) {
    const size = setSize(set, rows);
    boxes.set(set.id, { x, y, w: size.w, h: size.h });
    set.members.forEach((member, i) => {
      boxes.set(member.id, { x: x + SIZE.pad + i * (SIZE.nodeW + SIZE.gap), y: y + SIZE.setHead, w: SIZE.nodeW, h: nodeHeight(rows) });
    });
    return size;
  }

  function placeTopology(boxes, t, x, y, size) {
    boxes.set(t.id, { x, y, w: size.w, h: size.h });
    let cursor = y + SIZE.groupHead;
    for (const row of topologyRows(t)) {
      let left = x + (size.w - row.w) / 2;
      for (const set of row.sets) left += placeRow(boxes, set, left, cursor, row.rows).w + row.gap;
      cursor += row.h + SIZE.innerGap;
    }
  }

  function layoutTopologyGraph(graph) {
    const boxes = new Map();
    const sizes = graph.topologies.map(topologySize);
    const bands = [];
    let band = [];
    let bandW = 0;
    graph.topologies.forEach((t, i) => {
      const w = sizes[i].w;
      if (band.length && bandW + SIZE.bandGap + w > SIZE.maxRow) {
        bands.push({ items: band, w: bandW });
        band = [];
        bandW = 0;
      }
      bandW += (band.length ? SIZE.bandGap : 0) + w;
      band.push(i);
    });
    if (band.length) bands.push({ items: band, w: bandW });

    const nodeRowW = nodes => rowWidth(nodes.length, SIZE.serviceW);
    const innerW = Math.max(320, nodeRowW(graph.clients), nodeRowW(graph.services), ...bands.map(b => b.w));
    const left = SIZE.margin + SIZE.pad;
    let y = SIZE.margin + SIZE.frameHead;

    const placeNodes = nodes => {
      let x = left + (innerW - nodeRowW(nodes)) / 2;
      for (const node of nodes) {
        boxes.set(node.id, { x, y, w: SIZE.serviceW, h: SIZE.serviceH });
        x += SIZE.serviceW + SIZE.gap;
      }
      y += SIZE.serviceH + SIZE.rowGap;
    };
    if (graph.clients.length) placeNodes(graph.clients);
    bands.forEach(b => {
      let x = left + (innerW - b.w) / 2;
      const h = Math.max(...b.items.map(i => sizes[i].h));
      for (const i of b.items) {
        placeTopology(boxes, graph.topologies[i], x, y, sizes[i]);
        x += sizes[i].w + SIZE.bandGap;
      }
      y += h + SIZE.bandGap;
    });
    if (!bands.length) y += 50;
    if (graph.services.length) {
      y += SIZE.rowGap - SIZE.bandGap;
      placeNodes(graph.services);
      y -= SIZE.rowGap;
    } else {
      y -= SIZE.bandGap;
    }
    const height = y + SIZE.pad + SIZE.margin;
    const width = innerW + 2 * (SIZE.margin + SIZE.pad);
    return { width, height, boxes, frame: { x: SIZE.margin, y: SIZE.margin, w: width - 2 * SIZE.margin, h: height - 2 * SIZE.margin } };
  }

  // Edges sharing a box side are spread out so the arrows stay distinguishable.
  // Edges between members of one set arc underneath the row instead of
  // crossing the members in between.
  function routeEdges(graph, boxes) {
    const routes = [];
    const ends = new Map();
    graph.edges.forEach((edge, i) => {
      const a = boxes.get(edge.from);
      const b = boxes.get(edge.to);
      if (!a || !b) return;
      let sides;
      if (edge.below) sides = ['bottom', 'bottom'];
      else if (a.y + a.h <= b.y) sides = ['bottom', 'top'];
      else if (b.y + b.h <= a.y) sides = ['top', 'bottom'];
      else if (a.x + a.w <= b.x) sides = ['right', 'left'];
      else if (b.x + b.w <= a.x) sides = ['left', 'right'];
      else sides = ['bottom', 'bottom'];
      routes[i] = { edge, sides, points: [] };
      [[edge.from, a, sides[0], b, 0], [edge.to, b, sides[1], a, 1]].forEach(([id, box, side, other, end]) => {
        const key = `${id}|${side}`;
        if (!ends.has(key)) ends.set(key, []);
        const horizontal = side === 'left' || side === 'right';
        let order = horizontal ? other.y + other.h / 2 : other.x + other.w / 2;
        // Arcs on one side nest: the farther the target, the farther from it
        // the arc starts, so longer arcs pass outside shorter ones.
        if (sides[0] === sides[1]) order = 1 / (order - (box.x + box.w / 2) || 1);
        ends.get(key).push({ route: i, end, box, side, horizontal, order });
      });
    });
    const spread = (list, k, start, length) => (list.length === 1 ? start + length / 2 : start + length * (0.15 + 0.7 * (k + 1) / (list.length + 1)));
    for (const list of ends.values()) {
      list.sort((p, q) => p.order - q.order);
      list.forEach((p, k) => {
        routes[p.route].points[p.end] = p.horizontal
          ? { x: p.side === 'left' ? p.box.x : p.box.x + p.box.w, y: spread(list, k, p.box.y, p.box.h) }
          : { x: spread(list, k, p.box.x, p.box.w), y: p.side === 'top' ? p.box.y : p.box.y + p.box.h };
      });
    }
    return routes.filter(Boolean).map(route => {
      const [p, q] = route.points;
      let d;
      let mid;
      if (route.sides[0] === 'left' || route.sides[0] === 'right') {
        const mx = (p.x + q.x) / 2;
        d = `M${p.x} ${p.y} C${mx} ${p.y} ${mx} ${q.y} ${q.x} ${q.y}`;
        mid = { x: mx, y: (p.y + q.y) / 2 };
      } else if (route.sides[0] === route.sides[1]) {
        const depth = 8 + Math.min(Math.abs(q.x - p.x) / 20, 18);
        d = `M${p.x} ${p.y} C${p.x} ${p.y + depth} ${q.x} ${q.y + depth} ${q.x} ${q.y}`;
        mid = { x: (p.x + q.x) / 2, y: p.y + depth * 0.75 };
      } else {
        const my = (p.y + q.y) / 2;
        d = `M${p.x} ${p.y} C${p.x} ${my} ${q.x} ${my} ${q.x} ${q.y}`;
        mid = { x: (p.x + q.x) / 2, y: my };
      }
      return { edge: route.edge, d, mid };
    });
  }

  function esc(value) {
    return String(value).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function fit(text, max) {
    return text.length > max ? `${text.slice(0, max - 1)}…` : text;
  }

  function r(x) {
    return Math.round(x * 10) / 10;
  }

  function rect(box, cls, rx = 6) {
    return `<rect class="${cls}" x="${r(box.x)}" y="${r(box.y)}" width="${r(box.w)}" height="${r(box.h)}" rx="${rx}"/>`;
  }

  function text(x, y, value, cls, max) {
    return `<text class="${cls}" x="${r(x)}" y="${r(y)}">${esc(fit(value, max))}</text>`;
  }

  // The main process comes first; optional components follow, coloured by the
  // arrow family they belong to. Docker sidecar containers are dashed.
  function renderProcesses(processes, box) {
    let out = '';
    processes.forEach((process, i) => {
      const row = { x: box.x + 5, y: box.y + SIZE.nodeHead + i * (SIZE.procH + SIZE.procGap), w: box.w - 10, h: SIZE.procH };
      const cls = ['td-proc', `td-proc-${process.family}`, process.main && 'td-proc-main', process.sidecar && 'td-sidecar'].filter(Boolean).join(' ');
      out += `<g class="${cls}">${rect(row, 'td-proc-box', 3)}${text(row.x + 5, row.y + 9.5, process.name, 'td-proc-label', 19)}</g>`;
    });
    return out;
  }

  function renderMember(member, box) {
    const title = `${member.label}${member.processes.length ? `: ${member.processes.map(processText).join(', ')}` : ''}`;
    return `<g class="td-node td-${member.kind}"><title>${esc(title)}</title>${rect(box, 'td-box', 5)}${text(box.x + 6, box.y + 13, member.label, 'td-node-label', 18)}${renderProcesses(member.processes, box)}</g>`;
  }

  function renderSet(set, boxes) {
    const box = boxes.get(set.id);
    let out = `<g class="td-set${set.more ? ' td-more' : ''}">${rect(box, 'td-set-box', 6)}`;
    if (set.more) out += text(box.x + 10, box.y + box.h / 2 + 4, set.label, 'td-set-label', 20);
    else if (set.label) out += text(box.x + 8, box.y + 14, set.label, 'td-set-label', 30);
    for (const member of set.members) out += renderMember(member, boxes.get(member.id));
    return `${out}</g>`;
  }

  function renderTopology(t, boxes) {
    const box = boxes.get(t.id);
    const name = `${t.label}${t.subtitle ? `, ${t.subtitle}` : ''}. ${t.summary}. Activate to edit.`;
    let out = `<g class="td-group td-${t.kind}" data-ref="${esc(t.ref)}" data-refs="${esc(t.refs.join(' '))}" tabindex="0" role="button" aria-label="${esc(name)}">`;
    out += rect(box, 'td-group-box', 8);
    out += text(box.x + 10, box.y + 17, t.label, 'td-group-label', Math.floor(box.w / 7.5));
    if (t.subtitle) out += text(box.x + 10, box.y + 31, t.subtitle, 'td-group-sub', Math.floor(box.w / 6.5));
    if (t.routers) out += renderSet(t.routers, boxes);
    for (const set of t.sets) out += renderSet(set, boxes);
    return `${out}</g>`;
  }

  function renderService(node, boxes) {
    const box = boxes.get(node.id);
    const name = `${node.label}${node.sub ? `, ${node.sub}` : ''}. Activate to edit.`;
    return `<g class="td-service td-${node.kind}" data-ref="${esc(node.ref)}" data-refs="${esc(node.refs.join(' '))}" tabindex="0" role="button" aria-label="${esc(name)}">`
      + `<title>${esc(node.label)}${node.sub ? ` (${esc(node.sub)})` : ''}</title>${rect(box, 'td-box', 6)}`
      + text(box.x + 8, box.y + 18, node.label, 'td-node-label', 22)
      + (node.sub ? text(box.x + 8, box.y + 34, node.sub, 'td-node-sub', 26) : '')
      + '</g>';
  }

  function renderTopologySVG(graph, layout = layoutTopologyGraph(graph)) {
    const { boxes, width, height, frame } = layout;
    const labels = labelsByID(graph);
    let out = `<svg class="td-svg" viewBox="0 0 ${r(width)} ${r(height)}" role="group" aria-label="${esc(`Topology diagram: ${graph.frame}`)}" xmlns="http://www.w3.org/2000/svg"><defs>`;
    for (const [family] of FAMILIES) {
      out += `<marker id="td-arrow-${family}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="td-arrow-${family}" d="M0 0L10 5L0 10z"/></marker>`;
    }
    out += '</defs>';
    out += rect(frame, 'td-frame', 10) + text(frame.x + 10, frame.y + 17, graph.frame, 'td-frame-label', 40);
    if (!graph.topologies.length) out += text(width / 2 - 140, frame.y + frame.h / 2, 'Add a cluster or replica set to see it here.', 'td-empty', 60);
    for (const t of graph.topologies) out += renderTopology(t, boxes);
    out += '<g class="td-edges">';
    for (const { edge, d, mid } of routeEdges(graph, boxes)) {
      const title = `${labels.get(edge.from)} → ${labels.get(edge.to)}: ${edge.label}`;
      out += `<g class="td-edge td-edge-${edge.family}${edge.quiet ? ' td-quiet' : ''}" data-refs="${esc(edge.refs.join(' '))}"><title>${esc(title)}</title>`
        + `<path class="td-edge-hit" d="${d}"/><path class="td-edge-line" d="${d}" marker-end="url(#td-arrow-${edge.family})"/>`
        + (edge.quiet ? '' : `<text class="td-edge-label" x="${r(mid.x)}" y="${r(mid.y - 3)}">${esc(edge.label)}</text>`)
        + '</g>';
    }
    out += '</g>';
    for (const node of [...graph.clients, ...graph.services]) out += renderService(node, boxes);
    return `${out}</svg>`;
  }

  const api = { buildTopologyGraph, layoutTopologyGraph, describeTopologyGraph, renderTopologySVG, FAMILIES };
  if (typeof module !== 'undefined') module.exports = api;
  if (typeof document === 'undefined') return;

  // Adds the collapsible diagram panel to host. onSelect receives the data-ref
  // of the activated element ("cluster:0", "svc:monitoring", ...). The
  // environment page passes pinnable: false and its own selectHint.
  function mount(host, { onSelect, pinnable = true, selectHint = 'Select an element to edit it.' }) {
    const root = document.createElement('details');
    root.id = 'topology-diagram';
    root.className = 'topology-diagram';
    root.open = true;
    const summary = document.createElement('summary');
    summary.textContent = 'Topology diagram';
    const legend = document.createElement('div');
    legend.className = 'td-legend';
    for (const [family, label] of FAMILIES) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = `td-legend-item td-legend-${family}`;
      button.setAttribute('aria-pressed', 'true');
      button.title = `Show or hide ${label.toLowerCase()} arrows`;
      const swatch = document.createElement('span');
      swatch.className = 'td-swatch';
      swatch.setAttribute('aria-hidden', 'true');
      button.append(swatch, label);
      button.addEventListener('click', () => {
        const shown = button.getAttribute('aria-pressed') !== 'true';
        button.setAttribute('aria-pressed', String(shown));
        root.classList.toggle(`td-hide-${family}`, !shown);
      });
      legend.append(button);
    }
    const hint = document.createElement('span');
    hint.className = 'td-hint';
    const hints = {
      docker: `Each MongoDB box lists mongod or mongos and its optional components, which run as sidecar containers (dashed). ${selectHint}`,
      host: `Each MongoDB box is a host listing mongod or mongos and the optional components installed beside it. ${selectHint}`,
    };
    // Pinning keeps the diagram on screen while scrolling, at the cost of
    // covering part of the form, so it is opt-in and remembered per browser.
    const pin = document.createElement('button');
    pin.type = 'button';
    pin.className = 'td-legend-item td-pin';
    pin.textContent = 'Keep visible while scrolling';
    const setPinned = pinned => {
      pin.setAttribute('aria-pressed', String(pinned));
      root.classList.toggle('td-pinned', pinned);
    };
    let stored = null;
    try { stored = localStorage.getItem('topologyDiagramPinned'); } catch (err) { /* storage unavailable */ }
    setPinned(pinnable && stored === 'true');
    pin.addEventListener('click', () => {
      const pinned = pin.getAttribute('aria-pressed') !== 'true';
      setPinned(pinned);
      try { localStorage.setItem('topologyDiagramPinned', String(pinned)); } catch (err) { /* storage unavailable */ }
    });
    legend.append(hint);
    if (pinnable) legend.append(pin);
    const canvas = document.createElement('div');
    canvas.className = 'td-canvas';
    const list = document.createElement('ul');
    list.className = 'visually-hidden';
    list.id = 'topology-diagram-description';
    root.append(summary, legend, canvas, list);
    host.prepend(root);

    const activate = event => {
      const target = event.target.closest?.('[data-ref]');
      if (!target || !canvas.contains(target)) return;
      if (event.type === 'keydown') {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
      }
      onSelect(target.dataset.ref);
    };
    canvas.addEventListener('click', activate);
    canvas.addEventListener('keydown', activate);

    let lastKey = '';
    let graph = null;
    function update(state, focus) {
      graph = buildTopologyGraph(state);
      hint.textContent = hints[graph.platform === 'docker' ? 'docker' : 'host'];
      const key = JSON.stringify(graph);
      if (key !== lastKey) {
        lastKey = key;
        canvas.innerHTML = renderTopologySVG(graph);
        list.replaceChildren(...describeTopologyGraph(graph).map(line => {
          const item = document.createElement('li');
          item.textContent = line;
          return item;
        }));
        root.classList.toggle('td-few-edges', graph.edges.filter(edge => !edge.quiet).length <= 8);
      }
      root.classList.toggle('td-has-focus', !!focus);
      for (const element of canvas.querySelectorAll('[data-refs]')) {
        element.classList.toggle('td-dim', !!focus && !element.dataset.refs.split(' ').includes(focus));
      }
      return graph;
    }
    return { root, update };
  }

  window.TopologyDiagram = { ...api, mount };
})();
