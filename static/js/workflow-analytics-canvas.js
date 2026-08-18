/**
 * Read-only analytics canvas — same layout/edges as the workflow builder.
 * Expects #wf-analytics-canvas-data (JSON) and #wf-analytics-canvas-wrap.
 */
(function () {
  const wrap = document.getElementById('wf-analytics-canvas-wrap');
  const canvas = document.getElementById('wf-analytics-canvas');
  const edgeSvg = document.getElementById('wf-analytics-edge-svg');
  if (!wrap || !canvas || !edgeSvg) return;

  const data = window.__WF_ANALYTICS_CANVAS__;
  if (!data || !data.nodes) return;

  const NODE_W = 188;
  const NODE_H_FALLBACK = 110;

  function nodeClass(type) {
    if (!type) return 'wf-node-type-action';
    if (type.startsWith('trigger')) return 'wf-node-type-trigger';
    if (type.startsWith('condition')) return 'wf-node-type-condition';
    if (type === 'action_end') return 'wf-node-type-end';
    return 'wf-node-type-action';
  }

  function kindLabel(type) {
    if (type === 'action_send_email') return 'Email';
    if (type === 'action_wait') return 'Wait';
    if (type === 'condition_engagement') return 'Branch';
    if (type === 'condition_temperature') return 'Temperature';
    if (type === 'trigger_campaign_started') return 'Start';
    if (type === 'action_end') return 'End';
    return 'Step';
  }

  function escapeHtml(s) {
    const d = document.createElement('div');
    d.textContent = s == null ? '' : String(s);
    return d.innerHTML;
  }

  function getNode(key) {
    return (data.nodes || []).find((n) => n.node_key === key);
  }

  function measuredSize(n) {
      const el = canvas.querySelector('.wf-node[data-key="' + n.node_key.replace(/"/g, '') + '"]');
    if (el) {
      return { w: el.offsetWidth || NODE_W, h: el.offsetHeight || NODE_H_FALLBACK };
    }
    return { w: NODE_W, h: NODE_H_FALLBACK };
  }

  function nodeAnchor(n, side) {
    const size = measuredSize(n);
    const x = n.position_x;
    const y = n.position_y;
    if (side === 'top') return { x: x + size.w / 2, y: y };
    if (side === 'bottom') return { x: x + size.w / 2, y: y + size.h };
    if (side === 'left') return { x: x, y: y + size.h / 2 };
    return { x: x + size.w, y: y + size.h / 2 };
  }

  function pickConnectionSides(from, to) {
    const dx = to.position_x - from.position_x;
    const dy = to.position_y - from.position_y;
    if (Math.abs(dy) >= Math.abs(dx)) {
      return dy >= 0 ? { from: 'bottom', to: 'top' } : { from: 'top', to: 'bottom' };
    }
    return dx >= 0 ? { from: 'right', to: 'left' } : { from: 'left', to: 'right' };
  }

  function edgePath(from, to, fromSide, toSide) {
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    let c1x = from.x;
    let c1y = from.y;
    let c2x = to.x;
    let c2y = to.y;
    const pull = Math.max(40, Math.min(120, Math.hypot(dx, dy) * 0.4));
    if (fromSide === 'bottom') c1y += pull;
    else if (fromSide === 'top') c1y -= pull;
    else if (fromSide === 'right') c1x += pull;
    else c1x -= pull;
    if (toSide === 'bottom') c2y += pull;
    else if (toSide === 'top') c2y -= pull;
    else if (toSide === 'right') c2x += pull;
    else c2x -= pull;
    return `M ${from.x} ${from.y} C ${c1x} ${c1y}, ${c2x} ${c2y}, ${to.x} ${to.y}`;
  }

  function edgeMidpoint(from, to, fromSide, toSide) {
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    let c1x = from.x;
    let c1y = from.y;
    let c2x = to.x;
    let c2y = to.y;
    const pull = Math.max(40, Math.min(120, Math.hypot(dx, dy) * 0.4));
    if (fromSide === 'bottom') c1y += pull;
    else if (fromSide === 'top') c1y -= pull;
    else if (fromSide === 'right') c1x += pull;
    else c1x -= pull;
    if (toSide === 'bottom') c2y += pull;
    else if (toSide === 'top') c2y -= pull;
    else if (toSide === 'right') c2x += pull;
    else c2x -= pull;
    const t = 0.5;
    const mt = 1 - t;
    return {
      x: mt * mt * mt * from.x + 3 * mt * mt * t * c1x + 3 * mt * t * t * c2x + t * t * t * to.x,
      y: mt * mt * mt * from.y + 3 * mt * mt * t * c1y + 3 * mt * t * t * c2y + t * t * t * to.y
    };
  }

  function strokeFor(type) {
    if (type === 'true') return { stroke: '#16a34a', marker: 'url(#wf-a-arrow-true)' };
    if (type === 'false') return { stroke: '#dc2626', marker: 'url(#wf-a-arrow-false)' };
    if (type === 'hot') return { stroke: '#dc2626', marker: 'url(#wf-a-arrow-hot)' };
    if (type === 'warm') return { stroke: '#d97706', marker: 'url(#wf-a-arrow-warm)' };
    if (type === 'cold') return { stroke: '#2563eb', marker: 'url(#wf-a-arrow-cold)' };
    return { stroke: '#64748b', marker: 'url(#wf-a-arrow)' };
  }

  function ensureBounds() {
    let maxX = data.width || 1200;
    let maxY = data.height || 560;
    (data.nodes || []).forEach((n) => {
      const size = measuredSize(n);
      maxX = Math.max(maxX, n.position_x + size.w + 80);
      maxY = Math.max(maxY, n.position_y + size.h + 80);
    });
    canvas.style.width = maxX + 'px';
    canvas.style.height = maxY + 'px';
    edgeSvg.setAttribute('width', String(maxX));
    edgeSvg.setAttribute('height', String(maxY));
    edgeSvg.style.width = maxX + 'px';
    edgeSvg.style.height = maxY + 'px';
    return { w: maxX, h: maxY };
  }

  function renderNodes() {
    canvas.innerHTML = '';
    (data.nodes || []).forEach((n) => {
      const el = document.createElement('div');
      el.className = 'wf-node wf-analytics-node ' + nodeClass(n.node_type) + (n.is_merge ? ' wf-analytics-node--merge' : '');
      el.style.left = n.position_x + 'px';
      el.style.top = n.position_y + 'px';
      el.dataset.key = n.node_key;
      el.style.cursor = 'default';

      const openLine =
        n.node_type === 'action_send_email'
          ? `<div class="wf-analytics-node-meta text-blue-600">${n.opens || 0} opens · ${Math.round(n.open_rate || 0)}%</div>`
          : '';
      const stopped =
        n.stopped_here > 0
          ? `<div class="wf-analytics-node-meta text-red-500">${n.stopped_here} stopped</div>`
          : '';
      const path =
        n.path_summary
          ? `<span class="wf-analytics-path-chip">${escapeHtml(n.path_summary)}</span>`
          : '';

      el.innerHTML = `
        <div class="wf-port wf-port-in" aria-hidden="true"></div>
        <div class="wf-port wf-port-left" aria-hidden="true"></div>
        <div class="wf-port wf-port-right" aria-hidden="true"></div>
        <div class="wf-analytics-node-kind">${escapeHtml(kindLabel(n.node_type))}</div>
        <p class="font-semibold text-sm text-slate-800 leading-snug">${escapeHtml(n.label)}</p>
        ${n.description ? `<p class="text-xs text-slate-500 mt-0.5 leading-snug">${escapeHtml(n.description)}</p>` : ''}
        ${path}
        <div class="wf-analytics-node-metric">
          <span class="wf-analytics-node-count">${n.contacts_here || 0}</span>
          <span class="wf-analytics-node-count-label">here now</span>
        </div>
        <div class="wf-analytics-node-meta">${n.passed_through || 0} passed through</div>
        ${openLine}
        ${stopped}
        <div class="wf-port wf-port-out" aria-hidden="true"></div>`;
      canvas.appendChild(el);
    });
  }

  function drawEdges() {
    ensureBounds();
    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    defs.innerHTML = `
      <marker id="wf-a-arrow" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#64748b"/>
      </marker>
      <marker id="wf-a-arrow-true" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#16a34a"/>
      </marker>
      <marker id="wf-a-arrow-false" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#dc2626"/>
      </marker>
      <marker id="wf-a-arrow-hot" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#dc2626"/>
      </marker>
      <marker id="wf-a-arrow-warm" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#d97706"/>
      </marker>
      <marker id="wf-a-arrow-cold" markerWidth="8" markerHeight="8" refX="8" refY="3" orient="auto" markerUnits="userSpaceOnUse">
        <path d="M0,0 L0,6 L8,3 z" fill="#2563eb"/>
      </marker>`;
    edgeSvg.innerHTML = '';
    edgeSvg.appendChild(defs);

    const edges = data.edges || [];
    edges.forEach((e) => {
      const fromNode = getNode(e.source);
      const toNode = getNode(e.target);
      if (!fromNode || !toNode) return;

      const sides = pickConnectionSides(fromNode, toNode);
      const siblings = edges.filter((x) => x.source === e.source);
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
      const style = strokeFor(e.edge_type || 'default');

      const visPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      visPath.setAttribute('d', path);
      visPath.setAttribute('fill', 'none');
      visPath.setAttribute('stroke', style.stroke);
      visPath.setAttribute('stroke-width', '2');
      visPath.setAttribute('marker-end', style.marker);
      visPath.setAttribute('class', 'wf-edge-vis');
      edgeSvg.appendChild(visPath);

      const mid = edgeMidpoint(from, to, sides.from, sides.to);
      const labelParts = [];
      if (e.label) labelParts.push(e.label);
      if (e.flow > 0) labelParts.push(e.flow + ' took path');
      if (labelParts.length) {
        const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        label.setAttribute('x', String(mid.x));
        label.setAttribute('y', String(mid.y - 6));
        label.setAttribute('text-anchor', 'middle');
        label.setAttribute('class', 'wf-analytics-edge-label');
        label.textContent = labelParts.join(' · ');
        edgeSvg.appendChild(label);
      }
    });
  }

  renderNodes();
  requestAnimationFrame(() => {
    drawEdges();
    requestAnimationFrame(drawEdges);
  });
})();
