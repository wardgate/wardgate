package approval

import (
	"net/http"
)

// UIHandler serves the approval dashboard web UI.
type UIHandler struct {
	admin *AdminHandler
}

// NewUIHandler creates a new UI handler.
func NewUIHandler(admin *AdminHandler) *UIHandler {
	return &UIHandler{admin: admin}
}

// ServeHTTP serves the web UI and routes API requests.
func (h *UIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route API requests to admin handler
	if len(path) >= 8 && path[:8] == "/ui/api/" {
		h.admin.ServeHTTP(w, r)
		return
	}

	// Serve the dashboard HTML
	if path == "/ui/" || path == "/ui" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
		return
	}

	http.NotFound(w, r)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Wardgate - Approval Dashboard</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
      background: #0f172a;
      color: #e2e8f0;
      min-height: 100vh;
    }
    .container { max-width: 1200px; margin: 0 auto; padding: 2rem; }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 2rem;
      padding-bottom: 1rem;
      border-bottom: 1px solid #334155;
    }
    h1 { font-size: 1.5rem; font-weight: 600; }
    .logo { display: flex; align-items: center; gap: 0.75rem; }
    .logo svg { width: 32px; height: 32px; }
    
    /* Login */
    .login-container {
      max-width: 400px;
      margin: 4rem auto;
      padding: 2rem;
      background: #1e293b;
      border-radius: 12px;
      border: 1px solid #334155;
    }
    .login-container h2 { margin-bottom: 1.5rem; text-align: center; }
    .form-group { margin-bottom: 1rem; }
    .form-group label { display: block; margin-bottom: 0.5rem; font-size: 0.875rem; color: #94a3b8; }
    .form-group input {
      width: 100%;
      padding: 0.75rem;
      background: #0f172a;
      border: 1px solid #334155;
      border-radius: 6px;
      color: #e2e8f0;
      font-size: 1rem;
    }
    .form-group input:focus { outline: none; border-color: #3b82f6; }
    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      padding: 0.625rem 1rem;
      border-radius: 6px;
      font-size: 0.875rem;
      font-weight: 500;
      cursor: pointer;
      border: none;
      transition: all 0.15s;
    }
    .btn-primary { background: #3b82f6; color: white; }
    .btn-primary:hover { background: #2563eb; }
    .btn-success { background: #22c55e; color: white; }
    .btn-success:hover { background: #16a34a; }
    .btn-danger { background: #ef4444; color: white; }
    .btn-danger:hover { background: #dc2626; }
    .btn-sm { padding: 0.375rem 0.75rem; font-size: 0.8125rem; }
    .btn-full { width: 100%; }
    
    /* Tabs */
    .tabs { display: flex; gap: 0.5rem; margin-bottom: 1.5rem; }
    .tab {
      padding: 0.5rem 1rem;
      background: transparent;
      border: 1px solid #334155;
      border-radius: 6px;
      color: #94a3b8;
      cursor: pointer;
      font-size: 0.875rem;
    }
    .tab.active { background: #3b82f6; border-color: #3b82f6; color: white; }
    .tab:hover:not(.active) { background: #1e293b; }
    
    /* Cards */
    .card {
      background: #1e293b;
      border: 1px solid #334155;
      border-radius: 12px;
      margin-bottom: 1rem;
      overflow: hidden;
    }
    .card-header {
      padding: 1rem;
      border-bottom: 1px solid #334155;
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
    }
    .card-body { padding: 1rem; }
    .card-actions { display: flex; gap: 0.5rem; }
    
    /* Request details */
    .request-meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 1rem; margin-bottom: 1rem; }
    .meta-item { }
    .meta-label { font-size: 0.75rem; color: #64748b; text-transform: uppercase; letter-spacing: 0.05em; }
    .meta-value { font-size: 0.875rem; color: #e2e8f0; margin-top: 0.25rem; }
    .method { display: inline-block; padding: 0.125rem 0.5rem; border-radius: 4px; font-size: 0.75rem; font-weight: 600; }
    .method-GET { background: #22c55e20; color: #22c55e; }
    .method-POST { background: #3b82f620; color: #3b82f6; }
    .method-PUT { background: #f59e0b20; color: #f59e0b; }
    .method-DELETE { background: #ef444420; color: #ef4444; }
    .method-PATCH { background: #8b5cf620; color: #8b5cf6; }
    
    .status { display: inline-block; padding: 0.125rem 0.5rem; border-radius: 4px; font-size: 0.75rem; font-weight: 600; }
    .status-pending { background: #f59e0b20; color: #f59e0b; }
    .status-approved { background: #22c55e20; color: #22c55e; }
    .status-denied { background: #ef444420; color: #ef4444; }
    .status-expired { background: #64748b20; color: #64748b; }
    
    /* Content preview */
    .content-preview {
      background: #0f172a;
      border: 1px solid #334155;
      border-radius: 6px;
      padding: 1rem;
      font-family: 'Monaco', 'Menlo', monospace;
      font-size: 0.8125rem;
      white-space: pre-wrap;
      word-break: break-all;
      max-height: 300px;
      overflow-y: auto;
    }
    .content-label { font-size: 0.75rem; color: #64748b; margin-bottom: 0.5rem; text-transform: uppercase; }
    
    /* Summary */
    .summary { font-size: 0.9375rem; color: #cbd5e1; margin-bottom: 0.5rem; }
    
    /* Empty state */
    .empty-state {
      text-align: center;
      padding: 3rem;
      color: #64748b;
    }
    .empty-state svg { width: 48px; height: 48px; margin-bottom: 1rem; opacity: 0.5; }
    
    /* Refresh indicator */
    .refresh-indicator {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      font-size: 0.75rem;
      color: #64748b;
    }
    .refresh-indicator .dot {
      width: 8px;
      height: 8px;
      background: #22c55e;
      border-radius: 50%;
      animation: pulse 2s infinite;
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; }
      50% { opacity: 0.5; }
    }
    
    /* Toast */
    .toast {
      position: fixed;
      bottom: 2rem;
      right: 2rem;
      padding: 1rem 1.5rem;
      background: #1e293b;
      border: 1px solid #334155;
      border-radius: 8px;
      box-shadow: 0 10px 40px rgba(0,0,0,0.3);
      transform: translateY(100px);
      opacity: 0;
      transition: all 0.3s;
    }
    .toast.show { transform: translateY(0); opacity: 1; }
    .toast.success { border-color: #22c55e; }
    .toast.error { border-color: #ef4444; }
    
    /* Logout */
    .logout-btn {
      background: transparent;
      border: 1px solid #334155;
      color: #94a3b8;
      padding: 0.5rem 1rem;
      border-radius: 6px;
      cursor: pointer;
      font-size: 0.875rem;
    }
    .logout-btn:hover { background: #1e293b; }
    
    /* Expiry timer */
    .expiry { font-size: 0.75rem; color: #f59e0b; }
    .expiry.soon { color: #ef4444; }
  </style>
</head>
<body>
  <div class="container" id="app">
    <!-- Login view -->
    <div id="login-view" class="login-container">
      <h2>Wardgate Admin</h2>
      <form id="login-form">
        <div class="form-group">
          <label for="admin-key">Admin Key</label>
          <input type="password" id="admin-key" placeholder="Enter admin key" autocomplete="current-password">
        </div>
        <button type="submit" class="btn btn-primary btn-full">Login</button>
      </form>
      <p id="login-error" style="color: #ef4444; margin-top: 1rem; text-align: center; display: none;"></p>
    </div>
    
    <!-- Dashboard view (hidden initially) -->
    <div id="dashboard-view" style="display: none;">
      <header>
        <div class="logo">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          </svg>
          <h1>Wardgate Approvals</h1>
        </div>
        <div style="display: flex; align-items: center; gap: 1rem;">
          <div class="refresh-indicator">
            <span class="dot"></span>
            <span>Auto-refresh</span>
          </div>
          <button class="logout-btn" id="logout-btn">Logout</button>
        </div>
      </header>
      
      <div class="tabs">
        <button class="tab active" data-tab="pending">Pending</button>
        <button class="tab" data-tab="history">History</button>
      </div>
      
      <div id="pending-tab">
        <div id="pending-list"></div>
      </div>
      
      <div id="history-tab" style="display: none;">
        <div id="history-list"></div>
      </div>
    </div>
  </div>
  
  <div id="toast" class="toast"></div>
  
  <script>
    const API_BASE = '/ui/api';
    let adminKey = localStorage.getItem('wardgate_admin_key') || '';
    let refreshInterval = null;
    
    // DOM elements
    const loginView = document.getElementById('login-view');
    const dashboardView = document.getElementById('dashboard-view');
    const loginForm = document.getElementById('login-form');
    const loginError = document.getElementById('login-error');
    const pendingList = document.getElementById('pending-list');
    const historyList = document.getElementById('history-list');
    const tabs = document.querySelectorAll('.tab');
    const pendingTab = document.getElementById('pending-tab');
    const historyTab = document.getElementById('history-tab');
    
    // API calls
    async function api(path, method = 'GET') {
      const res = await fetch(API_BASE + path, {
        method,
        headers: { 'Authorization': 'Bearer ' + adminKey }
      });
      if (res.status === 401) {
        logout();
        throw new Error('Unauthorized');
      }
      if (!res.ok) throw new Error(await res.text());
      return res.json();
    }
    
    // Auth
    async function checkAuth() {
      if (!adminKey) return false;
      try {
        await api('/approvals');
        return true;
      } catch {
        return false;
      }
    }
    
    function logout() {
      adminKey = '';
      localStorage.removeItem('wardgate_admin_key');
      clearInterval(refreshInterval);
      loginView.style.display = 'block';
      dashboardView.style.display = 'none';
    }
    
    // Toast
    function showToast(message, type = 'success') {
      const toast = document.getElementById('toast');
      toast.textContent = message;
      toast.className = 'toast show ' + type;
      setTimeout(() => toast.className = 'toast', 3000);
    }
    
    // Render helpers
    function formatTime(dateStr) {
      const d = new Date(dateStr);
      return d.toLocaleString();
    }
    
    function getExpiryText(expiresAt) {
      const now = new Date();
      const exp = new Date(expiresAt);
      const diff = exp - now;
      if (diff <= 0) return { text: 'Expired', soon: true };
      const mins = Math.floor(diff / 60000);
      const secs = Math.floor((diff % 60000) / 1000);
      if (mins > 0) return { text: mins + 'm ' + secs + 's', soon: mins < 1 };
      return { text: secs + 's', soon: true };
    }
    
    function renderApproval(req, showActions = true) {
      const expiry = getExpiryText(req.expires_at);
      const methodClass = 'method-' + req.method;
      
      let contentHtml = '';
      if (req.body) {
        let bodyDisplay = req.body;
        try {
          bodyDisplay = JSON.stringify(JSON.parse(req.body), null, 2);
        } catch {}
        contentHtml = '<div class="content-label">Request Body</div><div class="content-preview">' + escapeHtml(bodyDisplay) + '</div>';
      }
      
      return '<div class="card">' +
        '<div class="card-header">' +
          '<div>' +
            '<span class="method ' + methodClass + '">' + req.method + '</span> ' +
            '<strong>' + escapeHtml(req.endpoint) + '</strong>' +
            '<span style="color: #64748b;"> ' + escapeHtml(req.path) + '</span>' +
            (req.summary ? '<div class="summary">' + escapeHtml(req.summary) + '</div>' : '') +
          '</div>' +
          (showActions ? '<div class="card-actions">' +
            '<button class="btn btn-success btn-sm" onclick="approve(\'' + req.id + '\')">Approve</button>' +
            '<button class="btn btn-danger btn-sm" onclick="deny(\'' + req.id + '\')">Deny</button>' +
          '</div>' : '<span class="status status-' + req.status + '">' + req.status + '</span>') +
        '</div>' +
        '<div class="card-body">' +
          '<div class="request-meta">' +
            '<div class="meta-item"><div class="meta-label">Agent</div><div class="meta-value">' + escapeHtml(req.agent_id || '-') + '</div></div>' +
            '<div class="meta-item"><div class="meta-label">Created</div><div class="meta-value">' + formatTime(req.created_at) + '</div></div>' +
            (showActions ? '<div class="meta-item"><div class="meta-label">Expires</div><div class="meta-value expiry' + (expiry.soon ? ' soon' : '') + '">' + expiry.text + '</div></div>' : '') +
            (req.content_type ? '<div class="meta-item"><div class="meta-label">Type</div><div class="meta-value">' + escapeHtml(req.content_type) + '</div></div>' : '') +
          '</div>' +
          contentHtml +
        '</div>' +
      '</div>';
    }
    
    function escapeHtml(str) {
      if (!str) return '';
      return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }
    
    function renderEmpty(message) {
      return '<div class="empty-state">' +
        '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>' +
        '<p>' + message + '</p>' +
      '</div>';
    }
    
    // Data loading
    async function loadPending() {
      try {
        const data = await api('/approvals');
        if (data.approvals && data.approvals.length > 0) {
          pendingList.innerHTML = data.approvals.map(r => renderApproval(r, true)).join('');
        } else {
          pendingList.innerHTML = renderEmpty('No pending approvals');
        }
      } catch (err) {
        pendingList.innerHTML = '<div class="empty-state"><p>Error loading approvals</p></div>';
      }
    }
    
    async function loadHistory() {
      try {
        const data = await api('/history');
        if (data.history && data.history.length > 0) {
          historyList.innerHTML = data.history.map(r => renderApproval(r, false)).join('');
        } else {
          historyList.innerHTML = renderEmpty('No history yet');
        }
      } catch (err) {
        historyList.innerHTML = '<div class="empty-state"><p>Error loading history</p></div>';
      }
    }
    
    // Actions
    async function approve(id) {
      try {
        await api('/approvals/' + id + '/approve', 'POST');
        showToast('Request approved');
        loadPending();
        loadHistory();
      } catch (err) {
        showToast('Failed to approve: ' + err.message, 'error');
      }
    }
    
    async function deny(id) {
      try {
        await api('/approvals/' + id + '/deny', 'POST');
        showToast('Request denied');
        loadPending();
        loadHistory();
      } catch (err) {
        showToast('Failed to deny: ' + err.message, 'error');
      }
    }
    
    // Make functions global for onclick
    window.approve = approve;
    window.deny = deny;
    
    // Tab switching
    tabs.forEach(tab => {
      tab.addEventListener('click', () => {
        tabs.forEach(t => t.classList.remove('active'));
        tab.classList.add('active');
        if (tab.dataset.tab === 'pending') {
          pendingTab.style.display = 'block';
          historyTab.style.display = 'none';
        } else {
          pendingTab.style.display = 'none';
          historyTab.style.display = 'block';
          loadHistory();
        }
      });
    });
    
    // Login
    loginForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const key = document.getElementById('admin-key').value;
      if (!key) return;
      
      adminKey = key;
      try {
        await api('/approvals');
        localStorage.setItem('wardgate_admin_key', key);
        loginView.style.display = 'none';
        dashboardView.style.display = 'block';
        startDashboard();
      } catch {
        adminKey = '';
        loginError.textContent = 'Invalid admin key';
        loginError.style.display = 'block';
      }
    });
    
    // Logout
    document.getElementById('logout-btn').addEventListener('click', logout);
    
    // Dashboard start
    function startDashboard() {
      loadPending();
      loadHistory();
      refreshInterval = setInterval(loadPending, 3000);
    }
    
    // Init
    (async () => {
      if (await checkAuth()) {
        loginView.style.display = 'none';
        dashboardView.style.display = 'block';
        startDashboard();
      }
    })();
  </script>
</body>
</html>
`
