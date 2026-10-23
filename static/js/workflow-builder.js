(function () {
  const nodes = [];
  const edges = [];
  let selectedKey = null;
  let selectedEdgeIndex = null;
  let lastAddedKey = null;
  let workflowDirty = false;
  window.__workflowIsDirty = function () { return workflowDirty; };
  window.__workflowMarkClean = function () { workflowDirty = false; };
  function markWorkflowDirty() { workflowDirty = true; }

  const canvas = document.getElementById('canvas');
  const canvasWrap = document.getElementById('canvas-wrap');
  const edgeSvg = document.getElementById('edge-svg');
  const edgeFrom = document.getElementById('edge-from');
  const edgeTo = document.getElementById('edge-to');
  const edgeTypeSel = document.getElementById('edge-type');
  const edgeListEl = document.getElementById('edge-list');
  const edgeListEmpty = document.getElementById('edge-list-empty');
  const edgeCountEl = document.getElementById('edge-count');
  const edgePriorityInput = document.getElementById('edge-priority');

  const NODE_W = 152;
  const NODE_H_FALLBACK = 56;
  let CANVAS_MIN_W = 1200;
  let CANVAS_MIN_H = 560;

  function syncCanvasMinFromViewport() {
    if (!canvasWrap) return;
    const w = Math.max(800, Math.floor(canvasWrap.clientWidth || 0));
    const h = Math.max(480, Math.floor(canvasWrap.clientHeight || 0));
    if (w > 0) CANVAS_MIN_W = Math.max(w, 800);
    if (h > 0) CANVAS_MIN_H = Math.max(h, 480);
  }

  let dragState = null;
  // Survives past mouseup so the trailing click after a drag does not open the node modal.
  let suppressNodeClick = false;
  let connectDrag = null;
  let previewPathEl = null;

  const EDGE_TYPE_LABELS = {
    default: 'Then → next',
    true: 'If yes',
    false: 'If no',
    hot: 'Hot lead',
    warm: 'Warm lead',
    cold: 'Cold lead'
  };

  function isEngagementCondition(type) {
    return type === 'condition_engagement';
  }

  function isTemperatureCondition(type) {
    return type === 'condition_temperature';
  }

  function isBranchCondition(type) {
    return isEngagementCondition(type) || isTemperatureCondition(type);
  }

  function uid() {
    return 'n-' + Math.random().toString(36).slice(2, 10);
  }

  function showMsg(text, ok) {
    const el = document.getElementById('builder-msg');
    if (!el) return;
    el.className = (ok ? 'alert-success' : 'alert-error') + ' wf-stage-toast';
    el.textContent = text;
    el.classList.remove('hidden');
  }

  function defaultConfig(type) {
    if (type === 'action_send_email') return { open_tracking: false, click_tracking: false, template_id: 0 };
    if (type === 'action_wait') return { duration_seconds: 90 }; // reply watchers: ~1–2 min
    if (type === 'trigger_reply_intent') return { intent: 'interested' };
    if (type === 'condition_engagement') return { predicate: 'has_replied', priority: 50, params: { email_send_scope: 'last_in_workflow' } };
    if (type === 'condition_temperature') return {};
    return {};
  }

  function closeNodeModal() {
    const modal = document.getElementById('wf-node-modal');
    if (modal && typeof modal.close === 'function') modal.close();
  }

  function openNodeModal() {
    const modal = document.getElementById('wf-node-modal');
    if (modal && typeof modal.showModal === 'function') modal.showModal();
  }

  function nodeClass(type) {
    if (type.startsWith('trigger')) return 'wf-node-type-trigger';
    if (type.startsWith('condition')) return 'wf-node-type-condition';
    if (type === 'action_end') return 'wf-node-type-end';
    return 'wf-node-type-action';
  }

  function nodeColorClass(type) {
    if (type.startsWith('action')) return 'text-blue-700';
    if (type.startsWith('condition')) return 'text-amber-700';
    if (type.startsWith('trigger')) return 'text-slate-600';
    return 'text-slate-800';
  }

  function nodeLabel(key) {
    const n = getNode(key);
    return n ? n.label : key;
  }

  function getNode(key) {
    return nodes.find(n => n.node_key === key);
  }

  function templateName(templateId) {
    const id = parseInt(templateId, 10);
    const t = TEMPLATES.find(x => x.id === id);
    return t ? t.name : 'Template #' + (templateId || '?');
  }

  function sendEmailNodes() {
    return nodes.filter(n => n.node_type === 'action_send_email');
  }

  function sendNodeDisplayLabel(n) {
    if (!n) return 'Send email';
    const label = (n.label || '').trim();
    return label && label !== 'Send Email' ? label : 'Send email';
  }

  function ensureConditionParams(n) {
    if (!n.config.params) n.config.params = { email_send_scope: 'last_in_workflow' };
    return n.config.params;
  }

  function conditionEmailRefLabel(n) {
    const params = ensureConditionParams(n);
    if (params.email_send_scope === 'node' && params.email_node_key) {
      const src = getNode(params.email_node_key);
      return src ? sendNodeDisplayLabel(src) : params.email_node_key;
    }
    return 'Most recent email';
  }

  const PREDICATE_LABELS = {
    has_opened: 'Opened',
    has_not_opened: 'Not opened',
    click_count_gte: 'Clicked',
    has_replied: 'Replied',
    has_not_replied: 'Not replied',
    has_positive_reply: 'Positive reply',
    has_negative_reply: 'Negative reply'
  };

  function conditionPredicateSummary(n) {
    const pred = n.config.predicate || 'has_replied';
    const params = ensureConditionParams(n);
    let label = PREDICATE_LABELS[pred] || pred;
    if (pred === 'click_count_gte') {
      const min = params.min || 1;
      label = min > 1 ? `Clicked ≥ ${min}` : 'Clicked';
    }
    if (pred === 'has_not_opened' || pred === 'has_not_replied') {
      const days = params.wait_days || 3;
      label = (pred === 'has_not_opened' ? 'Not opened' : 'Not replied') + ` after ${days}d`;
    }
    return label;
  }

  function needsGracePeriod(pred) {
    return pred === 'has_not_opened' || pred === 'has_not_replied';
  }

  function clearConditionRefsToNode(deletedKey) {
    nodes.forEach(n => {
      if (n.node_type !== 'condition_engagement') return;
      const params = ensureConditionParams(n);
      if (params.email_send_scope === 'node' && params.email_node_key === deletedKey) {
        params.email_send_scope = 'last_in_workflow';
        delete params.email_node_key;
      }
    });
  }

  function findEdgeIndex(from, to, type) {
    return edges.findIndex(e => e.source_node_key === from && e.target_node_key === to && e.edge_type === type);
  }

  function hasEdge(from, to, type) {
    return findEdgeIndex(from, to, type) >= 0;
  }

  function removeEdgesForNode(key) {
    for (let i = edges.length - 1; i >= 0; i--) {
      if (edges[i].source_node_key === key || edges[i].target_node_key === key) {
        edges.splice(i, 1);
        if (selectedEdgeIndex === i) selectedEdgeIndex = null;
        else if (selectedEdgeIndex != null && selectedEdgeIndex > i) selectedEdgeIndex--;
      }
    }
  }

  function pickEdgeTypeForSource(sourceKey, preferred) {
    const src = getNode(sourceKey);
    if (!src) return 'default';
    if (isTemperatureCondition(src.node_type)) {
      if (preferred && (preferred === 'hot' || preferred === 'warm' || preferred === 'cold')) return preferred;
      const used = new Set(edges.filter(e => e.source_node_key === sourceKey).map(e => e.edge_type));
      for (const t of ['hot', 'warm', 'cold']) {
        if (!used.has(t)) return t;
      }
      return 'cold';
    }
    if (!isEngagementCondition(src.node_type)) return 'default';
    if (preferred && preferred !== 'default') return preferred;
    const hasTrue = edges.some(e => e.source_node_key === sourceKey && e.edge_type === 'true');
    return hasTrue ? 'false' : 'true';
  }

  function autoConnect(newKey, sourceKey) {
    const source = sourceKey || selectedKey || lastAddedKey;
    if (!source || source === newKey) return;

    const edgeType = pickEdgeTypeForSource(source);
    if (!hasEdge(source, newKey, edgeType)) {
      edges.push({
        source_node_key: source,
        target_node_key: newKey,
        edge_type: edgeType,
        priority: 0,
        condition_json: '{}'
      });
    }
  }

  function layoutPosition(index) {
    const col = index % 3;
    const row = Math.floor(index / 3);
    return { x: 60 + col * 220, y: 60 + row * 120 };
  }

  function addNode(type, label, x, y, autoLink) {
    if (type === 'trigger_campaign_started') {
      const existing = nodes.find(n => n.node_type === 'trigger_campaign_started');
      if (existing) {
        showMsg('Only one Campaign Start trigger allowed', false);
        selectNode(existing.node_key);
        return null;
      }
      if (nodes.some(n => n.node_type === 'trigger_reply_intent')) {
        showMsg('Cannot mix Campaign Start with Reply watchers', false);
        return null;
      }
    }
    if (type === 'trigger_reply_intent') {
      if (nodes.some(n => n.node_type === 'trigger_campaign_started')) {
        showMsg('Cannot mix Reply watchers with Campaign Start — use a separate workflow', false);
        return null;
      }
    }

    const pos = layoutPosition(nodes.length);
    const n = {
      node_key: uid(),
      node_type: type,
      label: label,
      config: defaultConfig(type),
      position_x: x != null ? x : pos.x,
      position_y: y != null ? y : pos.y
    };
    nodes.push(n);

    if (autoLink !== false) autoConnect(n.node_key);

    lastAddedKey = n.node_key;
    selectNode(n.node_key);
    render();
    return n;
  }

  function deleteSelectedNode() {
    if (!selectedKey) return;
    const n = getNode(selectedKey);
    if (!n) return;
    if (n.node_type === 'trigger_campaign_started' && nodes.filter(x => x.node_type !== 'trigger_campaign_started').length > 0) {
      showMsg('Remove other nodes before deleting Start', false);
      return;
    }
    removeEdgesForNode(selectedKey);
    if (n.node_type === 'action_send_email') clearConditionRefsToNode(selectedKey);
    const idx = nodes.findIndex(x => x.node_key === selectedKey);
    nodes.splice(idx, 1);
    if (lastAddedKey === selectedKey) lastAddedKey = nodes.length ? nodes[nodes.length - 1].node_key : null;
    selectedKey = null;
    render();
  }

  function nodeEl(key) {
    return canvas.querySelector(`[data-key="${key}"]`);
  }

  function nodeSize(n) {
    const el = nodeEl(n.node_key);
    if (el) {
      return { w: el.offsetWidth || NODE_W, h: el.offsetHeight || NODE_H_FALLBACK };
    }
    return { w: NODE_W, h: NODE_H_FALLBACK };
  }

  function nodeCenter(n) {
    const { w, h } = nodeSize(n);
    return { x: n.position_x + w / 2, y: n.position_y + h / 2 };
  }

  /** Anchor point on a node side: top | bottom | left | right */
  function nodeAnchor(n, side) {
    const { w, h } = nodeSize(n);
    const x = n.position_x;
    const y = n.position_y;
    switch (side) {
      case 'left': return { x: x, y: y + h / 2 };
      case 'right': return { x: x + w, y: y + h / 2 };
      case 'top': return { x: x + w / 2, y: y };
      case 'bottom':
      default: return { x: x + w / 2, y: y + h };
    }
  }

  function pickConnectionSides(fromNode, toNode) {
    const a = nodeCenter(fromNode);
    const b = nodeCenter(toNode);
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    if (Math.abs(dx) > Math.abs(dy) * 1.15) {
      return dx >= 0 ? { from: 'right', to: 'left' } : { from: 'left', to: 'right' };
    }
    return dy >= 0 ? { from: 'bottom', to: 'top' } : { from: 'top', to: 'bottom' };
  }

  function nodeOutPoint(n, toward) {
    if (toward) {
      return nodeAnchor(n, pickConnectionSides(n, toward).from);
    }
    return nodeAnchor(n, 'bottom');
  }

  function nodeInPoint(n, from) {
    if (from) {
      return nodeAnchor(n, pickConnectionSides(from, n).to);
    }
    return nodeAnchor(n, 'top');
  }

  function edgePath(from, to, fromSide, toSide) {
    const startX = from.x;
    const startY = from.y;
    const endX = to.x;
    const endY = to.y;
    const dx = endX - startX;
    const dy = endY - startY;
    const dist = Math.hypot(dx, dy) || 1;
    const pull = Math.min(80, Math.max(28, dist * 0.35));

    function ctrl(side, x, y, outward) {
      const s = outward ? 1 : -1;
      switch (side) {
        case 'left': return { x: x - pull * s, y };
        case 'right': return { x: x + pull * s, y };
        case 'top': return { x, y: y - pull * s };
        case 'bottom':
        default: return { x, y: y + pull * s };
      }
    }

    const c1 = fromSide ? ctrl(fromSide, startX, startY, true) : { x: startX, y: (startY + endY) / 2 };
    const c2 = toSide ? ctrl(toSide, endX, endY, true) : { x: endX, y: (startY + endY) / 2 };
    return `M ${startX} ${startY} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${endX} ${endY}`;
  }

  function edgeMidpoint(from, to, fromSide, toSide) {
    const startX = from.x, startY = from.y, endX = to.x, endY = to.y;
    const dx = endX - startX;
    const dy = endY - startY;
    const dist = Math.hypot(dx, dy) || 1;
    const pull = Math.min(80, Math.max(28, dist * 0.35));
    function ctrl(side, x, y) {
      switch (side) {
        case 'left': return { x: x - pull, y };
        case 'right': return { x: x + pull, y };
        case 'top': return { x, y: y - pull };
        case 'bottom':
        default: return { x, y: y + pull };
      }
    }
    const c1 = fromSide ? ctrl(fromSide, startX, startY) : { x: startX, y: (startY + endY) / 2 };
    const c2 = toSide ? ctrl(toSide, endX, endY) : { x: endX, y: (startY + endY) / 2 };
    const t = 0.5;
    const mt = 1 - t;
    return {
      x: mt * mt * mt * startX + 3 * mt * mt * t * c1.x + 3 * mt * t * t * c2.x + t * t * t * endX,
      y: mt * mt * mt * startY + 3 * mt * mt * t * c1.y + 3 * mt * t * t * c2.y + t * t * t * endY
    };
  }

  function ensureCanvasBounds() {
    syncCanvasMinFromViewport();
    let maxX = CANVAS_MIN_W;
    let maxY = CANVAS_MIN_H;
    nodes.forEach(n => {
      const { w, h } = nodeSize(n);
      maxX = Math.max(maxX, n.position_x + w + 120);
      maxY = Math.max(maxY, n.position_y + h + 120);
    });
    canvas.style.minWidth = maxX + 'px';
    canvas.style.minHeight = maxY + 'px';
    return { w: maxX, h: maxY };
  }

  function drawEdges() {
    const { w, h } = ensureCanvasBounds();
    edgeSvg.removeAttribute('viewBox');
    edgeSvg.setAttribute('width', String(w));
    edgeSvg.setAttribute('height', String(h));
    edgeSvg.style.width = w + 'px';
    edgeSvg.style.height = h + 'px';

    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    // refX = markerWidth so the tip sits on the path endpoint (port center).
    defs.innerHTML = `
      <marker id="arrow" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#64748b"/>
      </marker>
      <marker id="arrow-true" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#16a34a"/>
      </marker>
      <marker id="arrow-false" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#dc2626"/>
      </marker>
      <marker id="arrow-hot" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#dc2626"/>
      </marker>
      <marker id="arrow-warm" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#d97706"/>
      </marker>
      <marker id="arrow-cold" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#2563eb"/>
      </marker>`;
    edgeSvg.innerHTML = '';
    edgeSvg.appendChild(defs);

    nodes.filter(n => n.node_type === 'condition_engagement').forEach(cond => {
      const params = ensureConditionParams(cond);
      if (params.email_send_scope !== 'node' || !params.email_node_key) return;
      const src = getNode(params.email_node_key);
      if (!src || src.node_type !== 'action_send_email') return;

      const sides = pickConnectionSides(cond, src);
      const from = nodeAnchor(cond, sides.from);
      const to = nodeAnchor(src, sides.to);
      const refPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      refPath.setAttribute('d', edgePath(from, to, sides.from, sides.to));
      refPath.setAttribute('fill', 'none');
      refPath.setAttribute('class', 'wf-ref-edge');
      edgeSvg.appendChild(refPath);
    });

    edges.forEach((e, edgeIndex) => {
      const fromNode = getNode(e.source_node_key);
      const toNode = getNode(e.target_node_key);
      if (!fromNode || !toNode) return;

      const sides = pickConnectionSides(fromNode, toNode);
      // Fan out multiple edges from the same source so they don't stack.
      const siblings = edges.filter(x => x.source_node_key === e.source_node_key);
      const siblingIdx = siblings.indexOf(e);
      const siblingCount = siblings.length;
      let from = nodeAnchor(fromNode, sides.from);
      let to = nodeAnchor(toNode, sides.to);
      if (siblingCount > 1) {
        const spread = 14;
        const offset = (siblingIdx - (siblingCount - 1) / 2) * spread;
        if (sides.from === 'bottom' || sides.from === 'top') from = { x: from.x + offset, y: from.y };
        else from = { x: from.x, y: from.y + offset };
        if (sides.to === 'bottom' || sides.to === 'top') to = { x: to.x + offset * 0.4, y: to.y };
        else to = { x: to.x, y: to.y + offset * 0.4 };
      }
      const path = edgePath(from, to, sides.from, sides.to);

      const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
      g.dataset.edgeIndex = String(edgeIndex);

      const hitPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      hitPath.setAttribute('d', path);
      hitPath.setAttribute('fill', 'none');
      hitPath.setAttribute('stroke', 'transparent');
      hitPath.setAttribute('stroke-width', '16');
      hitPath.setAttribute('class', 'wf-edge-hit');

      const visPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      visPath.setAttribute('d', path);
      visPath.setAttribute('fill', 'none');
      visPath.setAttribute('stroke-width', '2');
      visPath.setAttribute('class', 'wf-edge-vis');
      visPath.style.pointerEvents = 'none';

      if (e.edge_type === 'true') {
        visPath.setAttribute('stroke', '#16a34a');
        visPath.setAttribute('marker-end', 'url(#arrow-true)');
      } else if (e.edge_type === 'false') {
        visPath.setAttribute('stroke', '#dc2626');
        visPath.setAttribute('marker-end', 'url(#arrow-false)');
      } else if (e.edge_type === 'hot') {
        visPath.setAttribute('stroke', '#dc2626');
        visPath.setAttribute('marker-end', 'url(#arrow-hot)');
      } else if (e.edge_type === 'warm') {
        visPath.setAttribute('stroke', '#d97706');
        visPath.setAttribute('marker-end', 'url(#arrow-warm)');
      } else if (e.edge_type === 'cold') {
        visPath.setAttribute('stroke', '#2563eb');
        visPath.setAttribute('marker-end', 'url(#arrow-cold)');
      } else {
        visPath.setAttribute('stroke', '#64748b');
        visPath.setAttribute('marker-end', 'url(#arrow)');
      }

      if (edgeIndex === selectedEdgeIndex) {
        visPath.classList.add('wf-edge-selected');
      }

      const mid = edgeMidpoint(from, to, sides.from, sides.to);
      const deleteBtn = document.createElementNS('http://www.w3.org/2000/svg', 'g');
      deleteBtn.setAttribute('class', 'wf-edge-delete-btn');
      deleteBtn.setAttribute('transform', `translate(${mid.x},${mid.y})`);

      const deleteCircle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      deleteCircle.setAttribute('r', '10');
      deleteCircle.setAttribute('fill', '#fff');
      deleteCircle.setAttribute('stroke', '#dc2626');
      deleteCircle.setAttribute('stroke-width', '1.5');

      const deleteX = document.createElementNS('http://www.w3.org/2000/svg', 'text');
      deleteX.setAttribute('text-anchor', 'middle');
      deleteX.setAttribute('dominant-baseline', 'central');
      deleteX.setAttribute('fill', '#dc2626');
      deleteX.setAttribute('font-size', '14');
      deleteX.setAttribute('font-weight', '700');
      deleteX.textContent = '×';

      deleteBtn.appendChild(deleteCircle);
      deleteBtn.appendChild(deleteX);
      deleteBtn.addEventListener('click', (ev) => {
        ev.stopPropagation();
        ev.preventDefault();
        deleteEdge(edgeIndex);
      });

      g.addEventListener('mouseenter', () => {
        g.classList.add('wf-edge-hovering');
        visPath.classList.add('wf-edge-hover');
      });
      g.addEventListener('mouseleave', () => {
        g.classList.remove('wf-edge-hovering');
        visPath.classList.remove('wf-edge-hover');
      });
      hitPath.addEventListener('click', (ev) => {
        ev.stopPropagation();
        selectEdge(edgeIndex);
      });

      g.appendChild(hitPath);
      g.appendChild(visPath);
      g.appendChild(deleteBtn);

      if (e.edge_type === 'true' || e.edge_type === 'false' || e.edge_type === 'hot' || e.edge_type === 'warm' || e.edge_type === 'cold') {
        const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        let dx = 0;
        let color = '#64748b';
        let text = e.edge_type;
        if (e.edge_type === 'true') { dx = -18; color = '#16a34a'; text = 'yes'; }
        else if (e.edge_type === 'false') { dx = 18; color = '#dc2626'; text = 'no'; }
        else if (e.edge_type === 'hot') { dx = -24; color = '#dc2626'; text = 'hot'; }
        else if (e.edge_type === 'warm') { dx = 0; color = '#d97706'; text = 'warm'; }
        else if (e.edge_type === 'cold') { dx = 24; color = '#2563eb'; text = 'cold'; }
        label.setAttribute('x', mid.x + dx);
        label.setAttribute('y', mid.y - 10);
        label.setAttribute('fill', color);
        label.setAttribute('font-size', '11');
        label.setAttribute('font-weight', '600');
        label.style.pointerEvents = 'none';
        label.textContent = text;
        g.appendChild(label);
      }

      edgeSvg.appendChild(g);
    });

    if (previewPathEl) {
      edgeSvg.appendChild(previewPathEl);
    }
  }

  function updatePreviewLine(endX, endY) {
    if (!connectDrag) return;
    const fromNode = getNode(connectDrag.fromKey);
    const fromSide = connectDrag.fromSide || 'bottom';
    const start = fromNode
      ? nodeAnchor(fromNode, fromSide)
      : { x: connectDrag.startX, y: connectDrag.startY };
    const dx = endX - start.x;
    const dy = endY - start.y;
    let toSide = 'top';
    if (Math.abs(dx) > Math.abs(dy) * 1.15) {
      toSide = dx >= 0 ? 'left' : 'right';
    } else if (dy < 0) {
      toSide = 'bottom';
    }
    const path = edgePath(start, { x: endX, y: endY }, fromSide, toSide);
    if (!previewPathEl) {
      previewPathEl = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      previewPathEl.setAttribute('class', 'wf-edge-preview');
      previewPathEl.setAttribute('fill', 'none');
    }
    previewPathEl.setAttribute('d', path);
    drawEdges();
  }

  function clearPreviewLine() {
    previewPathEl = null;
    drawEdges();
  }

  function refreshEdgeSelects() {
    const opts = nodes.map(n => {
      const typeHint = n.node_type.replace(/_/g, ' ');
      return `<option value="${escapeAttr(n.node_key)}">${escapeHtml(n.label)} (${typeHint})</option>`;
    }).join('');

    edgeFrom.innerHTML = opts;
    edgeTo.innerHTML = opts;

    if (selectedEdgeIndex != null && edges[selectedEdgeIndex]) {
      const e = edges[selectedEdgeIndex];
      edgeFrom.value = e.source_node_key;
      edgeTo.value = e.target_node_key;
      edgeTypeSel.value = e.edge_type;
    }

    edgeCountEl.textContent = edges.length ? `(${edges.length})` : '';

    edgeListEl.innerHTML = '';
    if (edges.length === 0) {
      edgeListEmpty.classList.remove('hidden');
    } else {
      edgeListEmpty.classList.add('hidden');
      edges.forEach((e, i) => {
        const li = document.createElement('li');
        li.className = 'wf-edge-list-item' + (i === selectedEdgeIndex ? ' selected' : '');
        const badgeClass = 'wf-edge-badge wf-edge-badge-' + e.edge_type;
        li.innerHTML = `
          <span class="${badgeClass}">${EDGE_TYPE_LABELS[e.edge_type] || e.edge_type}</span>
          <span><strong>${escapeHtml(nodeLabel(e.source_node_key))}</strong>
            <span class="text-slate-400 mx-1">→</span>
            <strong>${escapeHtml(nodeLabel(e.target_node_key))}</strong></span>`;
        li.addEventListener('click', () => selectEdge(i));
        edgeListEl.appendChild(li);
      });
    }

    updateEdgeEditorUI();
  }

  function updateEdgeEditorUI() {
    const isEdit = selectedEdgeIndex != null;
    document.getElementById('btn-save-edge').classList.toggle('hidden', isEdit);
    document.getElementById('btn-update-edge').classList.toggle('hidden', !isEdit);
    document.getElementById('btn-delete-edge').classList.toggle('hidden', !isEdit);
    document.getElementById('btn-clear-edge').classList.toggle('hidden', !isEdit);
    document.getElementById('edge-editor').classList.toggle('wf-edge-editing', isEdit);
  }

  function clearEdgeSelection() {
    selectedEdgeIndex = null;
    updateEdgeEditorUI();
    refreshEdgeSelects();
    drawEdges();
  }

  function selectEdge(index) {
    if (index < 0 || index >= edges.length) return;
    selectedEdgeIndex = index;
    selectedKey = null;
    closeNodeModal();

    const e = edges[index];
    edgeFrom.value = e.source_node_key;
    edgeTo.value = e.target_node_key;
    edgeTypeSel.value = e.edge_type;
    if (edgePriorityInput) {
      edgePriorityInput.value = (e.priority != null ? e.priority : 10);
    }

    const pop = document.getElementById('wf-connections-popover');
    const btn = document.getElementById('btn-wf-connections');
    if (pop) {
      pop.classList.remove('hidden');
      if (btn) btn.setAttribute('aria-expanded', 'true');
    }

    updateEdgeEditorUI();
    refreshEdgeSelects();
    drawEdges();

    document.getElementById('edge-editor').scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  function deleteEdge(index) {
    if (index < 0 || index >= edges.length) return;
    edges.splice(index, 1);
    if (selectedEdgeIndex === index) selectedEdgeIndex = null;
    else if (selectedEdgeIndex != null && selectedEdgeIndex > index) selectedEdgeIndex--;
    render();
    showMsg('Connection removed', true);
  }

  function validateEdgeInput(from, to, type, excludeIndex) {
    if (!from || !to) {
      showMsg('Pick both a source and target node', false);
      return false;
    }
    if (from === to) {
      showMsg('A node cannot connect to itself', false);
      return false;
    }
    const src = getNode(from);
    if (src) {
      if (isTemperatureCondition(src.node_type)) {
        if (type !== 'hot' && type !== 'warm' && type !== 'cold') {
          showMsg('Temperature branches must be Hot, Warm, or Cold', false);
          return false;
        }
      } else if (isEngagementCondition(src.node_type)) {
        if (type !== 'true' && type !== 'false' && type !== 'default') {
          showMsg('Yes/No branches are only used after a Condition node', false);
          return false;
        }
      } else if (type !== 'default') {
        showMsg('Yes/No and temperature branches are only used after condition nodes', false);
        return false;
      }
    }
    const dup = edges.findIndex(e =>
      e.source_node_key === from && e.target_node_key === to && e.edge_type === type
    );
    if (dup >= 0 && dup !== excludeIndex) {
      showMsg('This connection already exists', false);
      return false;
    }
    return true;
  }

  function addEdge(from, to, type, priorityOverride = null) {
    if (!validateEdgeInput(from, to, type, null)) return false;

    let priority = 0;
    if (type === 'default') {
      if (priorityOverride != null) {
        priority = Math.max(0, parseInt(priorityOverride, 10) || 10);
      } else {
        const existingCount = edges.filter(e => e.source_node_key === from && e.edge_type === 'default').length;
        priority = 10 + existingCount * 10;
      }
    }
    edges.push({
      source_node_key: from,
      target_node_key: to,
      edge_type: type,
      priority: priority,
      condition_json: '{}'
    });
    showMsg('Connection added', true);
    return true;
  }

  function updateSelectedEdge() {
    if (selectedEdgeIndex == null) return;
    const from = edgeFrom.value;
    const to = edgeTo.value;
    const type = edgeTypeSel.value;
    if (!validateEdgeInput(from, to, type, selectedEdgeIndex)) return;

    let priority = 0;
    if (type === 'default') {
      priority = Math.max(0, parseInt(edgePriorityInput?.value, 10) || 10);
    }
    edges[selectedEdgeIndex] = {
      source_node_key: from,
      target_node_key: to,
      edge_type: type,
      priority: priority,
      condition_json: '{}'
    };
    showMsg('Connection updated', true);
    render();
  }

  function connectFromDrag(sourceKey, targetKey) {
    if (sourceKey === targetKey) return;
    const type = pickEdgeTypeForSource(sourceKey);

    if (selectedEdgeIndex != null) {
      const e = edges[selectedEdgeIndex];
      if (e.source_node_key === sourceKey && e.edge_type === type) {
        e.target_node_key = targetKey;
        showMsg('Connection target updated', true);
        render();
        return;
      }
    }

    // Allow fan-out on default edges (e.g. send -> multiple conditionals).
    // Only replace yes/no branches, where we expect uniqueness per source.
    if (type !== 'default') {
      const existingSameBranch = edges.findIndex(e =>
        e.source_node_key === sourceKey && e.edge_type === type
      );
      if (existingSameBranch >= 0) {
        edges[existingSameBranch].target_node_key = targetKey;
        selectedEdgeIndex = existingSameBranch;
        showMsg('Reconnected — replaced existing ' + (EDGE_TYPE_LABELS[type] || type) + ' branch', true);
        render();
        return;
      }
    }

    if (addEdge(sourceKey, targetKey, type, null)) {
      selectedEdgeIndex = edges.length - 1;
      render();
    }
  }

  function renderNodes() {
    canvas.querySelectorAll('.wf-node').forEach(el => el.remove());
    nodes.forEach(n => {
      const el = document.createElement('div');
      el.className = 'wf-node ' + nodeClass(n.node_type) + (n.node_key === selectedKey ? ' selected' : '');
      el.style.left = n.position_x + 'px';
      el.style.top = n.position_y + 'px';
      el.dataset.key = n.node_key;
      let subtitle = `<p class="text-xs text-slate-400 mt-0.5">${escapeHtml(n.node_type)}</p>`;
      if (n.node_type === 'action_send_email') {
        const tid = (n.config && n.config.template_id) || 0;
        subtitle = tid
          ? `<p class="text-xs text-blue-700 mt-0.5 font-medium">${escapeHtml(templateName(tid))}</p>`
          : `<p class="text-xs text-amber-700 mt-0.5">Pick a template</p>`;
      } else if (n.node_type === 'trigger_reply_intent') {
        const intent = (n.config && n.config.intent) || 'interested';
        subtitle = `<p class="text-xs text-violet-700 mt-0.5 font-medium">Watch: ${escapeHtml(String(intent).replace(/_/g, ' '))}</p>`;
      } else if (n.node_type === 'action_wait') {
        const secs = (n.config && n.config.duration_seconds) || 90;
        const waitLabel = secs < 3600 ? Math.round(secs / 60) + ' min' : (secs / 86400) + ' days';
        subtitle = `<p class="text-xs text-slate-500 mt-0.5">${escapeHtml(waitLabel)}</p>`;
      } else if (n.node_type === 'condition_engagement') {
        subtitle = `<p class="text-xs text-amber-700 mt-0.5 font-medium">re: ${escapeHtml(conditionEmailRefLabel(n))}</p>
          <p class="text-xs text-slate-400">${escapeHtml(conditionPredicateSummary(n))}</p>`;
      } else if (n.node_type === 'condition_temperature') {
        subtitle = `<p class="text-xs text-amber-700 mt-0.5 font-medium">Campaign lead temperature</p>
          <p class="text-xs text-slate-400">Hot / warm / cold</p>`;
      }
      el.innerHTML = `
        <div class="wf-port wf-port-in" title="Connect here (input)"></div>
        <div class="wf-port wf-port-left" title="Connect here (side)"></div>
        <div class="wf-port wf-port-right" title="Drag to connect (side)"></div>
        <p class="font-semibold text-sm ${nodeColorClass(n.node_type)}">${escapeHtml(n.label)}</p>
        ${subtitle}
        <div class="wf-port wf-port-out" title="Drag to connect (output)"></div>`;

      el.querySelectorAll('.wf-port-out, .wf-port-right').forEach(portOut => {
        portOut.addEventListener('mousedown', (ev) => startConnectDrag(ev, n.node_key));
      });

      el.addEventListener('mousedown', (ev) => {
        if (ev.target.closest('.wf-port')) return;
        startNodeDrag(ev, n.node_key);
      });
      el.addEventListener('mouseenter', () => {
        if (connectDrag && connectDrag.fromKey !== n.node_key) el.classList.add('connect-target');
      });
      el.addEventListener('mouseleave', () => el.classList.remove('connect-target'));
      el.addEventListener('mouseup', (ev) => {
        if (connectDrag && connectDrag.fromKey !== n.node_key) {
          ev.stopPropagation();
          connectFromDrag(connectDrag.fromKey, n.node_key);
          endConnectDrag();
        }
      });

      el.addEventListener('click', (ev) => {
        if (ev.target.closest('.wf-port')) return;
        if (suppressNodeClick) {
          suppressNodeClick = false;
          ev.preventDefault();
          ev.stopPropagation();
          return;
        }
        if (dragState && dragState.moved) return;
        ev.stopPropagation();
        clearEdgeSelection();
        selectNode(n.node_key);
      });

      canvas.appendChild(el);
    });
    drawEdges();
    // Re-measure after layout so tall condition nodes get correct port anchors.
    requestAnimationFrame(() => drawEdges());
    refreshEdgeSelects();
  }

  function render() {
    renderNodes();
  }

  function escapeHtml(s) {
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/"/g, '&quot;');
  }

  function startConnectDrag(ev, key) {
    ev.preventDefault();
    ev.stopPropagation();
    const n = getNode(key);
    if (!n) return;
    // Prefer the port the user grabbed so the preview starts on that handle.
    let side = 'bottom';
    if (ev.currentTarget && ev.currentTarget.classList.contains('wf-port-right')) side = 'right';
    else if (ev.currentTarget && ev.currentTarget.classList.contains('wf-port-left')) side = 'left';
    else if (ev.currentTarget && ev.currentTarget.classList.contains('wf-port-in')) side = 'top';
    const pt = nodeAnchor(n, side);
    connectDrag = { fromKey: key, startX: pt.x, startY: pt.y, fromSide: side };
    document.addEventListener('mousemove', onConnectDrag);
    document.addEventListener('mouseup', onConnectDragEnd);
  }

  function onConnectDrag(ev) {
    if (!connectDrag) return;
    const rect = canvas.getBoundingClientRect();
    const x = ev.clientX - rect.left + canvasWrap.scrollLeft;
    const y = ev.clientY - rect.top + canvasWrap.scrollTop;
    updatePreviewLine(x, y);
  }

  function onConnectDragEnd(ev) {
    if (!connectDrag) return;
    const targetEl = document.elementFromPoint(ev.clientX, ev.clientY);
    const nodeEl = targetEl && targetEl.closest('.wf-node');
    if (nodeEl && nodeEl.dataset.key !== connectDrag.fromKey) {
      connectFromDrag(connectDrag.fromKey, nodeEl.dataset.key);
    }
    endConnectDrag();
  }

  function endConnectDrag() {
    connectDrag = null;
    clearPreviewLine();
    canvas.querySelectorAll('.wf-node.connect-target').forEach(el => el.classList.remove('connect-target'));
    document.removeEventListener('mousemove', onConnectDrag);
    document.removeEventListener('mouseup', onConnectDragEnd);
  }

  function startNodeDrag(ev, key) {
    if (ev.button !== 0) return;
    ev.preventDefault();
    ev.stopPropagation();
    const n = getNode(key);
    if (!n) return;

    const rect = canvas.getBoundingClientRect();
    dragState = {
      key,
      moved: false,
      startX: ev.clientX,
      startY: ev.clientY,
      offsetX: ev.clientX - rect.left - n.position_x + canvasWrap.scrollLeft,
      offsetY: ev.clientY - rect.top - n.position_y + canvasWrap.scrollTop
    };

    const el = canvas.querySelector(`[data-key="${key}"]`);
    if (el) el.classList.add('dragging');

    document.addEventListener('mousemove', onNodeDrag);
    document.addEventListener('mouseup', endNodeDrag);
  }

  function onNodeDrag(ev) {
    if (!dragState) return;
    const dx = Math.abs(ev.clientX - dragState.startX);
    const dy = Math.abs(ev.clientY - dragState.startY);
    if (!dragState.moved && dx < 4 && dy < 4) return;
    dragState.moved = true;
    const n = getNode(dragState.key);
    if (!n) return;

    const rect = canvas.getBoundingClientRect();
    n.position_x = Math.max(0, ev.clientX - rect.left - dragState.offsetX + canvasWrap.scrollLeft);
    n.position_y = Math.max(0, ev.clientY - rect.top - dragState.offsetY + canvasWrap.scrollTop);

    const el = canvas.querySelector(`[data-key="${dragState.key}"]`);
    if (el) {
      el.style.left = n.position_x + 'px';
      el.style.top = n.position_y + 'px';
    }
    drawEdges();
  }

  function endNodeDrag() {
    if (dragState) {
      if (dragState.moved) suppressNodeClick = true;
      const el = canvas.querySelector(`[data-key="${dragState.key}"]`);
      if (el) el.classList.remove('dragging');
    }
    dragState = null;
    document.removeEventListener('mousemove', onNodeDrag);
    document.removeEventListener('mouseup', endNodeDrag);
  }

  function selectNode(key) {
    selectedKey = key;
    const n = getNode(key);
    if (!n) return;

    renderNodes();

    const title = document.getElementById('wf-node-modal-title');
    const fields = document.getElementById('wf-node-modal-fields');
    if (!fields) return;

    const typeTitle = {
      trigger_reply_intent: 'Watch reply',
      action_wait: 'Wait',
      action_send_email: 'Send email',
      action_end: 'End'
    };
    if (title) title.textContent = typeTitle[n.node_type] || n.label || 'Configure node';

    fields.innerHTML = `<label class="form-label">Label</label>
      <input class="form-input" id="prop-label" value="${escapeAttr(n.label)}">`;

    if (n.node_type === 'trigger_reply_intent') {
      const intents = (typeof REPLY_INTENTS !== 'undefined' && REPLY_INTENTS.length)
        ? REPLY_INTENTS
        : ['interested', 'meeting_ask', 'pricing', 'referral', 'not_now', 'ooo', 'unsubscribe', 'wrong_person'];
      let opts = intents.map(i => `<option value="${escapeAttr(i)}" ${(n.config.intent || 'interested') === i ? 'selected' : ''}>${escapeHtml(String(i).replace(/_/g, ' '))}</option>`).join('');
      fields.innerHTML += `<label class="form-label mt-2">What to watch</label><select class="form-input" id="prop-intent">${opts}</select>
        <p class="text-xs text-slate-500 mt-1">When a reply is classified with this intent, this branch runs.</p>`;
      document.getElementById('prop-intent').onchange = e => {
        n.config.intent = e.target.value;
        n.label = 'Watch: ' + String(e.target.value).replace(/_/g, ' ');
        document.getElementById('prop-label').value = n.label;
        markWorkflowDirty();
        render();
      };
    }
    if (n.node_type === 'action_wait') {
      const secs = n.config.duration_seconds || 90;
      const useDays = secs >= 86400;
      const amount = useDays ? Math.max(1, Math.round(secs / 86400)) : Math.max(1, Math.round(secs / 60));
      fields.innerHTML += `
        <div class="grid grid-cols-2 gap-2 mt-2">
          <div>
            <label class="form-label">How long</label>
            <input type="number" min="1" class="form-input" id="prop-wait-amount" value="${amount}">
          </div>
          <div>
            <label class="form-label">Unit</label>
            <select class="form-input" id="prop-wait-unit">
              <option value="minutes" ${!useDays ? 'selected' : ''}>Minutes</option>
              <option value="days" ${useDays ? 'selected' : ''}>Days</option>
            </select>
          </div>
        </div>
        <p class="text-xs text-slate-500 mt-1">Reply watchers usually wait 1–2 minutes before sending.</p>`;
      const syncWait = () => {
        const amt = Math.max(1, parseInt(document.getElementById('prop-wait-amount').value, 10) || 1);
        const unit = document.getElementById('prop-wait-unit').value;
        n.config.duration_seconds = unit === 'days' ? amt * 86400 : amt * 60;
        markWorkflowDirty();
        render();
      };
      document.getElementById('prop-wait-amount').onchange = syncWait;
      document.getElementById('prop-wait-unit').onchange = syncWait;
    }
    if (n.node_type === 'action_send_email') {
      const tid = parseInt(n.config.template_id, 10) || 0;
      let tplOpts = '<option value="0">Select template…</option>';
      (TEMPLATES || []).forEach(t => {
        tplOpts += `<option value="${t.id}" ${t.id === tid ? 'selected' : ''}>${escapeHtml(t.name)}</option>`;
      });
      if (!(TEMPLATES || []).length) {
        tplOpts += '<option value="0" disabled>No templates in this folder</option>';
      }
      const openOn = !!n.config.open_tracking;
      const clickOn = !!n.config.click_tracking;
      fields.innerHTML += `
        <label class="form-label mt-2">Template to send</label>
        <select class="form-input" id="prop-template">${tplOpts}</select>
        <p class="text-xs text-slate-500 mt-1">Only templates from this campaign folder are listed.</p>
        <label class="flex items-center gap-2 text-sm text-slate-700 mt-3">
          <input type="checkbox" id="prop-open-track" ${openOn ? 'checked' : ''}>
          Track opens (pixel)
        </label>
        <label class="flex items-center gap-2 text-sm text-slate-700 mt-2">
          <input type="checkbox" id="prop-click-track" ${clickOn ? 'checked' : ''}>
          Track link clicks
        </label>`;
      document.getElementById('prop-template').onchange = e => {
        n.config.template_id = parseInt(e.target.value, 10) || 0;
        if (n.config.template_id) {
          n.label = templateName(n.config.template_id);
          const labelEl = document.getElementById('prop-label');
          if (labelEl) labelEl.value = n.label;
        }
        markWorkflowDirty();
        render();
      };
      document.getElementById('prop-open-track').onchange = e => {
        n.config.open_tracking = !!e.target.checked;
        markWorkflowDirty();
        render();
      };
      document.getElementById('prop-click-track').onchange = e => {
        n.config.click_tracking = !!e.target.checked;
        markWorkflowDirty();
        render();
      };
    }
    if (n.node_type === 'action_end') {
      fields.innerHTML += `<p class="text-sm text-slate-600 mt-2">Ends this reply-watcher branch.</p>`;
    }

    document.getElementById('prop-label').onchange = e => {
      n.label = e.target.value;
      markWorkflowDirty();
      render();
    };
    openNodeModal();
  }

  let paletteDidDrag = false;
  document.querySelectorAll('.palette-item').forEach(item => {
    item.addEventListener('click', () => {
      if (paletteDidDrag) {
        paletteDidDrag = false;
        return;
      }
      addNode(item.dataset.type, item.dataset.label);
    });

    item.addEventListener('dragstart', (ev) => {
      paletteDidDrag = true;
      ev.dataTransfer.setData('application/workflow-node', JSON.stringify({
        type: item.dataset.type,
        label: item.dataset.label
      }));
      ev.dataTransfer.effectAllowed = 'copy';
    });

    item.addEventListener('dragend', () => {
      setTimeout(() => { paletteDidDrag = false; }, 0);
    });
  });

  canvasWrap.addEventListener('dragover', (ev) => {
    ev.preventDefault();
    ev.dataTransfer.dropEffect = 'copy';
    canvasWrap.classList.add('drag-over');
  });

  canvasWrap.addEventListener('dragleave', (ev) => {
    if (!canvasWrap.contains(ev.relatedTarget)) canvasWrap.classList.remove('drag-over');
  });

  canvasWrap.addEventListener('drop', (ev) => {
    ev.preventDefault();
    canvasWrap.classList.remove('drag-over');
    const raw = ev.dataTransfer.getData('application/workflow-node');
    if (!raw) return;
    const data = JSON.parse(raw);
    const rect = canvas.getBoundingClientRect();
    const x = ev.clientX - rect.left + canvasWrap.scrollLeft - NODE_W / 2;
    const y = ev.clientY - rect.top + canvasWrap.scrollTop - NODE_H_FALLBACK / 2;
    addNode(data.type, data.label, Math.max(8, x), Math.max(8, y));
  });

  canvasWrap.addEventListener('click', () => {
    if (!dragState || !dragState.moved) {
      clearEdgeSelection();
      selectedKey = null;
      renderNodes();
    }
  });

  document.getElementById('wf-node-modal-done')?.addEventListener('click', () => {
    closeNodeModal();
  });
  document.getElementById('wf-node-modal-delete')?.addEventListener('click', () => {
    deleteSelectedNode();
    closeNodeModal();
  });
  document.getElementById('wf-node-modal')?.addEventListener('click', (ev) => {
    if (ev.target === ev.currentTarget) closeNodeModal();
  });

  document.getElementById('btn-save-edge').addEventListener('click', () => {
    const from = edgeFrom.value;
    const to = edgeTo.value;
    const type = edgeTypeSel.value;
    const prio = edgePriorityInput ? parseInt(edgePriorityInput.value, 10) : null;
    if (addEdge(from, to, type, prio)) render();
  });

  document.getElementById('btn-update-edge').addEventListener('click', updateSelectedEdge);
  document.getElementById('btn-delete-edge').addEventListener('click', () => {
    if (selectedEdgeIndex != null) deleteEdge(selectedEdgeIndex);
  });
  document.getElementById('btn-clear-edge').addEventListener('click', clearEdgeSelection);

  edgeFrom.addEventListener('change', () => {
    const src = getNode(edgeFrom.value);
    if (!src) return;
    if (isTemperatureCondition(src.node_type)) {
      if (edgeTypeSel.value !== 'hot' && edgeTypeSel.value !== 'warm' && edgeTypeSel.value !== 'cold') {
        edgeTypeSel.value = 'hot';
      }
    } else if (!isEngagementCondition(src.node_type) && edgeTypeSel.value !== 'default') {
      edgeTypeSel.value = 'default';
    }
  });

  async function loadGraph() {
    const res = await fetch('/api/v1/workflows/' + WORKFLOW_ID + '/versions/' + VERSION_ID);
    const data = await res.json();
    const nodeList = data.Nodes || data.nodes || [];
    const edgeList = data.Edges || data.edges || [];

    nodeList.forEach(n => {
      const config = JSON.parse(n.ConfigJSON || n.config_json || '{}');
      if ((n.NodeType || n.node_type) === 'condition_engagement') {
        if (!config.params) config.params = { email_send_scope: 'last_in_workflow' };
        if (!config.predicate) config.predicate = 'has_replied';
        if ((config.predicate === 'has_not_opened' || config.predicate === 'has_not_replied') && !config.params.wait_days) {
          config.params.wait_days = 3;
        }
      }
      nodes.push({
        node_key: n.NodeKey || n.node_key,
        node_type: n.NodeType || n.node_type,
        label: n.Label || n.label,
        config,
        position_x: n.PositionX ?? n.position_x ?? 0,
        position_y: n.PositionY ?? n.position_y ?? 0
      });
    });
    edgeList.forEach(e => {
      edges.push({
        source_node_key: e.SourceNodeKey || e.source_node_key,
        target_node_key: e.TargetNodeKey || e.target_node_key,
        edge_type: e.EdgeType || e.edge_type || 'default',
        priority: e.Priority || e.priority || 0,
        condition_json: e.ConditionJSON || e.condition_json || '{}'
      });
    });

    if (nodes.length === 0) {
      addNode('trigger_reply_intent', 'Watch: interested', 60, 60, false);
    } else {
      lastAddedKey = nodes[nodes.length - 1].node_key;
      render();
    }
  }

  // Save persists + validates + publishes. Broken graphs are rejected (not published).
  async function saveGraph() {
    if (typeof WORKFLOW_ARCHIVED !== 'undefined' && WORKFLOW_ARCHIVED) {
      showMsg('Archived workflows cannot be saved', false);
      return false;
    }
    const body = {
      nodes: nodes.map(n => ({
        node_key: n.node_key,
        node_type: n.node_type,
        label: n.label,
        config_json: JSON.stringify(n.config),
        position_x: n.position_x,
        position_y: n.position_y
      })),
      edges: edges.map(e => ({
        source_node_key: e.source_node_key,
        target_node_key: e.target_node_key,
        edge_type: e.edge_type,
        priority: e.priority,
        condition_json: e.condition_json
      }))
    };
    const res = await fetch('/api/v1/workflows/' + WORKFLOW_ID + '/versions/' + VERSION_ID, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      const detail = (data.errors && data.errors.length) ? data.errors.join('; ') : (data.error || 'Save failed');
      showMsg(detail, false);
      return false;
    }
    if (data.version_id) {
      VERSION_ID = data.version_id;
    }
    showMsg('Saved', true);
    workflowDirty = false;
    setTimeout(() => location.reload(), 500);
    return true;
  }

  document.getElementById('canvas')?.addEventListener('mousedown', markWorkflowDirty);
  document.getElementById('btn-save')?.addEventListener('click', saveGraph);

  (function wireConnectionsPopover() {
    const btn = document.getElementById('btn-wf-connections');
    const pop = document.getElementById('wf-connections-popover');
    const closeBtn = document.getElementById('btn-wf-connections-close');
    if (!btn || !pop) return;
    function setOpen(open) {
      pop.classList.toggle('hidden', !open);
      btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    }
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      setOpen(pop.classList.contains('hidden'));
    });
    closeBtn?.addEventListener('click', () => setOpen(false));
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !pop.classList.contains('hidden')) setOpen(false);
    });
  })();

  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
      syncCanvasMinFromViewport();
      if (nodes.length) render();
    }, 80);
  });

  syncCanvasMinFromViewport();
  loadGraph();
})();
