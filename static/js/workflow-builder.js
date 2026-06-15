(function () {
  const nodes = [];
  const edges = [];
  let selectedKey = null;
  let selectedEdgeIndex = null;
  let lastAddedKey = null;

  const canvas = document.getElementById('canvas');
  const canvasWrap = document.getElementById('canvas-wrap');
  const edgeSvg = document.getElementById('edge-svg');
  const edgeFrom = document.getElementById('edge-from');
  const edgeTo = document.getElementById('edge-to');
  const edgeTypeSel = document.getElementById('edge-type');
  const edgeListEl = document.getElementById('edge-list');
  const edgeListEmpty = document.getElementById('edge-list-empty');
  const edgeCountEl = document.getElementById('edge-count');

  const NODE_W = 168;
  const NODE_H = 56;

  let dragState = null;
  let connectDrag = null;
  let previewPathEl = null;

  const EDGE_TYPE_LABELS = {
    default: 'Then → next',
    true: 'If yes',
    false: 'If no'
  };

  function uid() {
    return 'n-' + Math.random().toString(36).slice(2, 10);
  }

  function showMsg(text, ok) {
    const el = document.getElementById('builder-msg');
    el.className = ok ? 'alert-success mb-4' : 'alert-error mb-4';
    el.textContent = text;
    el.classList.remove('hidden');
  }

  function defaultConfig(type) {
    if (type === 'action_send_email') return { template_id: TEMPLATES[0]?.id || 0 };
    if (type === 'action_wait') return { duration_seconds: 259200 };
    if (type === 'condition_engagement') return { predicate: 'has_opened', params: { email_send_scope: 'last_in_workflow' } };
    return {};
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
    if (!src || src.node_type !== 'condition_engagement') return 'default';
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
        showMsg('Only one Start trigger allowed', false);
        selectNode(existing.node_key);
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
    const idx = nodes.findIndex(x => x.node_key === selectedKey);
    nodes.splice(idx, 1);
    if (lastAddedKey === selectedKey) lastAddedKey = nodes.length ? nodes[nodes.length - 1].node_key : null;
    selectedKey = null;
    document.getElementById('node-props').classList.add('hidden');
    render();
  }

  function nodeOutPoint(n) {
    return { x: n.position_x + NODE_W / 2, y: n.position_y + NODE_H };
  }

  function nodeInPoint(n) {
    return { x: n.position_x + NODE_W / 2, y: n.position_y };
  }

  function nodeCenter(n) {
    return { x: n.position_x + NODE_W / 2, y: n.position_y + NODE_H / 2 };
  }

  function edgePath(from, to) {
    const startX = from.x;
    const startY = from.y;
    const endX = to.x;
    const endY = to.y;
    const midY = (startY + endY) / 2;
    return `M ${startX} ${startY} C ${startX} ${midY}, ${endX} ${midY}, ${endX} ${endY}`;
  }

  function drawEdges() {
    const w = Math.max(canvasWrap.scrollWidth, 1200);
    const h = Math.max(canvasWrap.scrollHeight, 560);
    edgeSvg.setAttribute('width', w);
    edgeSvg.setAttribute('height', h);
    edgeSvg.setAttribute('viewBox', `0 0 ${w} ${h}`);

    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    defs.innerHTML = `
      <marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
        <path d="M0,0 L0,6 L8,3 z" fill="#64748b"/>
      </marker>
      <marker id="arrow-true" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
        <path d="M0,0 L0,6 L8,3 z" fill="#16a34a"/>
      </marker>
      <marker id="arrow-false" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
        <path d="M0,0 L0,6 L8,3 z" fill="#dc2626"/>
      </marker>`;
    edgeSvg.innerHTML = '';
    edgeSvg.appendChild(defs);

    edges.forEach((e, edgeIndex) => {
      const fromNode = getNode(e.source_node_key);
      const toNode = getNode(e.target_node_key);
      if (!fromNode || !toNode) return;

      const from = nodeOutPoint(fromNode);
      const to = nodeInPoint(toNode);
      const path = edgePath(from, to);

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
      } else {
        visPath.setAttribute('stroke', '#64748b');
        visPath.setAttribute('marker-end', 'url(#arrow)');
      }

      if (edgeIndex === selectedEdgeIndex) {
        visPath.classList.add('wf-edge-selected');
      }

      hitPath.addEventListener('mouseenter', () => visPath.classList.add('wf-edge-hover'));
      hitPath.addEventListener('mouseleave', () => visPath.classList.remove('wf-edge-hover'));
      hitPath.addEventListener('click', (ev) => {
        ev.stopPropagation();
        selectEdge(edgeIndex);
      });
      hitPath.addEventListener('contextmenu', (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        deleteEdge(edgeIndex);
      });

      g.appendChild(hitPath);
      g.appendChild(visPath);

      if (e.edge_type === 'true' || e.edge_type === 'false') {
        const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        label.setAttribute('x', (from.x + to.x) / 2 + (e.edge_type === 'true' ? -18 : 18));
        label.setAttribute('y', (from.y + to.y) / 2);
        label.setAttribute('fill', e.edge_type === 'true' ? '#16a34a' : '#dc2626');
        label.setAttribute('font-size', '11');
        label.setAttribute('font-weight', '600');
        label.style.pointerEvents = 'none';
        label.textContent = e.edge_type === 'true' ? 'yes' : 'no';
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
    const path = edgePath(
      { x: connectDrag.startX, y: connectDrag.startY },
      { x: endX, y: endY }
    );
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
    document.getElementById('node-props').classList.add('hidden');

    const e = edges[index];
    edgeFrom.value = e.source_node_key;
    edgeTo.value = e.target_node_key;
    edgeTypeSel.value = e.edge_type;

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
    if (src && src.node_type !== 'condition_engagement' && type !== 'default') {
      showMsg('Yes/No branches are only used after a Condition node', false);
      return false;
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

  function addEdge(from, to, type) {
    if (!validateEdgeInput(from, to, type, null)) return false;
    edges.push({
      source_node_key: from,
      target_node_key: to,
      edge_type: type,
      priority: 0,
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

    edges[selectedEdgeIndex] = {
      source_node_key: from,
      target_node_key: to,
      edge_type: type,
      priority: 0,
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

    if (addEdge(sourceKey, targetKey, type)) {
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
      el.innerHTML = `
        <div class="wf-port wf-port-in" title="Connect here (input)"></div>
        <p class="font-semibold text-sm ${nodeColorClass(n.node_type)}">${escapeHtml(n.label)}</p>
        <p class="text-xs text-slate-400 mt-0.5">${escapeHtml(n.node_type)}</p>
        <div class="wf-port wf-port-out" title="Drag to connect (output)"></div>`;

      const portOut = el.querySelector('.wf-port-out');
      portOut.addEventListener('mousedown', (ev) => startConnectDrag(ev, n.node_key));

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
        if (dragState && dragState.moved) return;
        ev.stopPropagation();
        clearEdgeSelection();
        selectNode(n.node_key);
      });

      canvas.appendChild(el);
    });
    drawEdges();
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
    const pt = nodeOutPoint(n);
    connectDrag = { fromKey: key, startX: pt.x, startY: pt.y };
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

    const panel = document.getElementById('node-props');
    const fields = document.getElementById('node-props-fields');
    panel.classList.remove('hidden');
    fields.innerHTML = `<label class="form-label">Label</label>
      <input class="form-input" id="prop-label" value="${escapeAttr(n.label)}">`;

    if (n.node_type === 'action_send_email') {
      const opts = TEMPLATES.map(t => `<option value="${t.id}">${escapeHtml(t.name)}</option>`).join('');
      fields.innerHTML += `<label class="form-label mt-2">Template</label><select class="form-input" id="prop-template">${opts}</select>`;
      const sel = document.getElementById('prop-template');
      sel.value = n.config.template_id || TEMPLATES[0]?.id;
      sel.onchange = e => { n.config.template_id = parseInt(e.target.value, 10); };
    }
    if (n.node_type === 'action_wait') {
      const days = (n.config.duration_seconds || 86400) / 86400;
      fields.innerHTML += `<label class="form-label mt-2">Wait (days)</label><input type="number" min="1" class="form-input" id="prop-days" value="${days}">`;
      document.getElementById('prop-days').onchange = e => { n.config.duration_seconds = parseInt(e.target.value, 10) * 86400; };
    }
    if (n.node_type === 'condition_engagement') {
      fields.innerHTML += `<label class="form-label mt-2">Predicate</label>
        <select class="form-input" id="prop-predicate">
          <option value="has_opened">Has opened</option>
          <option value="has_not_opened">Has not opened</option>
          <option value="click_count_gte">Click count ≥</option>
          <option value="has_replied">Has replied</option>
        </select>`;
      const sel = document.getElementById('prop-predicate');
      sel.value = n.config.predicate || 'has_opened';
      sel.onchange = e => { n.config.predicate = e.target.value; };
    }
    document.getElementById('prop-label').onchange = e => { n.label = e.target.value; render(); };
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
    const y = ev.clientY - rect.top + canvasWrap.scrollTop - NODE_H / 2;
    addNode(data.type, data.label, Math.max(8, x), Math.max(8, y));
  });

  canvasWrap.addEventListener('click', () => {
    if (!dragState || !dragState.moved) {
      clearEdgeSelection();
      selectedKey = null;
      document.getElementById('node-props').classList.add('hidden');
      renderNodes();
    }
  });

  document.getElementById('btn-save-edge').addEventListener('click', () => {
    const from = edgeFrom.value;
    const to = edgeTo.value;
    const type = edgeTypeSel.value;
    if (addEdge(from, to, type)) render();
  });

  document.getElementById('btn-update-edge').addEventListener('click', updateSelectedEdge);
  document.getElementById('btn-delete-edge').addEventListener('click', () => {
    if (selectedEdgeIndex != null) deleteEdge(selectedEdgeIndex);
  });
  document.getElementById('btn-clear-edge').addEventListener('click', clearEdgeSelection);

  edgeFrom.addEventListener('change', () => {
    const src = getNode(edgeFrom.value);
    if (src && src.node_type !== 'condition_engagement' && edgeTypeSel.value !== 'default') {
      edgeTypeSel.value = 'default';
    }
  });

  document.getElementById('btn-delete-node')?.addEventListener('click', deleteSelectedNode);

  async function loadGraph() {
    const res = await fetch('/api/v1/workflows/' + WORKFLOW_ID + '/versions/' + VERSION_ID);
    const data = await res.json();
    const nodeList = data.Nodes || data.nodes || [];
    const edgeList = data.Edges || data.edges || [];

    nodeList.forEach(n => {
      nodes.push({
        node_key: n.NodeKey || n.node_key,
        node_type: n.NodeType || n.node_type,
        label: n.Label || n.label,
        config: JSON.parse(n.ConfigJSON || n.config_json || '{}'),
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
      addNode('trigger_campaign_started', 'Campaign Start', 60, 60, false);
    } else {
      lastAddedKey = nodes[nodes.length - 1].node_key;
      render();
    }
  }

  async function saveGraph() {
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
    const data = await res.json();
    if (!res.ok) showMsg(data.error || 'Save failed', false);
    else showMsg('Workflow saved', true);
  }

  document.getElementById('btn-save').addEventListener('click', saveGraph);

  document.getElementById('btn-validate').addEventListener('click', async () => {
    await saveGraph();
    const res = await fetch('/api/v1/workflows/' + WORKFLOW_ID + '/versions/' + VERSION_ID + '/validate', { method: 'POST' });
    const data = await res.json();
    if (data.valid) showMsg('Workflow is valid', true);
    else showMsg((data.errors || []).join('; '), false);
  });

  document.getElementById('btn-publish').addEventListener('click', async () => {
    await saveGraph();
    const res = await fetch('/api/v1/workflows/' + WORKFLOW_ID + '/versions/' + VERSION_ID + '/publish', { method: 'POST' });
    const data = await res.json();
    if (!res.ok) showMsg(data.error || 'Publish failed', false);
    else showMsg('Workflow published', true);
  });

  loadGraph();
})();
