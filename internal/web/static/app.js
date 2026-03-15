(function() {
    const LIMIT = 50;
    let offset = 0;
    let autoRefreshTimer = null;

    const tbody = document.getElementById('log-body');
    const pageInfo = document.getElementById('page-info');
    const prevBtn = document.getElementById('prev-btn');
    const nextBtn = document.getElementById('next-btn');
    const statusFilter = document.getElementById('status-filter');
    const pathFilter = document.getElementById('path-filter');
    const searchFilter = document.getElementById('search-filter');
    const refreshBtn = document.getElementById('refresh-btn');
    const autoRefresh = document.getElementById('auto-refresh');
    const recordingToggleBtn = document.getElementById('recording-toggle-btn');
    const detailOverlay = document.getElementById('detail-overlay');
    const detailContent = document.getElementById('detail-content');
    const closeDetail = document.getElementById('close-detail');
    let recordingState = true;
    let recordingBusy = false;

    function buildURL() {
        let url = `/api/logs?limit=${LIMIT}&offset=${offset}`;
        const status = statusFilter.value;
        if (status) url += `&status=${status}`;
        const path = pathFilter.value.trim();
        if (path) url += `&path=${encodeURIComponent(path)}`;
        const search = searchFilter.value.trim();
        if (search) url += `&q=${encodeURIComponent(search)}`;
        return url;
    }

    function formatTime(ms) {
        if (!ms) return '—';
        return new Date(ms).toLocaleString();
    }

    function formatBytes(b) {
        if (b === null || b === undefined) return '—';
        if (b < 1024) return b + ' B';
        if (b < 1048576) return (b / 1024).toFixed(1) + ' KB';
        return (b / 1048576).toFixed(1) + ' MB';
    }

    async function loadLogs() {
        try {
            const resp = await fetch(buildURL());
            const json = await resp.json();
            renderTable(json.data || [], json.total || 0);
        } catch (e) {
            tbody.innerHTML = `<tr><td colspan="8">Error: ${e.message}</td></tr>`;
        }
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
            alert('Failed to toggle recording: ' + e.message);
        } finally {
            recordingBusy = false;
            renderRecordingState();
        }
    }

    function renderTable(data, total) {
        tbody.innerHTML = '';
        if (!data.length) {
            tbody.innerHTML = '<tr><td colspan="8">No logs found</td></tr>';
        }
        data.forEach(entry => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${entry.id}</td>
                <td>${formatTime(entry.ts_start)}</td>
                <td>${entry.request_method}</td>
                <td>${entry.request_path}</td>
                <td class="status-${Math.floor((entry.status_code||0)/100)}">${entry.status_code || '—'}</td>
                <td>${entry.duration_ms != null ? entry.duration_ms + 'ms' : '—'}</td>
                <td>${formatBytes(entry.req_bytes)}${entry.req_truncated ? ' ⚠' : ''}</td>
                <td>${formatBytes(entry.resp_bytes)}${entry.resp_truncated ? ' ⚠' : ''}</td>
            `;
            tr.style.cursor = 'pointer';
            tr.addEventListener('click', () => showDetail(entry.id));
            tbody.appendChild(tr);
        });

        const page = Math.floor(offset / LIMIT) + 1;
        const pages = Math.ceil(total / LIMIT) || 1;
        pageInfo.textContent = `Page ${page} of ${pages} (${total} total)`;
        prevBtn.disabled = offset === 0;
        nextBtn.disabled = offset + LIMIT >= total;
    }

    async function showDetail(id) {
        try {
            const resp = await fetch(`/api/logs/${id}`);
            const data = await resp.json();
            const entry = data.entry;
            const sv = data.stream_view;
            let currentMode = 'raw';

            function renderResponseBody() {
                const bodyEl = document.getElementById('resp-body-content');
                const toggleBtns = document.querySelectorAll('.sv-toggle-btn');
                const svMsg = document.getElementById('sv-message');

                if (currentMode === 'assembled' && sv.assembled_available) {
                    bodyEl.textContent = sv.assembled_body || '';
                } else {
                    bodyEl.textContent = entry.resp_body || '';
                }

                toggleBtns.forEach(btn => {
                    btn.classList.toggle('active', btn.dataset.mode === currentMode);
                });

                if (sv.assembled_available && sv.reason === 'partial_parse' && currentMode === 'assembled') {
                    svMsg.textContent = 'Assembled (partial parse)';
                    svMsg.classList.remove('hidden');
                } else {
                    svMsg.classList.add('hidden');
                }
            }

            let svToggleHTML = '';
            if (sv.assembled_available) {
                svToggleHTML = `
                    <div class="sv-toggle">
                        <button class="sv-toggle-btn active" data-mode="raw">Raw</button>
                        <button class="sv-toggle-btn" data-mode="assembled">Assembled</button>
                        <span id="sv-message" class="sv-message hidden"></span>
                    </div>`;
            } else if (sv.reason && sv.reason !== 'unsupported_path' && sv.reason !== 'missing_body') {
                svToggleHTML = `
                    <div class="sv-toggle">
                        <button class="sv-toggle-btn active" data-mode="raw">Raw</button>
                        <button class="sv-toggle-btn" data-mode="assembled" disabled>Assembled</button>
                        <span id="sv-message" class="sv-message">${escapeHTML(sv.reason)}</span>
                    </div>`;
            } else {
                svToggleHTML = '<span id="sv-message" class="sv-message hidden"></span>';
            }

            detailContent.innerHTML = `
                <h2>Log #${entry.id}</h2>
                <dl>
                    <dt>Time</dt><dd>${formatTime(entry.ts_start)} → ${formatTime(entry.ts_end)}</dd>
                    <dt>Duration</dt><dd>${entry.duration_ms != null ? entry.duration_ms + 'ms' : '—'}</dd>
                    <dt>Client IP</dt><dd>${entry.client_ip || '—'}</dd>
                    <dt>Method</dt><dd>${entry.request_method}</dd>
                    <dt>Path</dt><dd>${entry.request_path}</dd>
                    <dt>Upstream</dt><dd>${entry.upstream_url}</dd>
                    <dt>Status</dt><dd>${entry.status_code || '—'}</dd>
                    <dt>Error</dt><dd>${entry.error || '—'}</dd>
                </dl>
                <h3>Request Headers</h3>
                <pre>${formatJSON(entry.req_headers_json)}</pre>
                <h3>Request Body (${formatBytes(entry.req_bytes)}${entry.req_truncated ? ' TRUNCATED' : ''})</h3>
                <pre>${escapeHTML(entry.req_body || '')}</pre>
                <h3>Response Headers</h3>
                <pre>${formatJSON(entry.resp_headers_json)}</pre>
                <h3>Response Body (${formatBytes(entry.resp_bytes)}${entry.resp_truncated ? ' TRUNCATED' : ''})</h3>
                ${svToggleHTML}
                <pre id="resp-body-content"></pre>
            `;

            renderResponseBody();

            detailContent.querySelectorAll('.sv-toggle-btn').forEach(btn => {
                btn.addEventListener('click', () => {
                    if (btn.disabled) return;
                    currentMode = btn.dataset.mode;
                    renderResponseBody();
                });
            });

            detailOverlay.classList.remove('hidden');
        } catch (e) {
            alert('Failed to load detail: ' + e.message);
        }
    }

    function formatJSON(s) {
        if (!s) return '—';
        try { return escapeHTML(JSON.stringify(JSON.parse(s), null, 2)); }
        catch { return escapeHTML(s); }
    }

    function escapeHTML(s) {
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    }

    prevBtn.addEventListener('click', () => { offset = Math.max(0, offset - LIMIT); loadLogs(); });
    nextBtn.addEventListener('click', () => { offset += LIMIT; loadLogs(); });
    refreshBtn.addEventListener('click', loadLogs);
    statusFilter.addEventListener('change', () => { offset = 0; loadLogs(); });
    pathFilter.addEventListener('change', () => { offset = 0; loadLogs(); });
    searchFilter.addEventListener('change', () => { offset = 0; loadLogs(); });
    closeDetail.addEventListener('click', () => detailOverlay.classList.add('hidden'));
    detailOverlay.addEventListener('click', (e) => { if (e.target === detailOverlay) detailOverlay.classList.add('hidden'); });

    autoRefresh.addEventListener('change', () => {
        if (autoRefresh.checked) {
            autoRefreshTimer = setInterval(() => {
                loadLogs();
                fetchRecordingState();
            }, 5000);
        } else {
            clearInterval(autoRefreshTimer);
        }
    });
    recordingToggleBtn.addEventListener('click', toggleRecordingState);

    loadLogs();
    renderRecordingState();
    fetchRecordingState();
})();
