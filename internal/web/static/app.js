(function() {
    const LIMIT = 50;
    const AUTO_LOAD_THRESHOLD = 128 * 1024;
    const ELLIPSIS_LIMIT = 120;
    const DETAIL_SCROLL_STEP = 56;

    let offset = 0;
    let autoRefreshTimer = null;
    let currentLogs = [];
    let selectedLogId = null;
    let detailState = null;
    let detailStatusTimer = null;
    let recordingState = true;
    let recordingBusy = false;

    const tbody = document.getElementById('log-body');
    const pageInfo = document.getElementById('page-info');
    const prevBtn = document.getElementById('prev-btn');
    const nextBtn = document.getElementById('next-btn');
    const queryFilter = document.getElementById('query-filter');
    const refreshBtn = document.getElementById('refresh-btn');
    const autoRefresh = document.getElementById('auto-refresh');
    const recordingToggleBtn = document.getElementById('recording-toggle-btn');
    const detailOverlay = document.getElementById('detail-overlay');
    const detailPanel = detailOverlay.querySelector('.detail-panel');
    const detailContent = document.getElementById('detail-content');
    const closeDetail = document.getElementById('close-detail');
    const helpOverlay = document.getElementById('help-overlay');
    const closeHelp = document.getElementById('close-help');

    function buildURL() {
        let url = `/api/logs?limit=${LIMIT}&offset=${offset}`;
        const q = queryFilter.value.trim();
        if (q) {
            url += `&query=${encodeURIComponent(q)}`;
        }
        return url;
    }

    function formatTime(ms) {
        if (!ms) {
            return '-';
        }
        return new Date(ms).toLocaleString();
    }

    function formatBytes(b) {
        if (b === null || b === undefined) {
            return '-';
        }
        if (b < 1024) {
            return `${b} B`;
        }
        if (b < 1048576) {
            return `${(b / 1024).toFixed(1)} KB`;
        }
        return `${(b / 1048576).toFixed(1)} MB`;
    }

    function escapeHTML(value) {
        const div = document.createElement('div');
        div.textContent = value;
        return div.innerHTML;
    }

    function formatHeadersDisplay(headers) {
        if (!headers || Object.keys(headers).length === 0) {
            return '-';
        }
        return JSON.stringify(headers, null, 2);
    }

    function formatHeadersCompact(headers) {
        return JSON.stringify(headers || {});
    }

    function tryPrettyPrintJSON(content) {
        if (!content) {
            return { content: '', pretty: false };
        }
        const trimmed = content.trim();
        if (!trimmed || (trimmed[0] !== '{' && trimmed[0] !== '[')) {
            return { content, pretty: false };
        }
        try {
            return {
                content: JSON.stringify(JSON.parse(trimmed), null, 2),
                pretty: true
            };
        } catch (e) {
            return { content, pretty: false };
        }
    }

    function bodyCacheKey(part, mode, full) {
        return `${part}:${mode}:${full ? 'full' : 'preview'}`;
    }

    function isHelpOpen() {
        return !helpOverlay.classList.contains('hidden');
    }

    function isDetailOpen() {
        return !detailOverlay.classList.contains('hidden');
    }

    function isEditableTarget(target) {
        if (!target) {
            return false;
        }
        const tag = (target.tagName || '').toLowerCase();
        return target.isContentEditable || tag === 'input' || tag === 'textarea' || tag === 'select';
    }

    function isActiveDetailState(state) {
        return detailState === state && isDetailOpen();
    }

    function focusQueryFilter() {
        queryFilter.focus();
        queryFilter.select();
    }

    function getSelectedRowIndex() {
        return currentLogs.findIndex(entry => entry.id === selectedLogId);
    }

    function renderSelection() {
        tbody.querySelectorAll('tr[data-log-id]').forEach(tr => {
            tr.classList.toggle('selected', Number(tr.dataset.logId) === selectedLogId);
        });
    }

    function selectLogByIndex(index, options = {}) {
        const scroll = options.scroll !== false;
        if (!currentLogs.length) {
            selectedLogId = null;
            return;
        }
        const clamped = Math.max(0, Math.min(index, currentLogs.length - 1));
        selectedLogId = currentLogs[clamped].id;
        renderSelection();
        if (scroll) {
            const row = tbody.querySelector(`tr[data-log-id="${selectedLogId}"]`);
            if (row) {
                row.scrollIntoView({ block: 'nearest' });
            }
        }
    }

    function selectLogByDelta(delta) {
        if (!currentLogs.length) {
            return;
        }
        const currentIndex = getSelectedRowIndex();
        if (currentIndex === -1) {
            selectLogByIndex(delta >= 0 ? 0 : currentLogs.length - 1);
            return;
        }
        selectLogByIndex(currentIndex + delta);
    }

    function closeHelpOverlay() {
        helpOverlay.classList.add('hidden');
    }

    function showHelpOverlay() {
        helpOverlay.classList.remove('hidden');
    }

    function clearDetailStatus() {
        clearTimeout(detailStatusTimer);
        detailStatusTimer = null;
        const status = detailContent.querySelector('#detail-shortcut-status');
        if (status) {
            status.textContent = '';
            status.classList.add('hidden');
        }
    }

    function showDetailStatus(message, timeoutMs = 2000) {
        const status = detailContent.querySelector('#detail-shortcut-status');
        if (!status) {
            return;
        }
        clearTimeout(detailStatusTimer);
        status.textContent = message;
        status.classList.remove('hidden');
        if (timeoutMs > 0) {
            detailStatusTimer = setTimeout(() => {
                if (status.isConnected) {
                    status.textContent = '';
                    status.classList.add('hidden');
                }
            }, timeoutMs);
        }
    }

    function closeDetailOverlay() {
        if (detailState) {
            detailState.copyPrefixPending = false;
        }
        detailState = null;
        clearDetailStatus();
        detailOverlay.classList.add('hidden');
    }

    function renderRecordingState() {
        recordingToggleBtn.disabled = recordingBusy;
        recordingToggleBtn.classList.toggle('recording-on', recordingState);
        recordingToggleBtn.classList.toggle('recording-off', !recordingState);
        recordingToggleBtn.textContent = recordingState ? 'Recording: ON' : 'Recording: PAUSED';
    }

    async function fetchRecordingState() {
        try {
            const resp = await fetch('/health');
            const json = await resp.json();
            recordingState = !!json.recording;
            renderRecordingState();
        } catch (e) {
            recordingToggleBtn.textContent = 'Recording: unavailable';
        }
    }

    async function toggleRecordingState() {
        recordingBusy = true;
        renderRecordingState();
        try {
            const resp = await fetch('/api/recording', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ recording: !recordingState })
            });
            if (!resp.ok) {
                throw new Error(`HTTP ${resp.status}`);
            }
            const json = await resp.json();
            recordingState = !!json.recording;
            renderRecordingState();
        } catch (e) {
            alert(`Failed to toggle recording: ${e.message}`);
        } finally {
            recordingBusy = false;
            renderRecordingState();
        }
    }

    async function loadLogs() {
        try {
            const resp = await fetch(buildURL());
            const json = await resp.json();
            renderTable(json.data || [], json.total || 0);
        } catch (e) {
            currentLogs = [];
            selectedLogId = null;
            tbody.innerHTML = `<tr><td colspan="8">Error: ${escapeHTML(e.message)}</td></tr>`;
        }
    }

    function renderTable(data, total) {
        currentLogs = data;
        tbody.innerHTML = '';

        if (!data.length) {
            selectedLogId = null;
            tbody.innerHTML = '<tr><td colspan="8">No logs found</td></tr>';
        } else {
            if (!data.some(entry => entry.id === selectedLogId)) {
                selectedLogId = data[0].id;
            }
            data.forEach(entry => {
                const tr = document.createElement('tr');
                tr.dataset.logId = entry.id;
                tr.innerHTML = `
                    <td>${entry.id}</td>
                    <td>${escapeHTML(formatTime(entry.ts_start))}</td>
                    <td>${escapeHTML(entry.request_method || '-')}</td>
                    <td>${escapeHTML(entry.request_path || '-')}</td>
                    <td class="status-${Math.floor((entry.status_code || 0) / 100)}">${entry.status_code || '-'}</td>
                    <td>${entry.duration_ms != null ? `${entry.duration_ms}ms` : '-'}</td>
                    <td>${formatBytes(entry.req_bytes)}${entry.req_truncated ? ' !' : ''}</td>
                    <td>${formatBytes(entry.resp_bytes)}${entry.resp_truncated ? ' !' : ''}</td>
                `;
                tr.style.cursor = 'pointer';
                tr.classList.toggle('selected', entry.id === selectedLogId);
                tr.addEventListener('click', () => {
                    selectedLogId = entry.id;
                    renderSelection();
                    void showDetail(entry.id);
                });
                tbody.appendChild(tr);
            });
        }

        const page = Math.floor(offset / LIMIT) + 1;
        const pages = Math.ceil(total / LIMIT) || 1;
        pageInfo.textContent = `Page ${page} of ${pages} (${total} total)`;
        prevBtn.disabled = offset === 0;
        nextBtn.disabled = offset + LIMIT >= total;
    }

    function createButton(label, className, handler) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.textContent = label;
        btn.className = className;
        btn.addEventListener('click', handler);
        return btn;
    }

    function withBusyButton(button, busyText, action) {
        const defaultText = button.textContent;
        button.disabled = true;
        button.textContent = busyText;
        return Promise.resolve()
            .then(action)
            .finally(() => {
                if (!button.isConnected) {
                    return;
                }
                if (button.textContent === busyText) {
                    button.textContent = defaultText;
                    button.disabled = false;
                }
            });
    }

    function flashButton(button, label) {
        if (!button || !button.isConnected) {
            return;
        }
        button.textContent = label;
        button.disabled = true;
        window.setTimeout(() => {
            if (!button.isConnected) {
                return;
            }
            button.textContent = 'Copy';
            button.disabled = false;
        }, 1200);
    }

    async function copyToClipboard(text, label, button) {
        try {
            if (navigator.clipboard && navigator.clipboard.writeText) {
                await navigator.clipboard.writeText(text);
            } else {
                const ta = document.createElement('textarea');
                ta.value = text;
                ta.setAttribute('readonly', 'readonly');
                ta.style.position = 'absolute';
                ta.style.left = '-9999px';
                document.body.appendChild(ta);
                ta.select();
                const success = document.execCommand('copy');
                document.body.removeChild(ta);
                if (!success) {
                    throw new Error('clipboard copy failed');
                }
            }
            flashButton(button, 'Copied');
            showDetailStatus(`Copied ${label}`, 1500);
        } catch (e) {
            showDetailStatus(`Copy failed: ${e.message}`, 2500);
        }
    }

    function resolveBodyMode(state, part) {
        return part === 'resp' && state.currentMode === 'assembled' ? 'assembled' : 'raw';
    }

    function getCachedBody(state, part, mode) {
        return state.bodyCache.get(bodyCacheKey(part, mode, true)) || state.bodyCache.get(bodyCacheKey(part, mode, false)) || null;
    }

    async function fetchBodyForState(state, part, mode, options = {}) {
        const full = !!options.full;
        const cacheKey = bodyCacheKey(part, mode, full);
        if (state.bodyCache.has(cacheKey)) {
            return state.bodyCache.get(cacheKey);
        }

        let url = `/api/logs/${state.id}/body?part=${part}&mode=${mode}`;
        if (full) {
            url += '&full=true';
        } else {
            url += `&ellipsis=${ELLIPSIS_LIMIT}`;
        }

        const resp = await fetch(url);
        if (!resp.ok) {
            const err = await resp.json().catch(() => null);
            throw new Error(err && err.message ? err.message : `HTTP ${resp.status}`);
        }

        const bodyData = await resp.json();
        if (isActiveDetailState(state)) {
            state.bodyCache.set(cacheKey, bodyData);
            if (full) {
                state.bodyCache.set(bodyCacheKey(part, mode, false), bodyData);
            }
        }
        return bodyData;
    }

    function renderHeaderActions(part) {
        const state = detailState;
        if (!state || !state.entry) {
            return;
        }
        const actions = document.getElementById(`${part}-headers-actions`);
        if (!actions) {
            return;
        }
        actions.innerHTML = '';
        const copyBtn = createButton('Copy', 'copy-btn', event => {
            const headers = part === 'req' ? state.entry.req_headers : state.entry.resp_headers;
            void withBusyButton(event.currentTarget, 'Copying...', async () => {
                await copyToClipboard(formatHeadersCompact(headers), `${part === 'req' ? 'request' : 'response'} headers`, event.currentTarget);
            });
        });
        copyBtn.title = `Copy ${part === 'req' ? 'request' : 'response'} headers as compact JSON`;
        actions.appendChild(copyBtn);
    }

    function bodyButtonLabel(part, mode) {
        if (part === 'req') {
            return 'request body';
        }
        return mode === 'assembled' ? 'assembled response body' : 'response body';
    }

    async function copyBody(part, mode, button) {
        const state = detailState;
        if (!state) {
            return;
        }
        await withBusyButton(button, 'Copying...', async () => {
            const bodyData = await fetchBodyForState(state, part, mode, { full: true });
            if (!isActiveDetailState(state)) {
                return;
            }
            if (!bodyData.available) {
                showDetailStatus(`Body unavailable: ${bodyData.reason || 'not available'}`, 2500);
                return;
            }
            await copyToClipboard(bodyData.content || '', bodyButtonLabel(part, mode), button);
        });
    }

    async function openBodyInspector(part, button) {
        const state = detailState;
        if (!state) {
            return;
        }
        const mode = resolveBodyMode(state, part);
        await withBusyButton(button, 'Opening...', async () => {
            const bodyData = await fetchBodyForState(state, part, mode, { full: true });
            if (!isActiveDetailState(state)) {
                return;
            }
            if (!bodyData.available) {
                showDetailStatus(`Body unavailable: ${bodyData.reason || 'not available'}`, 2500);
                return;
            }

            const rendered = tryPrettyPrintJSON(bodyData.content || '');
            const win = window.open('', '_blank', 'noopener');
            if (!win) {
                showDetailStatus('Inspector popup blocked by the browser', 2500);
                return;
            }

            const title = `Log #${state.id} ${part === 'req' ? 'Request' : 'Response'} Body (${mode})`;
            win.document.write(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>${escapeHTML(title)}</title>
    <style>
        body { margin: 0; padding: 1rem; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; background: #111; color: #eee; }
        h1 { font-size: 1rem; margin: 0 0 1rem; font-family: system-ui, sans-serif; }
        pre { margin: 0; white-space: pre-wrap; word-break: break-word; }
    </style>
</head>
<body>
    <h1>${escapeHTML(title)}</h1>
    <pre>${escapeHTML(rendered.content)}</pre>
</body>
</html>`);
            win.document.close();
        });
    }

    function renderBodyActions(part, mode, hasBody) {
        const actions = document.getElementById(`${part}-body-actions`);
        if (!actions) {
            return;
        }
        actions.innerHTML = '';
        if (!hasBody) {
            return;
        }

        const copyBtn = createButton('Copy', 'copy-btn', event => {
            void copyBody(part, mode, event.currentTarget);
        });
        copyBtn.title = `Copy the canonical ${bodyButtonLabel(part, mode)}`;
        actions.appendChild(copyBtn);

        const openBtn = createButton('Open', 'detail-action-btn', event => {
            void openBodyInspector(part, event.currentTarget);
        });
        openBtn.title = `Open the canonical ${bodyButtonLabel(part, mode)} in a new window`;
        actions.appendChild(openBtn);
    }

    function renderBodyContent(container, bodyData, part, mode) {
        container.innerHTML = '';
        if (!bodyData.available) {
            container.innerHTML = `<em>${escapeHTML(bodyData.reason || 'Not available')}</em>`;
            return;
        }

        const info = document.createElement('div');
        info.className = 'body-info';
        let infoText = `${formatBytes(bodyData.included_bytes)} / ${formatBytes(bodyData.total_bytes)} total`;
        if (bodyData.truncated) {
            infoText += ' (preview)';
        } else if (bodyData.ellipsized) {
            infoText += ' (long strings shortened)';
        } else if (bodyData.complete) {
            infoText += ' (full)';
        }
        info.textContent = infoText;
        container.appendChild(info);

        const pre = document.createElement('pre');
        const rendered = tryPrettyPrintJSON(bodyData.content || '');
        pre.textContent = rendered.content;
        if (rendered.pretty) {
            pre.classList.add('json-pretty');
        }
        container.appendChild(pre);

        if (!bodyData.complete) {
            const fullBtn = createButton('Load Full', 'load-body-btn', event => {
                const button = event.currentTarget;
                void withBusyButton(button, 'Loading...', async () => {
                    const state = detailState;
                    if (!state) {
                        return;
                    }
                    await fetchBodyForState(state, part, mode, { full: true });
                    if (isActiveDetailState(state)) {
                        renderBodySection(part);
                    }
                }).catch(e => {
                    if (button.isConnected) {
                        button.textContent = `Error: ${e.message}`;
                    }
                });
            });
            fullBtn.title = `Load the full canonical ${bodyButtonLabel(part, mode)}`;
            container.appendChild(fullBtn);
        }
    }

    async function loadPreviewBody(part, mode) {
        const state = detailState;
        if (!state) {
            return;
        }
        const container = document.getElementById(`${part}-body-container`);
        if (!container) {
            return;
        }
        container.innerHTML = '<em>Loading...</em>';
        try {
            await fetchBodyForState(state, part, mode, { full: false });
            if (isActiveDetailState(state)) {
                renderBodySection(part);
            }
        } catch (e) {
            if (isActiveDetailState(state)) {
                container.innerHTML = `<em>Error: ${escapeHTML(e.message)}</em>`;
            }
        }
    }

    function renderBodySection(part) {
        const state = detailState;
        if (!state || !state.entry) {
            return;
        }
        const container = document.getElementById(`${part}-body-container`);
        if (!container) {
            return;
        }

        const mode = resolveBodyMode(state, part);
        const hasBody = part === 'req' ? state.entry.has_request_body : state.entry.has_response_body;
        renderBodyActions(part, mode, hasBody);

        if (!hasBody) {
            container.innerHTML = '<em>No body</em>';
            return;
        }

        const cached = getCachedBody(state, part, mode);
        if (cached) {
            renderBodyContent(container, cached, part, mode);
            return;
        }

        const bodyBytes = part === 'req' ? state.entry.req_bytes : state.entry.resp_bytes;
        if (bodyBytes > AUTO_LOAD_THRESHOLD) {
            container.innerHTML = '';
            const warning = document.createElement('div');
            warning.className = 'body-size-warning';
            warning.textContent = `Body is ${formatBytes(bodyBytes)}. Click to load a preview.`;
            container.appendChild(warning);

            const loadBtn = createButton(part === 'req' ? 'Load Request Body' : 'Load Response Body', 'load-body-btn', () => {
                void loadPreviewBody(part, mode);
            });
            container.appendChild(loadBtn);
            return;
        }

        void loadPreviewBody(part, mode);
    }

    function updateStreamToggleButtons() {
        if (!detailState) {
            return;
        }
        detailContent.querySelectorAll('.sv-toggle-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.mode === detailState.currentMode);
        });
    }

    function renderConversation(thread) {
        const container = document.getElementById('conversation-container');
        if (!container) {
            return;
        }
        container.classList.remove('hidden');
        container.innerHTML = '';

        const header = document.createElement('h3');
        header.textContent = `Conversation (turn ${thread.selected_entry_index + 1} of ${thread.total_entries})`;
        container.appendChild(header);

        (thread.messages || []).forEach(msg => {
            const block = document.createElement('div');
            block.className = `conv-message conv-${msg.role}`;

            const roleEl = document.createElement('div');
            roleEl.className = 'conv-role';
            roleEl.textContent = `${msg.role} (`;

            const logLink = document.createElement('a');
            logLink.href = '#';
            logLink.textContent = `Log #${msg.log_id}`;
            logLink.onclick = event => {
                event.preventDefault();
                selectedLogId = msg.log_id;
                renderSelection();
                void showDetail(msg.log_id);
            };

            roleEl.appendChild(logLink);
            roleEl.appendChild(document.createTextNode(')'));
            block.appendChild(roleEl);

            const contentEl = document.createElement('pre');
            contentEl.className = 'conv-content';
            contentEl.textContent = msg.content;
            block.appendChild(contentEl);

            container.appendChild(block);
        });
    }

    function bindDetailActions() {
        const state = detailState;
        if (!state || !state.entry) {
            return;
        }

        renderHeaderActions('req');
        renderHeaderActions('resp');

        detailContent.querySelectorAll('.sv-toggle-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                if (btn.disabled || !detailState) {
                    return;
                }
                detailState.currentMode = btn.dataset.mode;
                updateStreamToggleButtons();
                renderBodySection('resp');
            });
        });

        const convBtn = document.getElementById('view-conversation-btn');
        if (convBtn) {
            convBtn.addEventListener('click', () => {
                void withBusyButton(convBtn, 'Loading...', async () => {
                    const activeState = detailState;
                    if (!activeState) {
                        return;
                    }
                    if (activeState.conversation) {
                        renderConversation(activeState.conversation);
                        showDetailStatus('Conversation already loaded', 1000);
                        return;
                    }
                    const resp = await fetch(`/api/logs/${activeState.id}/thread`);
                    if (!resp.ok) {
                        throw new Error(`HTTP ${resp.status}`);
                    }
                    const thread = await resp.json();
                    if (!isActiveDetailState(activeState)) {
                        return;
                    }
                    activeState.conversation = thread;
                    renderConversation(thread);
                }).catch(e => {
                    if (convBtn.isConnected) {
                        convBtn.textContent = `Error: ${e.message}`;
                    }
                });
            });
        }
    }

    function renderDetail() {
        const state = detailState;
        if (!state || !state.entry) {
            return;
        }

        const entry = state.entry;
        const sv = state.sv || {};
        const canViewConversation = entry.request_path.endsWith('/chat/completions') && !entry.req_truncated;

        let svToggleHTML = '';
        if (sv.assembled_available) {
            svToggleHTML = `
                <div class="sv-toggle">
                    <button class="sv-toggle-btn active" data-mode="raw">Raw</button>
                    <button class="sv-toggle-btn" data-mode="assembled">Assembled</button>
                    <span id="sv-message" class="sv-message hidden"></span>
                </div>
            `;
        } else if (sv.reason && sv.reason !== 'unsupported_path' && sv.reason !== 'missing_body') {
            svToggleHTML = `
                <div class="sv-toggle">
                    <button class="sv-toggle-btn active" data-mode="raw">Raw</button>
                    <button class="sv-toggle-btn" data-mode="assembled" disabled>Assembled</button>
                    <span id="sv-message" class="sv-message">${escapeHTML(sv.reason)}</span>
                </div>
            `;
        }

        detailContent.innerHTML = `
            <div class="detail-header">
                <div>
                    <h2>Log #${entry.id}</h2>
                    <div id="detail-shortcut-status" class="shortcut-status hidden"></div>
                </div>
                <div class="detail-header-actions">
                    ${canViewConversation ? '<button id="view-conversation-btn" class="detail-action-btn">View Conversation</button>' : ''}
                </div>
            </div>
            <dl>
                <dt>Time</dt><dd>${escapeHTML(formatTime(entry.ts_start))} -> ${escapeHTML(formatTime(entry.ts_end))}</dd>
                <dt>Duration</dt><dd>${entry.duration_ms != null ? `${entry.duration_ms}ms` : '-'}</dd>
                <dt>Client IP</dt><dd>${escapeHTML(entry.client_ip || '-')}</dd>
                <dt>Method</dt><dd>${escapeHTML(entry.request_method || '-')}</dd>
                <dt>Path</dt><dd>${escapeHTML(entry.request_path || '-')}</dd>
                <dt>Upstream</dt><dd>${escapeHTML(entry.upstream_url || '-')}</dd>
                <dt>Status</dt><dd>${entry.status_code || '-'}</dd>
                <dt>Error</dt><dd>${escapeHTML(entry.error || '-')}</dd>
            </dl>

            <div class="section-heading">
                <h3>Request Headers</h3>
                <div id="req-headers-actions" class="section-actions"></div>
            </div>
            <pre id="req-headers-pre"></pre>

            <div class="section-heading">
                <h3>Request Body (${formatBytes(entry.req_bytes)}${entry.req_truncated ? ' TRUNCATED' : ''})</h3>
                <div id="req-body-actions" class="section-actions"></div>
            </div>
            <div id="req-body-container"></div>

            <div class="section-heading">
                <h3>Response Headers</h3>
                <div id="resp-headers-actions" class="section-actions"></div>
            </div>
            <pre id="resp-headers-pre"></pre>

            <div class="section-heading">
                <h3>Response Body (${formatBytes(entry.resp_bytes)}${entry.resp_truncated ? ' TRUNCATED' : ''})</h3>
                <div id="resp-body-actions" class="section-actions"></div>
            </div>
            ${svToggleHTML}
            <div id="resp-body-container"></div>
            <div id="conversation-container" class="hidden"></div>
        `;

        document.getElementById('req-headers-pre').textContent = formatHeadersDisplay(entry.req_headers);
        document.getElementById('resp-headers-pre').textContent = formatHeadersDisplay(entry.resp_headers);

        bindDetailActions();
        updateStreamToggleButtons();
        renderBodySection('req');
        renderBodySection('resp');
    }

    async function showDetail(id) {
        const state = {
            id,
            currentMode: 'raw',
            bodyCache: new Map(),
            copyPrefixPending: false,
            conversation: null,
            entry: null,
            sv: null
        };
        detailState = state;
        detailContent.innerHTML = '<p>Loading detail...</p>';
        detailOverlay.classList.remove('hidden');

        try {
            const resp = await fetch(`/api/logs/${id}`);
            if (!resp.ok) {
                throw new Error(`HTTP ${resp.status}`);
            }
            const data = await resp.json();
            if (detailState !== state) {
                return;
            }
            state.entry = data.entry;
            state.sv = data.stream_view || {};
            renderDetail();
        } catch (e) {
            if (detailState === state) {
                detailContent.innerHTML = `<p>Error: ${escapeHTML(e.message)}</p>`;
            }
        }
    }

    function scrollDetail(delta) {
        detailPanel.scrollBy({ top: delta, left: 0, behavior: 'auto' });
    }

    async function loadFullVisibleBodies() {
        const state = detailState;
        if (!state || !state.entry) {
            return;
        }
        const tasks = [];
        if (state.entry.has_request_body) {
            tasks.push(fetchBodyForState(state, 'req', 'raw', { full: true }));
        }
        if (state.entry.has_response_body) {
            tasks.push(fetchBodyForState(state, 'resp', resolveBodyMode(state, 'resp'), { full: true }));
        }
        if (!tasks.length) {
            showDetailStatus('No bodies available', 1500);
            return;
        }
        showDetailStatus('Loading full bodies...', 0);
        try {
            await Promise.all(tasks);
            if (isActiveDetailState(state)) {
                clearDetailStatus();
                renderBodySection('req');
                renderBodySection('resp');
            }
        } catch (e) {
            if (isActiveDetailState(state)) {
                showDetailStatus(`Failed to load full bodies: ${e.message}`, 2500);
            }
        }
    }

    function triggerConversationView() {
        const button = document.getElementById('view-conversation-btn');
        if (!button) {
            showDetailStatus('Conversation view is not available for this entry', 2500);
            return;
        }
        button.click();
    }

    function handleCopyPrefixKey(key) {
        const state = detailState;
        if (!state) {
            return false;
        }

        state.copyPrefixPending = false;
        switch (key) {
            case 'h': {
                const button = document.getElementById('req-headers-actions')?.querySelector('.copy-btn');
                if (button) {
                    button.click();
                    return true;
                }
                break;
            }
            case 'b': {
                const button = document.getElementById('req-body-actions')?.querySelector('.copy-btn');
                if (button) {
                    button.click();
                    return true;
                }
                break;
            }
            case 'H': {
                const button = document.getElementById('resp-headers-actions')?.querySelector('.copy-btn');
                if (button) {
                    button.click();
                    return true;
                }
                break;
            }
            case 'B': {
                const button = document.getElementById('resp-body-actions')?.querySelector('.copy-btn');
                if (button) {
                    button.click();
                    return true;
                }
                break;
            }
        }

        clearDetailStatus();
        return false;
    }

    function handleDetailKeydown(event) {
        const state = detailState;
        if (!state || !state.entry) {
            return false;
        }
        if (isEditableTarget(event.target)) {
            return false;
        }

        if (state.copyPrefixPending) {
            const handledPrefix = handleCopyPrefixKey(event.key);
            if (handledPrefix || ['h', 'b', 'H', 'B'].includes(event.key)) {
                event.preventDefault();
                return true;
            }
        }

        switch (event.key) {
            case 'Escape':
            case 'u':
            case 'q':
                closeDetailOverlay();
                event.preventDefault();
                return true;
            case 'n':
                scrollDetail(DETAIL_SCROLL_STEP);
                event.preventDefault();
                return true;
            case 'p':
                scrollDetail(-DETAIL_SCROLL_STEP);
                event.preventDefault();
                return true;
            case 'v':
                if (state.sv && state.sv.assembled_available) {
                    state.currentMode = state.currentMode === 'raw' ? 'assembled' : 'raw';
                    updateStreamToggleButtons();
                    renderBodySection('resp');
                } else {
                    showDetailStatus('Assembled response view is not available', 2000);
                }
                event.preventDefault();
                return true;
            case 'c':
                triggerConversationView();
                event.preventDefault();
                return true;
            case 't':
                void loadFullVisibleBodies();
                event.preventDefault();
                return true;
            case 'j': {
                const button = document.getElementById('req-body-actions')?.querySelector('.detail-action-btn');
                if (button) {
                    void openBodyInspector('req', button);
                    event.preventDefault();
                    return true;
                }
                break;
            }
            case 'J': {
                const button = document.getElementById('resp-body-actions')?.querySelector('.detail-action-btn');
                if (button) {
                    void openBodyInspector('resp', button);
                    event.preventDefault();
                    return true;
                }
                break;
            }
            case 'w':
                state.copyPrefixPending = true;
                showDetailStatus('Copy: h=request headers, b=request body, H=response headers, B=response body', 2500);
                event.preventDefault();
                return true;
        }

        return false;
    }

    function handleGlobalKeydown(event) {
        if (isHelpOpen()) {
            if (event.key === 'Escape' || event.key === 'u' || event.key === 'h') {
                closeHelpOverlay();
                event.preventDefault();
            }
            return;
        }

        if (isDetailOpen()) {
            handleDetailKeydown(event);
            return;
        }

        if (isEditableTarget(event.target)) {
            if (event.target === queryFilter && event.key === '/') {
                focusQueryFilter();
                event.preventDefault();
            }
            return;
        }

        switch (event.key) {
            case 'j':
                selectLogByDelta(1);
                event.preventDefault();
                break;
            case 'k':
                selectLogByDelta(-1);
                event.preventDefault();
                break;
            case 'Enter':
                if (selectedLogId !== null) {
                    void showDetail(selectedLogId);
                }
                event.preventDefault();
                break;
            case '/':
                focusQueryFilter();
                event.preventDefault();
                break;
            case 'R':
                if (event.shiftKey) {
                    void toggleRecordingState();
                    event.preventDefault();
                }
                break;
            case 'h':
                showHelpOverlay();
                event.preventDefault();
                break;
        }
    }

    prevBtn.addEventListener('click', () => {
        offset = Math.max(0, offset - LIMIT);
        void loadLogs();
    });

    nextBtn.addEventListener('click', () => {
        offset += LIMIT;
        void loadLogs();
    });

    refreshBtn.addEventListener('click', () => {
        void loadLogs();
    });

    queryFilter.addEventListener('change', () => {
        offset = 0;
        void loadLogs();
    });

    queryFilter.addEventListener('keydown', event => {
        if (event.key === 'Enter') {
            offset = 0;
            void loadLogs();
            event.preventDefault();
        }
    });

    closeDetail.addEventListener('click', closeDetailOverlay);
    detailOverlay.addEventListener('click', event => {
        if (event.target === detailOverlay) {
            closeDetailOverlay();
        }
    });

    closeHelp.addEventListener('click', closeHelpOverlay);
    helpOverlay.addEventListener('click', event => {
        if (event.target === helpOverlay) {
            closeHelpOverlay();
        }
    });

    autoRefresh.addEventListener('change', () => {
        if (autoRefresh.checked) {
            autoRefreshTimer = setInterval(() => {
                void loadLogs();
                void fetchRecordingState();
            }, 5000);
        } else {
            clearInterval(autoRefreshTimer);
        }
    });

    recordingToggleBtn.addEventListener('click', () => {
        void toggleRecordingState();
    });

    document.addEventListener('keydown', handleGlobalKeydown);

    void loadLogs();
    renderRecordingState();
    void fetchRecordingState();
})();
