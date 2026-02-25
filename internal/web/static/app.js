// ClaudeTalk Web Client
(function () {
    'use strict';

    // --- State ---
    let ws = null;
    let room = '';
    let sender = '';
    let seenSeqs = new Set();
    let claudeActive = false;

    // --- Color palette (matches Go CLI) ---
    const senderColorPalette = [
        '#89dceb', // Cyan
        '#a6e3a1', // Green
        '#f9e2af', // Yellow
        '#cba6f7', // Magenta
        '#89b4fa', // Blue
        '#f38ba8', // Red
        '#94e2d5', // Bright Cyan / Teal
        '#b4dba0', // Bright Green
    ];

    function senderColor(name) {
        let h = 0;
        for (let i = 0; i < name.length; i++) {
            h = (h * 31 + name.charCodeAt(i)) >>> 0;
        }
        return senderColorPalette[h % senderColorPalette.length];
    }

    // --- DOM refs ---
    const joinScreen = document.getElementById('join-screen');
    const chatScreen = document.getElementById('chat-screen');
    const joinForm = document.getElementById('join-form');
    const roomInput = document.getElementById('room-input');
    const nameInput = document.getElementById('name-input');
    const roomTitle = document.getElementById('room-title');
    const messagesDiv = document.getElementById('messages');
    const msgInput = document.getElementById('msg-input');
    const sendBtn = document.getElementById('send-btn');
    const claudeInput = document.getElementById('claude-input');
    const claudeBtn = document.getElementById('claude-btn');
    const stopBtn = document.getElementById('stop-btn');
    const synopsisBtn = document.getElementById('synopsis-btn');
    const leaveBtn = document.getElementById('leave-btn');
    const participantList = document.getElementById('participant-list');
    const fileList = document.getElementById('file-list');

    // --- API helpers ---
    function apiBase() {
        return window.location.origin;
    }

    async function apiFetch(path, opts) {
        const resp = await fetch(apiBase() + path, opts);
        return resp;
    }

    // --- Join ---
    joinForm.addEventListener('submit', function (e) {
        e.preventDefault();
        room = roomInput.value.trim();
        sender = nameInput.value.trim();
        if (!room || !sender) return;
        joinRoom(room, sender);
    });

    async function joinRoom(r, s) {
        room = r;
        sender = s;

        // Load latest messages
        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/messages/latest?n=50');
            if (resp.ok) {
                const data = await resp.json();
                for (const env of data.messages || []) {
                    renderMessage(env);
                }
            }
        } catch (e) {
            console.error('Failed to load messages:', e);
        }

        // Connect WebSocket
        connectWS();

        // Switch UI
        joinScreen.classList.add('hidden');
        chatScreen.classList.remove('hidden');
        roomTitle.textContent = '#' + room;
        msgInput.focus();

        // Start polling participants/files
        refreshParticipants();
        refreshFiles();
        setInterval(refreshParticipants, 10000);
        setInterval(refreshFiles, 15000);
    }

    // --- WebSocket ---
    let wsHeartbeat = null;

    function connectWS() {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = proto + '//' + window.location.host + '/ws/' + encodeURIComponent(room) + '?sender=' + encodeURIComponent(sender);
        ws = new WebSocket(url);

        ws.onopen = function () {
            console.log('WebSocket connected');
            // Catch up on any messages missed while disconnected.
            catchUpMessages();
            // Heartbeat: send a ping every 30s to keep the connection alive through proxies.
            if (wsHeartbeat) clearInterval(wsHeartbeat);
            wsHeartbeat = setInterval(function () {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({ _ping: true }));
                }
            }, 30000);
        };

        ws.onmessage = function (evt) {
            try {
                const env = JSON.parse(evt.data);
                if (env._ping) return; // ignore server-side pings
                onMessage(env);
            } catch (e) {
                console.error('Parse error:', e);
            }
        };

        ws.onclose = function () {
            console.log('WebSocket closed, reconnecting in 3s...');
            if (wsHeartbeat) { clearInterval(wsHeartbeat); wsHeartbeat = null; }
            setTimeout(connectWS, 3000);
        };

        ws.onerror = function (err) {
            console.error('WebSocket error:', err);
        };
    }

    async function catchUpMessages() {
        if (seenSeqs.size === 0) return;
        const lastSeq = Math.max(...seenSeqs);
        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/messages?after=' + lastSeq + '&limit=100');
            if (resp.ok) {
                const data = await resp.json();
                for (const env of data.messages || []) {
                    renderMessage(env);
                }
            }
        } catch (e) { /* ignore */ }
    }

    function onMessage(env) {
        // Deduplicate by seq
        if (env.seq && seenSeqs.has(env.seq)) return;
        if (env.seq) seenSeqs.add(env.seq);
        // Client-side guard: drop private messages not meant for this user.
        if (env.metadata && env.metadata.private === 'true') {
            if (env.sender !== sender && env.metadata.to !== sender) return;
        }
        renderMessage(env);
        refreshParticipants();
    }

    // --- Render messages ---
    function renderMessage(env) {
        if (env.seq && seenSeqs.has(env.seq) && messagesDiv.querySelector('[data-seq="' + env.seq + '"]')) return;
        if (env.seq) seenSeqs.add(env.seq);

        const el = document.createElement('div');
        el.className = 'msg';
        if (env.seq) el.dataset.seq = env.seq;

        if (env.type === 'system') {
            el.className = 'msg msg-system';
            const ts = formatTime(env.timestamp);
            el.textContent = '[' + ts + '] ' + (env.payload && env.payload.text || '');
            messagesDiv.appendChild(el);
            scrollToBottom();
            return;
        }

        const ts = formatTime(env.timestamp);
        const color = senderColor(env.sender);
        const isBot = env.metadata && env.metadata.is_claude === 'true';

        let html = '<span class="seq">#' + (env.seq || 0) + '</span>';
        html += '<span class="timestamp">' + escHtml(ts) + '</span>';
        html += '<span class="sender" style="color:' + color + '">' + escHtml(env.sender) + '</span>';

        if (isBot) {
            html += '<span class="badge badge-bot">BOT</span>';
        }

        if (env.metadata && env.metadata.private === 'true') {
            el.classList.add('msg-whisper');
            html += ' <span class="directed whisper-label">&#x1F512; whisper &rarr; ' + escHtml(env.metadata.to) + '</span>';
        } else if (env.metadata && env.metadata.to) {
            html += ' <span class="directed">&rarr; ' + escHtml(env.metadata.to) + '</span>';
        }

        switch (env.type) {
            case 'code':
                el.classList.add('msg-code');
                html += '<pre><code>' + escHtml(env.payload.code || '') + '</code></pre>';
                break;
            case 'diff':
                el.classList.add('msg-diff');
                html += '<pre>' + escHtml(env.payload.diff || '') + '</pre>';
                break;
            default:
                html += ' ' + escHtml(env.payload && env.payload.text || '');
                break;
        }

        el.innerHTML = html;
        messagesDiv.appendChild(el);
        scrollToBottom();
    }

    function formatTime(ts) {
        if (!ts) return '';
        const d = new Date(ts);
        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    }

    function escHtml(s) {
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    }

    function scrollToBottom() {
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
    }

    // --- Send message ---
    sendBtn.addEventListener('click', sendMessage);
    msgInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') sendMessage();
    });

    async function sendMessage() {
        const text = msgInput.value.trim();
        if (!text) return;
        msgInput.value = '';

        try {
            await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/messages', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ sender: sender, type: 'text', payload: { text: text } }),
            });
        } catch (e) {
            console.error('Send failed:', e);
        }
    }

    // --- Ask Claude ---
    claudeBtn.addEventListener('click', askClaude);
    claudeInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') askClaude();
    });

    async function askClaude() {
        const prompt = claudeInput.value.trim();
        if (!prompt) return;
        claudeInput.value = '';
        claudeBtn.disabled = true;
        claudeActive = true;
        stopBtn.classList.remove('hidden');

        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/spawn', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ sender: sender, prompt: prompt }),
            });
            if (!resp.ok) {
                const data = await resp.json().catch(() => ({}));
                console.error('Spawn failed:', data.error || resp.statusText);
            }
        } catch (e) {
            console.error('Spawn request failed:', e);
        }

        // Re-enable after a short delay (server runs async)
        setTimeout(function () {
            claudeBtn.disabled = false;
            claudeActive = false;
            stopBtn.classList.add('hidden');
        }, 2000);
    }

    // --- Stop Claude ---
    stopBtn.addEventListener('click', async function () {
        try {
            await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/stop', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ sender: sender }),
            });
        } catch (e) {
            console.error('Stop failed:', e);
        }
        claudeBtn.disabled = false;
        claudeActive = false;
        stopBtn.classList.add('hidden');
    });

    // --- Synopsis ---
    synopsisBtn.addEventListener('click', async function () {
        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/synopsis', {
                method: 'POST',
            });
            if (resp.ok) {
                const blob = await resp.blob();
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = room + '-synopsis.md';
                document.body.appendChild(a);
                a.click();
                a.remove();
                URL.revokeObjectURL(url);
            }
        } catch (e) {
            console.error('Synopsis failed:', e);
        }
    });

    // --- Leave ---
    leaveBtn.addEventListener('click', function () {
        if (ws) {
            ws.onclose = null;
            ws.close();
            ws = null;
        }
        seenSeqs.clear();
        messagesDiv.innerHTML = '';
        chatScreen.classList.add('hidden');
        joinScreen.classList.remove('hidden');
    });

    // --- Participants ---
    async function refreshParticipants() {
        if (!room) return;
        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/participants');
            if (!resp.ok) return;
            const data = await resp.json();
            participantList.innerHTML = '';
            const participants = data.participants || [];
            if (participants.length === 0) {
                participantList.innerHTML = '<li class="muted">No one here yet</li>';
                return;
            }
            for (const p of participants) {
                const li = document.createElement('li');
                li.className = p.connected ? 'participant-connected' : 'participant-disconnected';
                li.textContent = p.name;
                if (p.role && p.role !== 'user') {
                    li.textContent += ' (' + p.role + ')';
                }
                participantList.appendChild(li);
            }
        } catch (e) {
            // Ignore refresh errors
        }
    }

    // --- Files ---
    async function refreshFiles() {
        if (!room) return;
        try {
            const resp = await apiFetch('/api/rooms/' + encodeURIComponent(room) + '/files');
            if (!resp.ok) return;
            const data = await resp.json();
            fileList.innerHTML = '';
            const files = data.files || [];
            if (files.length === 0) {
                fileList.innerHTML = '<li class="muted">No files shared</li>';
                return;
            }
            for (const f of files) {
                const li = document.createElement('li');
                const a = document.createElement('a');
                a.href = apiBase() + '/api/rooms/' + encodeURIComponent(room) + '/files/' + f.id;
                a.textContent = f.filename;
                a.style.color = 'var(--accent)';
                a.target = '_blank';
                li.appendChild(a);
                fileList.appendChild(li);
            }
        } catch (e) {
            // Ignore refresh errors
        }
    }
})();
