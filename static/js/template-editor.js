(function () {
    const form = document.getElementById('template-form');
    if (!form || typeof Quill === 'undefined') return;

    const aiEnabled = form.dataset.aiEnabled === 'true';
    const subjectInput = document.getElementById('template-subject');
    const bodyHidden = document.getElementById('template-body');
    const chipsEl = document.getElementById('template-var-chips');
    const sampleFieldsEl = document.getElementById('template-sample-fields');
    const previewSubject = document.getElementById('preview-subject');
    const previewFrame = document.getElementById('preview-frame');
    const editorEl = document.getElementById('template-editor');
    const lintSidebar = document.getElementById('template-lint-sidebar');
    const subjectNudgesEl = document.getElementById('template-subject-nudges');
    const startersEl = document.getElementById('template-starters');
    const toneBanner = document.getElementById('template-tone-banner');
    const selectionPopover = document.getElementById('template-selection-popover');

    let defaultSample = {};
    const sampleScript = document.getElementById('template-default-sample');
    try {
        defaultSample = sampleScript ? JSON.parse(sampleScript.textContent || '{}') : {};
    } catch (_) {
        defaultSample = { name: 'Alex', company: 'Acme Corp' };
    }
    const sampleValues = { ...defaultSample };

    const varRe = /\{\{\s*~?\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\|[^}]*)?\s*\}\}/g;
    const ifVarRe = /\{%\s*if\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*%\}/g;

    const quill = new Quill(editorEl, {
        theme: 'snow',
        modules: {
            toolbar: [
                ['bold', 'italic'],
                [{ list: 'ordered' }, { list: 'bullet' }],
                ['link'],
                ['clean'],
            ],
            history: { delay: 500, maxStack: 100, userOnly: true },
        },
        placeholder: 'Write your email…',
    });

    const initialBody = bodyHidden.value || '';
    if (initialBody) {
        quill.clipboard.dangerouslyPasteHTML(initialBody);
    }

    let toneCheckPassed = false;
    let lastToneCheckKey = '';
    let lastSubjectForNudges = null;
    let lastFetchedSubject = '';
    let aiHintDismissed = sessionStorage.getItem('template-ai-hint-dismissed') === '1';
    let dismissedLintCodes = new Set();
    try {
        dismissedLintCodes = new Set(JSON.parse(sessionStorage.getItem('template-lint-dismissed') || '[]'));
    } catch (_) {
        dismissedLintCodes = new Set();
    }
    let currentAIHint = '';
    let selectionRange = null;
    let popoverLoading = false;

    function syncBody() {
        bodyHidden.value = quill.root.innerHTML;
    }

    function isBodyEmpty() {
        const html = quill.root.innerHTML.trim();
        return !html || html === '<p><br></p>' || html === '<p></p>';
    }

    function extractVariables() {
        const text = (subjectInput.value || '') + '\n' + quill.root.innerHTML;
        const seen = new Set();
        const keys = [];
        let m;
        const re = new RegExp(varRe.source, 'g');
        while ((m = re.exec(text)) !== null) {
            const key = m[1];
            if (!seen.has(key)) {
                seen.add(key);
                keys.push(key);
            }
        }
        const ifRe = new RegExp(ifVarRe.source, 'g');
        while ((m = ifRe.exec(text)) !== null) {
            const key = m[1];
            if (!seen.has(key)) {
                seen.add(key);
                keys.push(key);
            }
        }
        keys.sort();
        return keys;
    }

    function defaultForKey(key) {
        if (sampleValues[key] !== undefined) return sampleValues[key];
        if (defaultSample[key] !== undefined) return defaultSample[key];
        return key.charAt(0).toUpperCase() + key.slice(1).replace(/_/g, ' ');
    }

    function renderChips(keys) {
        if (!chipsEl) return;
        chipsEl.innerHTML = '';
        if (keys.length === 0) {
            chipsEl.innerHTML = '<span class="text-sm text-slate-400">Type {{name}} in subject or body — variables are detected automatically.</span>';
            return;
        }
        const hint = document.createElement('p');
        hint.className = 'text-xs text-slate-400 w-full mb-1';
        hint.textContent = 'Click to insert plain {{key}} — add filters from the syntax guide.';
        chipsEl.appendChild(hint);
        keys.forEach((key) => {
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'template-var-chip';
            btn.textContent = '{{' + key + '}}';
            btn.title = 'Insert at cursor';
            btn.addEventListener('click', () => insertVariable(key));
            chipsEl.appendChild(btn);
        });
    }

    function renderSampleFields(keys) {
        if (!sampleFieldsEl) return;
        sampleFieldsEl.innerHTML = '';
        if (keys.length === 0) {
            sampleFieldsEl.innerHTML = '<p class="text-xs text-slate-400">Sample values appear when you add variables.</p>';
            return;
        }
        keys.forEach((key) => {
            if (sampleValues[key] === undefined) {
                sampleValues[key] = defaultForKey(key);
            }
            const wrap = document.createElement('div');
            wrap.className = 'template-sample-field';
            const label = document.createElement('label');
            label.className = 'form-label text-xs';
            label.textContent = key;
            const input = document.createElement('input');
            input.type = 'text';
            input.className = 'form-input text-sm';
            input.value = sampleValues[key];
            input.dataset.varKey = key;
            input.addEventListener('input', () => {
                sampleValues[key] = input.value;
                schedulePreview();
            });
            wrap.appendChild(label);
            wrap.appendChild(input);
            sampleFieldsEl.appendChild(wrap);
        });
    }

    function insertVariable(key) {
        const token = '{{' + key + '}}';
        if (document.activeElement === subjectInput) {
            const start = subjectInput.selectionStart ?? subjectInput.value.length;
            const end = subjectInput.selectionEnd ?? start;
            const v = subjectInput.value;
            subjectInput.value = v.slice(0, start) + token + v.slice(end);
            const pos = start + token.length;
            subjectInput.setSelectionRange(pos, pos);
            subjectInput.focus();
        } else {
            const range = quill.getSelection(true);
            const index = range ? range.index : quill.getLength();
            quill.insertText(index, token);
            quill.setSelection(index + token.length);
            quill.focus();
        }
        onContentChange();
    }

    let previewTimer = null;
    function schedulePreview() {
        clearTimeout(previewTimer);
        previewTimer = setTimeout(refreshPreview, 300);
    }

    const previewUseAI = document.getElementById('preview-use-ai');

    async function refreshPreview() {
        syncBody();
        const keys = extractVariables();
        const sample = {};
        keys.forEach((k) => {
            sample[k] = sampleValues[k] !== undefined ? sampleValues[k] : defaultForKey(k);
        });
        const useAI = previewUseAI && previewUseAI.checked;
        try {
            const res = await fetch('/templates/preview', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    subject: subjectInput.value || '',
                    body: bodyHidden.value || '',
                    sample,
                    use_ai: useAI,
                }),
            });
            if (!res.ok) return;
            const data = await res.json();
            if (previewSubject) previewSubject.textContent = data.subject || '(no subject)';
            if (previewFrame) previewFrame.srcdoc = data.body_html || '';
        } catch (_) {
            /* ignore */
        }
    }

    let lintTimer = null;
    function scheduleLint() {
        clearTimeout(lintTimer);
        lintTimer = setTimeout(refreshLint, 500);
    }

    function renderLintSidebar(ruleLint, aiHint) {
        if (!lintSidebar) return;
        lintSidebar.innerHTML = '';
        const items = (Array.isArray(ruleLint) ? ruleLint : []).filter((issue) => !dismissedLintCodes.has(issue.code));
        if (aiHint && !aiHintDismissed) {
            items.push({
                level: 'info',
                code: 'ai_personalization',
                message: aiHint,
                source: 'ai',
            });
        }
        if (items.length === 0) {
            lintSidebar.innerHTML = '<p class="template-lint-empty">No tips right now.</p>';
            return;
        }
        const title = document.createElement('p');
        title.className = 'template-lint-title';
        title.textContent = 'Optional tips';
        lintSidebar.appendChild(title);
        const list = document.createElement('ul');
        list.className = 'template-lint-list';
        items.forEach((issue) => {
            const li = document.createElement('li');
            li.className = 'template-lint-item template-lint-' + (issue.level || 'info');
            const msg = document.createElement('span');
            msg.textContent = issue.message;
            li.appendChild(msg);
            const dismiss = document.createElement('button');
            dismiss.type = 'button';
            dismiss.className = 'template-lint-dismiss';
            dismiss.textContent = 'Dismiss';
            dismiss.addEventListener('click', () => {
                if (issue.source === 'ai') {
                    aiHintDismissed = true;
                    sessionStorage.setItem('template-ai-hint-dismissed', '1');
                } else if (issue.code) {
                    dismissedLintCodes.add(issue.code);
                    sessionStorage.setItem('template-lint-dismissed', JSON.stringify([...dismissedLintCodes]));
                }
                renderLintSidebar(ruleLint, issue.source === 'ai' ? '' : aiHint);
            });
            li.appendChild(dismiss);
            list.appendChild(li);
        });
        lintSidebar.appendChild(list);
    }

    async function refreshLint() {
        syncBody();
        let ruleLint = [];
        try {
            const res = await fetch('/templates/lint', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    subject: subjectInput.value || '',
                    body: bodyHidden.value || '',
                }),
            });
            if (res.ok) {
                const data = await res.json();
                ruleLint = data.lint || [];
            }
        } catch (_) {
            /* ignore */
        }

        let aiHint = currentAIHint;
        const hasVariables = extractVariables().length > 0;
        const hasRulePersonalization = ruleLint.some((i) => i.code === 'no_personalization');
        if (aiEnabled && !aiHintDismissed && !hasRulePersonalization && !hasVariables) {
            try {
                const res = await fetch('/templates/ai/personalization-hint', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        subject: subjectInput.value || '',
                        body: bodyHidden.value || '',
                        variables: extractVariables(),
                    }),
                });
                if (res.ok) {
                    const data = await res.json();
                    aiHint = data.hint || '';
                    currentAIHint = aiHint;
                }
            } catch (_) {
                /* ignore */
            }
        } else if (hasRulePersonalization) {
            aiHint = '';
            currentAIHint = '';
        }

        renderLintSidebar(ruleLint, aiHint);
    }

    function hideSelectionPopover() {
        if (selectionPopover) selectionPopover.hidden = true;
        selectionRange = null;
    }

    function showSelectionPopover(range, text) {
        if (!aiEnabled || !selectionPopover || popoverLoading) return;
        if (!text || text.length > 50) {
            hideSelectionPopover();
            return;
        }
        selectionRange = range;
        const bounds = quill.getBounds(range.index, range.length);
        const editorRect = editorEl.getBoundingClientRect();
        selectionPopover.style.top = (editorRect.top + bounds.bottom + window.scrollY + 6) + 'px';
        selectionPopover.style.left = (editorRect.left + bounds.left + window.scrollX) + 'px';
        selectionPopover.hidden = false;
    }

    async function runRewrite(action) {
        if (!selectionRange || popoverLoading) return;
        const text = quill.getText(selectionRange.index, selectionRange.length).trim();
        if (!text || text.length > 50) return;
        popoverLoading = true;
        selectionPopover.classList.add('template-ai-loading');
        try {
            const res = await fetch('/templates/ai/rewrite', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ text, action }),
            });
            if (!res.ok) return;
            const data = await res.json();
            if (!data.text) return;
            quill.deleteText(selectionRange.index, selectionRange.length);
            quill.insertText(selectionRange.index, data.text);
            quill.setSelection(selectionRange.index + data.text.length);
            onContentChange();
        } catch (_) {
            /* ignore */
        } finally {
            popoverLoading = false;
            selectionPopover.classList.remove('template-ai-loading');
            hideSelectionPopover();
        }
    }

    if (selectionPopover) {
        selectionPopover.querySelectorAll('[data-ai-action]').forEach((btn) => {
            btn.addEventListener('click', () => runRewrite(btn.dataset.aiAction));
        });
    }

    quill.on('selection-change', (range) => {
        if (!aiEnabled || !range || range.length === 0) {
            hideSelectionPopover();
            return;
        }
        const text = quill.getText(range.index, range.length).trim();
        if (text.length >= 1 && text.length <= 50) {
            showSelectionPopover(range, text);
        } else {
            hideSelectionPopover();
        }
    });

    document.addEventListener('mousedown', (e) => {
        if (selectionPopover && !selectionPopover.hidden &&
            !selectionPopover.contains(e.target) && !editorEl.contains(e.target)) {
            hideSelectionPopover();
        }
    });

    let subjectNudgeTimer = null;
    function scheduleSubjectNudges() {
        if (!aiEnabled || !subjectNudgesEl) return;
        clearTimeout(subjectNudgeTimer);
        subjectNudgeTimer = setTimeout(fetchSubjectNudges, 1500);
    }

    async function fetchSubjectNudges() {
        if (!aiEnabled || !subjectNudgesEl) return;
        const subject = (subjectInput.value || '').trim();
        if (!subject || subject === lastFetchedSubject) return;
        lastFetchedSubject = subject;
        try {
            const res = await fetch('/templates/ai/subject-alternatives', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    subject,
                    variables: extractVariables(),
                }),
            });
            if (!res.ok) {
                subjectNudgesEl.hidden = true;
                return;
            }
            const data = await res.json();
            const alts = (data.alternatives || []).filter((a) => a && a !== subject);
            if (alts.length === 0) {
                subjectNudgesEl.hidden = true;
                return;
            }
            subjectNudgesEl.innerHTML = '';
            const label = document.createElement('span');
            label.className = 'template-subject-nudges-label';
            label.textContent = 'Try:';
            subjectNudgesEl.appendChild(label);
            alts.forEach((alt) => {
                const chip = document.createElement('button');
                chip.type = 'button';
                chip.className = 'template-subject-nudge-chip';
                chip.textContent = alt;
                chip.addEventListener('click', () => {
                    subjectInput.value = alt;
                    lastSubjectForNudges = alt;
                    lastFetchedSubject = alt;
                    subjectNudgesEl.hidden = true;
                    onContentChange();
                });
                subjectNudgesEl.appendChild(chip);
            });
            subjectNudgesEl.hidden = false;
        } catch (_) {
            subjectNudgesEl.hidden = true;
        }
    }

    async function loadStarters() {
        if (!aiEnabled || !startersEl || !isBodyEmpty()) return;
        try {
            const res = await fetch('/templates/ai/starters', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
            if (!res.ok) return;
            const data = await res.json();
            const starters = data.starters || [];
            if (starters.length === 0 || !isBodyEmpty()) return;
            startersEl.innerHTML = '';
            const prompt = document.createElement('p');
            prompt.className = 'template-starters-prompt';
            prompt.textContent = 'Start from a short intro template?';
            startersEl.appendChild(prompt);
            const row = document.createElement('div');
            row.className = 'template-starters-row';
            starters.forEach((s) => {
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.className = 'template-starter-btn';
                btn.textContent = s.label;
                btn.addEventListener('click', () => {
                    quill.clipboard.dangerouslyPasteHTML(s.skeleton);
                    startersEl.hidden = true;
                    onContentChange();
                });
                row.appendChild(btn);
            });
            startersEl.appendChild(row);
            startersEl.hidden = false;
        } catch (_) {
            /* ignore */
        }
    }

    function updateStartersVisibility() {
        if (!startersEl) return;
        if (isBodyEmpty() && aiEnabled) {
            if (startersEl.childElementCount === 0) loadStarters();
            else startersEl.hidden = false;
        } else {
            startersEl.hidden = true;
        }
    }

    function hideToneBanner() {
        if (toneBanner) toneBanner.hidden = true;
    }

    function showToneBanner(message) {
        if (!toneBanner) return;
        const textEl = toneBanner.querySelector('.template-tone-banner-text');
        if (textEl) textEl.textContent = message || 'Reads formal — intentional?';
        toneBanner.hidden = false;
    }

    async function runToneCheck() {
        syncBody();
        const key = (subjectInput.value || '') + '\n' + (bodyHidden.value || '');
        if (key === lastToneCheckKey && toneCheckPassed) return true;
        lastToneCheckKey = key;

        if (!aiEnabled) {
            toneCheckPassed = true;
            return true;
        }

        const plain = quill.getText().trim();
        if (plain.length < 20) {
            toneCheckPassed = true;
            return true;
        }

        try {
            const res = await fetch('/templates/ai/tone-check', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    subject: subjectInput.value || '',
                    body: bodyHidden.value || '',
                }),
            });
            if (!res.ok) {
                toneCheckPassed = true;
                return true;
            }
            const data = await res.json();
            if (data.tone === 'formal' && data.message) {
                showToneBanner(data.message);
                return false;
            }
            if (data.tone === 'formal') {
                showToneBanner('Reads formal — intentional?');
                return false;
            }
            toneCheckPassed = true;
            return true;
        } catch (_) {
            toneCheckPassed = true;
            return true;
        }
    }

    if (toneBanner) {
        toneBanner.querySelector('[data-tone-action="proceed"]')?.addEventListener('click', () => {
            toneCheckPassed = true;
            hideToneBanner();
            form.requestSubmit();
        });
        toneBanner.querySelector('[data-tone-action="soften"]')?.addEventListener('click', async () => {
            syncBody();
            try {
                const res = await fetch('/templates/ai/soften-body', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ body: bodyHidden.value || '' }),
                });
                if (res.ok) {
                    const data = await res.json();
                    if (data.body) {
                        quill.clipboard.dangerouslyPasteHTML(data.body);
                        onContentChange();
                    }
                }
            } catch (_) {
                /* ignore */
            }
            hideToneBanner();
            toneCheckPassed = false;
        });
    }

    function onContentChange() {
        syncBody();
        toneCheckPassed = false;
        const keys = extractVariables();
        renderChips(keys);
        renderSampleFields(keys);
        schedulePreview();
        scheduleLint();
        updateStartersVisibility();
        if (aiEnabled && subjectInput.value !== lastSubjectForNudges) {
            scheduleSubjectNudges();
        }
    }

    quill.on('text-change', onContentChange);
    subjectInput.addEventListener('input', () => {
        if (subjectInput.value !== lastSubjectForNudges) {
            lastFetchedSubject = '';
        }
        onContentChange();
    });
    if (previewUseAI) {
        previewUseAI.addEventListener('change', schedulePreview);
    }

    document.querySelectorAll('.syntax-copy').forEach((btn) => {
        btn.addEventListener('click', async () => {
            const text = btn.dataset.copy || '';
            if (!text) return;
            try {
                await navigator.clipboard.writeText(text);
                const prev = btn.textContent;
                btn.textContent = 'Copied';
                setTimeout(() => { btn.textContent = prev; }, 1200);
            } catch (_) {
                /* ignore */
            }
        });
    });

    form.addEventListener('submit', async (e) => {
        syncBody();
        if (!toneCheckPassed) {
            e.preventDefault();
            const ok = await runToneCheck();
            if (ok && toneCheckPassed) {
                form.requestSubmit();
            }
        }
    });

    onContentChange();
    if (aiEnabled) loadStarters();
})();
