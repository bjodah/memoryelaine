(function() {
    const LIMIT = 50;
    let offset = 0;
    let autoRefreshTimer = null;

    const tbody = document.getElementById('log-body');
    const pageInfo = document.getElementById('page-info');
    const prevBtn = document.getElementById('prev-btn');
    const nextBtn = document.getElementById('next-btn');
    const queryFilter = document.getElementById('query-filter');
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
        const q = queryFilter.value.trim();
        if (q) url += `&query=${encodeURIComponent(q)}`;
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

            async function fetchBody(part, mode, full) {
                let url = `/api/logs/${id}/body?part=${part}`;
                if (mode) url += `&mode=${mode}`;
                if (full) url += `&full=true`;
                const r = await fetch(url);
                if (!r.ok) {
                    const err = await r.json().catch(() => null);
                    throw new Error(err && err.message ? err.message : `HTTP ${r.status}`);
                }
                return r.json();
            }

            function renderBodySection(containerId, part) {
                const container = document.getElementById(containerId);
                if (!container) return;

                const hasBody = part === 'req' ? entry.has_request_body : entry.has_response_body;
                if (!hasBody) {
                    container.innerHTML = '<em>No body</em>';
                    return;
                }

                const loadBtn = document.createElement('button');
                loadBtn.textContent = part === 'req' ? 'Load Request Body' : 'Load Response Body';
                loadBtn.className = 'load-body-btn';
                container.innerHTML = '';
                container.appendChild(loadBtn);

                loadBtn.addEventListener('click', async () => {
                    container.innerHTML = '<em>Loading…</em>';
                    try {
                        const bodyData = await fetchBody(part, currentMode === 'assembled' && part === 'resp' ? 'assembled' : 'raw', false);
                        renderBodyContent(container, bodyData, part);
                    } catch (e) {
                        container.innerHTML = '<em>Error: ' + escapeHTML(e.message) + '</em>';
                    }
                });
            }

            function renderBodyContent(container, bodyData, part) {
                container.innerHTML = '';
                if (!bodyData.available) {
                    container.innerHTML = '<em>' + escapeHTML(bodyData.reason || 'Not available') + '</em>';
                    return;
                }
                const info = document.createElement('div');
                info.className = 'body-info';
                info.textContent = formatBytes(bodyData.included_bytes) + ' / ' + formatBytes(bodyData.total_bytes) + ' total';
                container.appendChild(info);

                const pre = document.createElement('pre');
                pre.textContent = bodyData.content;
                container.appendChild(pre);

                if (bodyData.truncated) {
                    const fullBtn = document.createElement('button');
                    fullBtn.textContent = 'Load Full';
                    fullBtn.className = 'load-body-btn';
                    container.appendChild(fullBtn);
                    fullBtn.addEventListener('click', async () => {
                        fullBtn.disabled = true;
                        fullBtn.textContent = 'Loading…';
                        try {
                            const mode = currentMode === 'assembled' && part === 'resp' ? 'assembled' : 'raw';
                            const full = await fetchBody(part, mode, true);
                            renderBodyContent(container, full, part);
                        } catch (e) {
                            fullBtn.textContent = 'Error: ' + e.message;
                        }
                    });
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
            }

            detailContent.innerHTML = `
                <h2>Log #${entry.id}</h2>
                ${entry.request_path.endsWith('/chat/completions') && !entry.req_truncated ? '<button id="view-conversation-btn" class="load-body-btn">View Conversation</button>' : ''}
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
                <pre>${formatHeaders(entry.req_headers)}</pre>
                <h3>Request Body (${formatBytes(entry.req_bytes)}${entry.req_truncated ? ' TRUNCATED' : ''})</h3>
                <div id="req-body-container"></div>
                <h3>Response Headers</h3>
                <pre>${formatHeaders(entry.resp_headers)}</pre>
                <h3>Response Body (${formatBytes(entry.resp_bytes)}${entry.resp_truncated ? ' TRUNCATED' : ''})</h3>
                ${svToggleHTML}
                <div id="resp-body-container"></div>
                <div id="conversation-container" class="hidden"></div>
            `;

            renderBodySection('req-body-container', 'req');
            renderBodySection('resp-body-container', 'resp');

            detailContent.querySelectorAll('.sv-toggle-btn').forEach(btn => {
                btn.addEventListener('click', () => {
                    if (btn.disabled) return;
                    currentMode = btn.dataset.mode;
                    detailContent.querySelectorAll('.sv-toggle-btn').forEach(b => {
                        b.classList.toggle('active', b.dataset.mode === currentMode);
                    });
                    const container = document.getElementById('resp-body-container');
                    if (!container) return;
                    if (currentMode === 'assembled' && sv.assembled_available) {
                        container.innerHTML = '<em>Loading…</em>';
                        fetchBody('resp', 'assembled', false).then(bodyData => {
                            renderBodyContent(container, bodyData, 'resp');
                        }).catch(e => {
                            container.innerHTML = '<em>Error: ' + escapeHTML(e.message) + '</em>';
                        });
                    } else if (currentMode === 'raw') {
                        container.innerHTML = '<em>Loading…</em>';
                        fetchBody('resp', 'raw', false).then(bodyData => {
                            renderBodyContent(container, bodyData, 'resp');
                        }).catch(e => {
                            container.innerHTML = '<em>Error: ' + escapeHTML(e.message) + '</em>';
                        });
                    }
                });
            });

            // Conversation view button
            const convBtn = document.getElementById('view-conversation-btn');
            if (convBtn) {
                convBtn.addEventListener('click', async () => {
                    convBtn.disabled = true;
                    convBtn.textContent = 'Loading…';
                    try {
                        const r = await fetch(`/api/logs/${id}/thread`);
                        if (!r.ok) throw new Error(`HTTP ${r.status}`);
                        const thread = await r.json();
                        const container = document.getElementById('conversation-container');
                        container.classList.remove('hidden');
                        container.innerHTML = '';

                        const header = document.createElement('h3');
                        header.textContent = `Conversation (turn ${thread.selected_entry_index + 1} of ${thread.total_entries})`;
                        container.appendChild(header);

                        (thread.messages || []).forEach(msg => {
                            const block = document.createElement('div');
                            block.className = 'conv-message conv-' + msg.role;
                            const roleEl = document.createElement('div');
                            roleEl.className = 'conv-role';
                            roleEl.textContent = msg.role + ' (';
                            const logLink = document.createElement('a');
                            logLink.href = '#';
                            logLink.textContent = 'Log #' + msg.log_id;
                            logLink.onclick = (e) => {
                                e.preventDefault();
                                document.getElementById('conversation-container').classList.add('hidden');
                                showDetail(msg.log_id);
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

                        convBtn.textContent = 'View Conversation';
                        convBtn.disabled = false;
                    } catch (e) {
                        convBtn.textContent = 'Error: ' + e.message;
                        convBtn.disabled = false;
                    }
                });
            }

            detailOverlay.classList.remove('hidden');
        } catch (e) {
            alert('Failed to load detail: ' + e.message);
        }
    }

    function formatHeaders(headers) {
        if (!headers || Object.keys(headers).length === 0) return '—';
        return escapeHTML(JSON.stringify(headers, null, 2));
    }

    function escapeHTML(s) {
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    }

    prevBtn.addEventListener('click', () => { offset = Math.max(0, offset - LIMIT); loadLogs(); });
    nextBtn.addEventListener('click', () => { offset += LIMIT; loadLogs(); });
    refreshBtn.addEventListener('click', loadLogs);
    queryFilter.addEventListener('change', () => { offset = 0; loadLogs(); });
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
