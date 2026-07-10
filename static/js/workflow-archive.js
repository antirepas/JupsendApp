(function () {
    const modal = document.getElementById('workflow-archive-modal');
    if (!modal) return;

    const form = document.getElementById('workflow-archive-form');
    const statsEl = document.getElementById('workflow-archive-stats');
    const titleEl = document.getElementById('workflow-archive-title');
    let workflowID = null;

    function hideModal() {
        modal.classList.add('hidden');
        workflowID = null;
        if (form) {
            form.action = '#';
            const cb = form.querySelector('input[name="cancel_queued"]');
            if (cb) cb.checked = false;
        }
    }

    function showModal() {
        modal.classList.remove('hidden');
    }

    function renderStats(data) {
        if (!statsEl) return;
        const parts = [];
        parts.push('<strong>' + (data.waiting_instances || 0) + '</strong> contacts still in workflow');
        parts.push('<strong>' + (data.queued_send_jobs || 0) + '</strong> emails queued to send');
        parts.push('<strong>' + (data.completed_instances || 0) + '</strong> contacts completed');
        statsEl.innerHTML = parts.map((p) => '<p>' + p + '</p>').join('');
    }

    async function openArchiveModal(id, name) {
        workflowID = id;
        if (titleEl) {
            titleEl.textContent = 'Archive "' + (name || 'workflow') + '"?';
        }
        if (form) {
            form.action = '/workflows/' + id + '/archive';
        }
        if (statsEl) {
            statsEl.innerHTML = '<p class="text-slate-400">Loading…</p>';
        }
        showModal();
        try {
            const res = await fetch('/workflows/' + id + '/archive-preview', { credentials: 'same-origin' });
            if (!res.ok) {
                statsEl.innerHTML = '<p class="text-red-600">Could not load workflow stats.</p>';
                return;
            }
            const data = await res.json();
            renderStats(data);
        } catch (_) {
            statsEl.innerHTML = '<p class="text-red-600">Could not load workflow stats.</p>';
        }
    }

    document.querySelectorAll('[data-archive-workflow]').forEach((btn) => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            const id = btn.dataset.workflowId;
            const name = btn.dataset.workflowName || '';
            if (id) openArchiveModal(id, name);
        });
    });

    modal.querySelectorAll('[data-archive-dismiss]').forEach((el) => {
        el.addEventListener('click', hideModal);
    });

    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && !modal.classList.contains('hidden')) {
            hideModal();
        }
    });
})();
