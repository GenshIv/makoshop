document.addEventListener('DOMContentLoaded', () => {
    // Elements
    const btnGenerate = document.getElementById('btn-generate');
    const btnImportLocal = document.getElementById('btn-import-local');
    const uploadZone = document.getElementById('upload-zone');
    const btnPrev = document.getElementById('btn-prev');
    const btnNext = document.getElementById('btn-next');
    const pageIndicator = document.getElementById('page-indicator');
    const fileInput = document.getElementById('file-input');
    const statusDot = document.getElementById('status-dot');
    const statusText = document.getElementById('status-text');
    
    const progressSection = document.getElementById('progress-section');
    const progressPhase = document.getElementById('progress-phase');
    const progressPct = document.getElementById('progress-pct');
    const progressBarFill = document.getElementById('progress-bar-fill');
    const statProcessed = document.getElementById('stat-processed');
    const statSpeed = document.getElementById('stat-speed');
    const statElapsed = document.getElementById('stat-elapsed');

    const filterCategory = document.getElementById('filter-category');
    const filterMerchant = document.getElementById('filter-merchant');
    const filterStatus = document.getElementById('filter-status');
    
    const metricLatency = document.getElementById('metric-latency');
    const metricCount = document.getElementById('metric-count');
    const resultsBody = document.getElementById('results-body');
    const btnRealtimeWrite = document.getElementById('btn-realtime-write');
    const btnSyncIndex = document.getElementById('btn-sync-index');
    const bufferCount = document.getElementById('buffer-count');

    let pollInterval = null;
    let debounceTimer = null;
    let currentSortBy = '';
    let currentSortOrder = '';
    let currentPage = 1;
    let currentMode = 'pre_sorted';

    // --- State Polling ---

    function startPolling() {
        if (pollInterval) clearInterval(pollInterval);
        
        progressSection.classList.remove('hidden');
        btnGenerate.disabled = true;
        if (btnImportLocal) btnImportLocal.disabled = true;
        uploadZone.style.pointerEvents = 'none';
        uploadZone.style.opacity = '0.5';

        pollInterval = setInterval(async () => {
            try {
                const res = await fetch('/api/progress');
                const data = await res.json();
                
                updateStatusUI(data.status);

                if (data.status === 'generating' || data.status === 'importing') {
                    const pct = data.total > 0 ? Math.round((data.processed / data.total) * 100) : 0;
                    progressPhase.textContent = data.status === 'generating' ? 'Phase 1/2: Generating Mock CSV...' : 'Phase 2/2: Importing into MakoDB...';
                    progressPct.textContent = `${pct}%`;
                    progressBarFill.style.width = `${pct}%`;
                    statProcessed.textContent = `${data.processed.toLocaleString()} / ${data.total.toLocaleString()}`;
                    statSpeed.textContent = `${Math.round(data.speed).toLocaleString()} rows/sec`;
                    statElapsed.textContent = `${data.elapsed.toFixed(1)}s`;
                } else if (data.status === 'completed') {
                    clearInterval(pollInterval);
                    pollInterval = null;
                    
                    progressPhase.textContent = 'Import Finished Successfully!';
                    progressPct.textContent = '100%';
                    progressBarFill.style.width = '100%';
                    progressBarFill.style.background = 'var(--success)';
                    statProcessed.textContent = `${data.total.toLocaleString()} / ${data.total.toLocaleString()}`;
                    statSpeed.textContent = `Completed`;
                    
                    // Enable inputs & fetch initial data
                    btnGenerate.disabled = false;
                    if (btnImportLocal) btnImportLocal.disabled = false;
                    uploadZone.style.pointerEvents = 'auto';
                    uploadZone.style.opacity = '1';
                    
                    fetchTransactions();
                } else if (data.status === 'error') {
                    clearInterval(pollInterval);
                    pollInterval = null;
                    progressPhase.textContent = 'Error Encountered!';
                    progressPct.textContent = 'Failed';
                    progressBarFill.style.width = '100%';
                    progressBarFill.style.background = 'var(--error)';
                    alert(`Error: ${data.error}`);
                    
                    btnGenerate.disabled = false;
                    if (btnImportLocal) btnImportLocal.disabled = false;
                    uploadZone.style.pointerEvents = 'auto';
                    uploadZone.style.opacity = '1';
                }
            } catch (err) {
                console.error('Error fetching progress:', err);
            }
        }, 300);
    }

    function updateStatusUI(status) {
        statusDot.className = 'pulse-dot';
        if (status === 'idle') {
            statusDot.classList.add('idle');
            statusText.textContent = 'Ready';
        } else if (status === 'generating' || status === 'importing') {
            statusDot.classList.add('running');
            statusText.textContent = status === 'generating' ? 'Generating Mock CSV...' : 'Importing Database...';
        } else if (status === 'completed') {
            statusDot.classList.add('completed');
            statusText.textContent = 'Database Ready';
        } else if (status === 'error') {
            statusDot.classList.add('error');
            statusText.textContent = 'Import Failed';
        }
    }

    // --- CSV Upload / Generation handlers ---

    btnGenerate.addEventListener('click', async () => {
        try {
            const res = await fetch('/api/generate-mock');
            if (res.ok) {
                startPolling();
            } else {
                const text = await res.text();
                alert(`Error starting generation: ${text}`);
            }
        } catch (err) {
            alert(`Error: ${err.message}`);
        }
    });

    if (btnImportLocal) {
        btnImportLocal.addEventListener('click', async () => {
            try {
                const res = await fetch('/api/import-local-sales');
                if (res.ok) {
                    startPolling();
                } else {
                    const text = await res.text();
                    alert(`Error starting import: ${text}`);
                }
            } catch (err) {
                alert(`Error: ${err.message}`);
            }
        });
    }

    uploadZone.addEventListener('click', () => fileInput.click());

    uploadZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        uploadZone.style.borderColor = 'var(--primary)';
        uploadZone.style.background = 'rgba(6, 182, 212, 0.05)';
    });

    uploadZone.addEventListener('dragleave', () => {
        uploadZone.style.borderColor = 'rgba(255, 255, 255, 0.15)';
        uploadZone.style.background = 'rgba(255, 255, 255, 0.01)';
    });

    uploadZone.addEventListener('drop', (e) => {
        e.preventDefault();
        uploadZone.style.borderColor = 'rgba(255, 255, 255, 0.15)';
        uploadZone.style.background = 'rgba(255, 255, 255, 0.01)';
        
        if (e.dataTransfer.files.length > 0) {
            handleFileUpload(e.dataTransfer.files[0]);
        }
    });

    fileInput.addEventListener('change', () => {
        if (fileInput.files.length > 0) {
            handleFileUpload(fileInput.files[0]);
        }
    });

    async function handleFileUpload(file) {
        if (!file.name.endsWith('.csv')) {
            alert('Please select a valid CSV file.');
            return;
        }

        const formData = new FormData();
        formData.append('file', file);

        try {
            updateStatusUI('importing');
            progressSection.classList.remove('hidden');
            progressPhase.textContent = 'Uploading CSV...';
            progressBarFill.style.width = '10%';
            
            const res = await fetch('/api/upload', {
                method: 'POST',
                body: formData
            });

            if (res.ok) {
                startPolling();
            } else {
                const text = await res.text();
                alert(`Failed to upload file: ${text}`);
                updateStatusUI('idle');
            }
        } catch (err) {
            alert(`Error: ${err.message}`);
            updateStatusUI('idle');
        }
    }

    // --- Search & Filtering Query Execution ---

    async function updateBufferCount() {
        try {
            const res = await fetch('/api/transactions/stats');
            const data = await res.json();
            if (bufferCount) {
                bufferCount.textContent = data.buffered_count.toLocaleString();
            }
        } catch (e) {
            console.error("Failed to fetch buffer stats:", e);
        }
    }

    async function fetchTransactions() {
        updateBufferCount();
        const cat = filterCategory.value.trim();
        const mer = filterMerchant.value.trim();
        const stat = filterStatus.value.trim();

        const params = new URLSearchParams({
            category: cat,
            merchant: mer,
            status: stat,
            limit: '50',
            page: currentPage.toString(),
            mode: currentMode
        });

        if (currentSortBy) {
            params.append('sort_by', currentSortBy);
            params.append('sort_order', currentSortOrder);
        }

        try {
            const res = await fetch(`/api/transactions?${params.toString()}`);
            const data = await res.json();
            
            // Render performance metrics
            metricLatency.textContent = `${data.latency_ms.toFixed(3)} ms`;
            
            // Render table
            renderTable(data.transactions);

            // Update Pagination UI
            const limit = data.limit || 50;
            const totalCount = data.results_count || 0;
            metricCount.textContent = totalCount.toLocaleString();

            const totalPages = Math.max(1, Math.ceil(totalCount / limit));
            pageIndicator.textContent = `Page ${data.page} of ${totalPages}`;

            btnPrev.disabled = data.page <= 1;
            btnNext.disabled = data.page >= totalPages;
        } catch (err) {
            console.error('Error searching transactions:', err);
        }
    }

    function renderTable(txs) {
        resultsBody.innerHTML = '';
        
        if (!txs || txs.length === 0) {
            resultsBody.innerHTML = `
                <tr>
                    <td colspan="14" class="placeholder-row">
                        No transactions found matching your criteria.
                    </td>
                </tr>
            `;
            return;
        }

        txs.forEach(tx => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td class="monospace">${tx.id}</td>
                <td>${tx.date}</td>
                <td>${tx.ship_date || ''}</td>
                <td>${tx.region || ''}</td>
                <td>${tx.country || ''}</td>
                <td>${tx.item_type || ''}</td>
                <td><span class="badge ${tx.sales_channel ? tx.sales_channel.toLowerCase() : ''}">${tx.sales_channel || ''}</span></td>
                <td>${tx.priority || ''}</td>
                <td class="monospace">${tx.units_sold ? tx.units_sold.toLocaleString() : 0}</td>
                <td class="monospace">$${tx.unit_price ? tx.unit_price.toFixed(2) : '0.00'}</td>
                <td class="monospace">$${tx.unit_cost ? tx.unit_cost.toFixed(2) : '0.00'}</td>
                <td class="monospace">$${tx.total_revenue ? tx.total_revenue.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2}) : '0.00'}</td>
                <td class="monospace">$${tx.total_cost ? tx.total_cost.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2}) : '0.00'}</td>
                <td class="monospace" style="color: ${tx.total_profit >= 0 ? 'var(--success)' : 'var(--error)'}">$${tx.total_profit ? tx.total_profit.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2}) : '0.00'}</td>
            `;
            resultsBody.appendChild(tr);
        });
    }

    // Debounced filter handler
    function triggerFilterSearch() {
        if (debounceTimer) clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => {
            currentPage = 1;
            fetchTransactions();
        }, 150);
    }

    [filterCategory, filterMerchant, filterStatus].forEach(input => {
        input.addEventListener('input', triggerFilterSearch);
    });

    // Search mode toggle event listeners
    const searchModeToggle = document.getElementById('search-mode-toggle');
    if (searchModeToggle) {
        searchModeToggle.addEventListener('click', (e) => {
            const btn = e.target.closest('.mode-btn');
            if (!btn) return;
            
            searchModeToggle.querySelectorAll('.mode-btn').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            currentMode = btn.dataset.mode;
            
            currentPage = 1;
            fetchTransactions();
        });
    }

    if (btnRealtimeWrite) {
        btnRealtimeWrite.addEventListener('click', async () => {
            btnRealtimeWrite.disabled = true;
            try {
                const res = await fetch('/api/transactions/create', { method: 'POST' });
                const data = await res.json();
                if (data.success) {
                    btnRealtimeWrite.style.background = 'var(--success)';
                    btnRealtimeWrite.style.color = 'white';
                    btnRealtimeWrite.style.boxShadow = '0 0 15px var(--success)';
                    setTimeout(() => {
                        btnRealtimeWrite.style.background = '';
                        btnRealtimeWrite.style.color = '';
                        btnRealtimeWrite.style.boxShadow = '';
                        btnRealtimeWrite.disabled = false;
                    }, 400);
                    
                    updateBufferCount();
                    fetchTransactions();
                } else {
                    btnRealtimeWrite.disabled = false;
                }
            } catch (e) {
                console.error(e);
                btnRealtimeWrite.disabled = false;
            }
        });
    }

    if (btnSyncIndex) {
        btnSyncIndex.addEventListener('click', async () => {
            btnSyncIndex.disabled = true;
            btnSyncIndex.textContent = "💾 Syncing...";
            try {
                const res = await fetch('/api/transactions/flush', { method: 'POST' });
                const data = await res.json();
                if (data.success) {
                    btnSyncIndex.style.background = 'var(--success)';
                    btnSyncIndex.style.color = 'white';
                    btnSyncIndex.textContent = "💾 Synced!";
                    setTimeout(() => {
                        btnSyncIndex.style.background = '';
                        btnSyncIndex.style.color = '';
                        btnSyncIndex.textContent = "💾 Sync Index";
                        btnSyncIndex.disabled = false;
                    }, 800);
                    
                    updateBufferCount();
                    fetchTransactions();
                } else {
                    btnSyncIndex.textContent = "💾 Sync Index";
                    btnSyncIndex.disabled = false;
                }
            } catch (e) {
                console.error(e);
                btnSyncIndex.textContent = "💾 Sync Index";
                btnSyncIndex.disabled = false;
            }
        });
    }

    // Initial buffer count update
    updateBufferCount();

    // --- Autocomplete Suggestions Dropdowns ---

    function setupAutocomplete(inputEl, suggestionsEl, fieldName) {
        inputEl.addEventListener('input', async () => {
            const query = inputEl.value.trim();
            if (query.length < 1) {
                suggestionsEl.innerHTML = '';
                suggestionsEl.style.display = 'none';
                return;
            }

            try {
                const res = await fetch(`/api/suggest?field=${fieldName}&q=${encodeURIComponent(query)}`);
                const suggestions = await res.json();
                
                suggestionsEl.innerHTML = '';
                if (suggestions && suggestions.length > 0) {
                    suggestions.forEach(item => {
                        const li = document.createElement('li');
                        li.textContent = item;
                        li.addEventListener('click', () => {
                            inputEl.value = item;
                            suggestionsEl.innerHTML = '';
                            suggestionsEl.style.display = 'none';
                            // Immediately execute search
                            currentPage = 1;
                            fetchTransactions();
                        });
                        suggestionsEl.appendChild(li);
                    });
                    suggestionsEl.style.display = 'block';
                } else {
                    suggestionsEl.style.display = 'none';
                }
            } catch (err) {
                console.error(`Error fetching suggestions for ${fieldName}:`, err);
            }
        });

        // Hide suggestions when clicking outside
        document.addEventListener('click', (e) => {
            if (!inputEl.contains(e.target) && !suggestionsEl.contains(e.target)) {
                suggestionsEl.innerHTML = '';
                suggestionsEl.style.display = 'none';
            }
        });

        // Query suggestions on focus
        inputEl.addEventListener('focus', () => {
            if (inputEl.value.trim().length > 0) {
                // Trigger an input event to show suggestions
                inputEl.dispatchEvent(new Event('input'));
            }
        });
    }

    setupAutocomplete(filterCategory, document.getElementById('sug-category'), 'category');
    setupAutocomplete(filterMerchant, document.getElementById('sug-merchant'), 'merchant');
    setupAutocomplete(filterStatus, document.getElementById('sug-status'), 'status');

    // Check if db is already populated on page load
    async function checkInitialState() {
        try {
            const res = await fetch('/api/progress');
            const data = await res.json();
            updateStatusUI(data.status);
            if (data.status === 'completed') {
                fetchTransactions();
            } else if (data.status === 'generating' || data.status === 'importing') {
                startPolling();
            }
        } catch (err) {
            console.error('Error getting initial state:', err);
        }
    }

    // Setup sorting headers
    document.querySelectorAll('.results-table th.sortable').forEach(th => {
        th.addEventListener('click', () => {
            const column = th.getAttribute('data-sort');
            
            if (currentSortBy === column) {
                // Cycle: asc -> desc -> clear
                if (currentSortOrder === 'asc') {
                    currentSortOrder = 'desc';
                } else if (currentSortOrder === 'desc') {
                    currentSortBy = '';
                    currentSortOrder = '';
                }
            } else {
                currentSortBy = column;
                currentSortOrder = 'asc';
            }

            // Update UI indicators
            document.querySelectorAll('.results-table th.sortable').forEach(header => {
                const icon = header.querySelector('.sort-icon');
                const col = header.getAttribute('data-sort');
                if (col === currentSortBy) {
                    icon.textContent = currentSortOrder === 'asc' ? ' ▲' : ' ▼';
                    header.style.color = 'var(--primary)';
                } else {
                    icon.textContent = '';
                    header.style.color = '';
                }
            });

            // Fetch results sorted
            currentPage = 1;
            fetchTransactions();
        });
    });

    // Pagination Listeners
    if (btnPrev && btnNext) {
        btnPrev.addEventListener('click', () => {
            if (currentPage > 1) {
                currentPage--;
                fetchTransactions();
            }
        });

        btnNext.addEventListener('click', () => {
            currentPage++;
            fetchTransactions();
        });
    }

    checkInitialState();
});
