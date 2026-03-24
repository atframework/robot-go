package master

// dashboardHTML 内嵌的 Web 前端 SPA
const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Robot Stress Test — Master Dashboard</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
:root{--bg:#0f1117;--card:#1a1d27;--border:#2a2d3a;--accent:#4f8cff;--accent2:#34d399;
--danger:#f87171;--warn:#fbbf24;--text:#e2e8f0;--muted:#94a3b8;--hover:#252836}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;
background:var(--bg);color:var(--text);min-height:100vh}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}

/* Layout */
.header{background:var(--card);border-bottom:1px solid var(--border);padding:12px 24px;
display:flex;align-items:center;justify-content:space-between;position:sticky;top:0;z-index:100}
.header h1{font-size:18px;font-weight:600;display:flex;align-items:center;gap:8px}
.header h1 span{color:var(--accent)}
.header .status{font-size:13px;color:var(--muted)}

.nav{display:flex;gap:4px;background:var(--card);border-bottom:1px solid var(--border);
padding:0 24px;position:sticky;top:49px;z-index:99}
.nav button{background:none;border:none;color:var(--muted);padding:12px 16px;
cursor:pointer;font-size:14px;border-bottom:2px solid transparent;transition:.2s}
.nav button:hover{color:var(--text);background:var(--hover)}
.nav button.active{color:var(--accent);border-bottom-color:var(--accent)}

.container{max-width:1400px;margin:0 auto;padding:24px}
.page{display:none}
.page.active{display:block}

/* Cards */
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px;margin-bottom:24px}
.stat-card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:20px}
.stat-card .label{font-size:12px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px}
.stat-card .value{font-size:28px;font-weight:700;margin-top:4px}
.stat-card .value.green{color:var(--accent2)}
.stat-card .value.blue{color:var(--accent)}
.stat-card .value.yellow{color:var(--warn)}

/* Tables */
.table-wrap{background:var(--card);border:1px solid var(--border);border-radius:8px;overflow:hidden}
.table-wrap h3{padding:16px 20px 0;font-size:15px;font-weight:600}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:10px 20px;border-bottom:1px solid var(--border);font-size:13px}
th{color:var(--muted);font-weight:500;font-size:12px;text-transform:uppercase;letter-spacing:.5px}
th.sortable{cursor:pointer;user-select:none}
th.sortable:hover{color:var(--fg)}
.sort-arrow{font-size:10px;margin-left:3px;color:var(--accent)}
tr:last-child td{border-bottom:none}
tr:hover td{background:var(--hover)}

/* Status badges */
.badge{display:inline-block;padding:2px 10px;border-radius:12px;font-size:12px;font-weight:500}
.badge.online{background:#065f4620;color:var(--accent2);border:1px solid #065f4640}
.badge.offline{background:#7f1d1d20;color:var(--danger);border:1px solid #7f1d1d40}
.badge.running{background:#1e3a5f30;color:var(--accent);border:1px solid #1e3a5f60}
.badge.done{background:#065f4620;color:var(--accent2);border:1px solid #065f4640}
.badge.error{background:#7f1d1d20;color:var(--danger);border:1px solid #7f1d1d40}
.badge.pending{background:#78350f20;color:var(--warn);border:1px solid #78350f40}
.badge.stopped{background:#78350f20;color:var(--warn);border:1px solid #78350f40}
.badge.busy{background:#1e3a5f30;color:var(--accent);border:1px solid #1e3a5f60}

/* Buttons */
.btn{display:inline-flex;align-items:center;gap:6px;padding:8px 16px;border:none;border-radius:6px;
font-size:13px;font-weight:500;cursor:pointer;transition:.15s}
.btn-primary{background:var(--accent);color:#fff}
.btn-primary:hover{background:#3b7bef}
.btn-primary:disabled{opacity:.5;cursor:not-allowed}
.btn-sm{padding:4px 12px;font-size:12px}
.btn-secondary{background:var(--border);color:var(--text)}
.btn-secondary:hover{background:#3a3d4a}
.btn-danger{background:#dc2626;color:#fff}
.btn-danger:hover{background:#b91c1c}
.btn-warn{background:#d97706;color:#fff}
.btn-warn:hover{background:#b45309}

/* Form */
.form-group{margin-bottom:16px}
.form-group label{display:block;font-size:13px;color:var(--muted);margin-bottom:6px}
.form-group input,.form-group textarea,.form-group select{
width:100%;background:var(--bg);border:1px solid var(--border);border-radius:6px;
padding:10px 12px;color:var(--text);font-size:13px;font-family:inherit}
.form-group textarea{resize:vertical;min-height:160px;font-family:'Cascadia Code',Consolas,monospace;font-size:12px}
.form-group input:focus,.form-group textarea:focus{outline:none;border-color:var(--accent)}
.form-row{display:grid;grid-template-columns:1fr 1fr;gap:16px}
.form-card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:24px}
.form-card h3{margin-bottom:16px;font-size:15px}
.form-hint{font-size:11px;color:var(--muted);margin-top:4px}

/* Drop zone */
.drop-zone{position:relative;transition:border-color .2s}
.drop-zone.drag-over{border-color:var(--accent) !important}
.drop-zone.drag-over::after{content:'Drop case file here';position:absolute;inset:0;
background:rgba(79,140,255,.12);display:flex;align-items:center;justify-content:center;
font-size:16px;color:var(--accent);border-radius:6px;pointer-events:none;z-index:10}

/* Toast */
.toast-container{position:fixed;top:60px;right:24px;z-index:1000;display:flex;flex-direction:column;gap:8px}
.toast{padding:12px 20px;border-radius:8px;font-size:13px;animation:slideIn .3s ease;max-width:400px;
box-shadow:0 4px 12px rgba(0,0,0,.4)}
.toast.success{background:#065f46;color:#d1fae5;border:1px solid #059669}
.toast.error{background:#7f1d1d;color:#fecaca;border:1px solid #dc2626}
.toast.info{background:#1e3a5f;color:#bfdbfe;border:1px solid #3b82f6}
@keyframes slideIn{from{transform:translateX(100%);opacity:0}to{transform:translateX(0);opacity:1}}

/* Report viewer */
.report-frame{width:100%;height:calc(100vh - 200px);border:1px solid var(--border);border-radius:8px;background:#fff}
.report-viewer-bar{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}

/* Empty state */
.empty{text-align:center;padding:48px;color:var(--muted)}
.empty .icon{font-size:48px;margin-bottom:12px}

/* Target selector */
.target-mode-toggle{display:flex;gap:0;margin-bottom:10px;border:1px solid var(--border);border-radius:6px;overflow:hidden;width:fit-content}
.target-mode-toggle button{background:none;border:none;color:var(--muted);padding:6px 16px;cursor:pointer;font-size:12px;font-weight:500;transition:.15s}
.target-mode-toggle button.active{background:var(--accent);color:#fff}
.multi-select-wrap{position:relative}
.multi-select-box{min-height:38px;background:var(--bg);border:1px solid var(--border);border-radius:6px;
padding:4px 8px;cursor:pointer;display:flex;flex-wrap:wrap;gap:4px;align-items:center;user-select:none}
.multi-select-box:focus-within{border-color:var(--accent)}
.ms-tag{display:inline-flex;align-items:center;gap:4px;background:var(--accent);color:#fff;
border-radius:4px;padding:2px 8px;font-size:12px}
.ms-tag .rm{cursor:pointer;opacity:.8;font-size:10px}
.ms-tag .rm:hover{opacity:1}
.ms-placeholder{color:var(--muted);font-size:13px;padding:2px 4px}
.ms-dropdown{display:none;position:absolute;top:calc(100% + 4px);left:0;right:0;
background:var(--card);border:1px solid var(--border);border-radius:6px;
max-height:220px;overflow-y:auto;z-index:200;box-shadow:0 4px 16px rgba(0,0,0,.4)}
.ms-dropdown.open{display:block}
.ms-option{padding:8px 14px;font-size:13px;cursor:pointer;display:flex;align-items:center;gap:8px}
.ms-option:hover{background:var(--hover)}
.ms-option.selected{color:var(--accent)}
.ms-option .chk{width:14px;height:14px;border:1px solid var(--border);border-radius:3px;
display:inline-block;flex-shrink:0;position:relative}
.ms-option.selected .chk::after{content:'✓';position:absolute;top:-2px;left:1px;
font-size:11px;color:var(--accent)}
.ms-search{padding:6px 10px;border-bottom:1px solid var(--border)}
.ms-search input{width:100%;background:var(--bg);border:1px solid var(--border);
border-radius:4px;padding:4px 8px;color:var(--text);font-size:12px}
.ms-search input:focus{outline:none;border-color:var(--accent)}

/* Submit Task page — textarea fills remaining viewport height */
#page-submit.active{display:flex;flex-direction:column;height:calc(100vh - 140px)}
#page-submit .form-card{flex:1;min-height:0;display:flex;flex-direction:column}
#page-submit .form-card>.form-group:first-of-type{flex:1;min-height:0;display:flex;flex-direction:column}
#taskContent{flex:1;min-height:0;resize:none}

/* Responsive */
@media(max-width:768px){
.form-row{grid-template-columns:1fr}
.stats{grid-template-columns:1fr 1fr}
.container{padding:16px}
}
</style>
</head>
<body>

<div class="header">
  <h1>&#x1F680; <span>Robot</span> Stress Test Master</h1>
  <div class="status" id="connStatus">Connecting...</div>
</div>

<div class="nav" id="nav">
  <button class="active" data-page="overview">Overview</button>
  <button data-page="agents">Agents</button>
  <button data-page="submit">Submit Task</button>
  <button data-page="tasks">Tasks</button>
  <button data-page="history">History</button>
  <button data-page="reports">Reports</button>
  <button data-page="viewer" id="navViewer" style="display:none">Report Viewer</button>
</div>

<div class="toast-container" id="toasts"></div>

<div class="container">

<!-- ========== Overview ========== -->
<div class="page active" id="page-overview">
  <div class="stats">
    <div class="stat-card"><div class="label">Online Agents</div><div class="value green" id="ov-agents">-</div></div>
    <div class="stat-card"><div class="label">Running Tasks</div><div class="value blue" id="ov-running">-</div></div>
    <div class="stat-card"><div class="label">Completed Tasks</div><div class="value green" id="ov-done">-</div></div>
    <div class="stat-card"><div class="label">Failed Tasks</div><div class="value" style="color:var(--danger)" id="ov-error">-</div></div>
  </div>
  <div class="table-wrap">
    <h3>Recent Tasks</h3>
    <table>
      <thead><tr><th>Report ID</th><th>Status</th><th>Error</th></tr></thead>
      <tbody id="ov-tasks"></tbody>
    </table>
  </div>
</div>

<!-- ========== Agents ========== -->
<div class="page" id="page-agents">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
    <h2 style="font-size:18px">Registered Agents</h2>
    <div style="display:flex;gap:8px">
      <button class="btn btn-warn btn-sm" onclick="rebootAllAgents()">&#x1F504; Reboot All</button>
      <button class="btn btn-secondary btn-sm" onclick="loadAgents()">&#x21BB; Refresh</button>
    </div>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr>
        <th class="sortable" onclick="sortAgents('id')">Agent ID <span id="sort-id" class="sort-arrow">&#x25B2;</span></th>
        <th class="sortable" onclick="sortAgents('group_id')">Group <span id="sort-group_id" class="sort-arrow"></span></th>
        <th class="sortable" onclick="sortAgents('status')">Status <span id="sort-status" class="sort-arrow"></span></th>
        <th class="sortable" onclick="sortAgents('last_seen')">Last Seen <span id="sort-last_seen" class="sort-arrow"></span></th>
        <th>Actions</th>
      </tr></thead>
      <tbody id="agents-body"></tbody>
    </table>
  </div>
  <div class="empty" id="agents-empty" style="display:none">
    <div class="icon">&#x1F4E1;</div>
    <div>No agents registered yet.<br>Start an agent with <code>-mode agent -master-addr http://&lt;master&gt;:8080</code></div>
  </div>
</div>

<!-- ========== Submit Task ========== -->
<div class="page" id="page-submit">
  <div class="form-card" style="max-width:800px">
    <h3>Submit Stress Test Task</h3>
    <div class="form-group">
      <label>Case File Content <span style="font-size:11px;color:var(--accent)">(drag &amp; drop a .conf file here)</span></label>
      <textarea id="taskContent" class="drop-zone" placeholder="#!stress
# caseName  errorBreak  prefix  start  end  batch  qps  runTime
login_bench false       test_   1      1001 50     50   60">#!stress
</textarea>
      <div class="form-hint">First line must be <code>#!stress</code>. Each subsequent line defines a stress case.</div>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label>Report ID (optional, auto-generated if empty)</label>
        <input id="taskReportId" type="text" placeholder="e.g. bench-20250601">
      </div>
      <div class="form-group">
        <label>Repeated Time</label>
        <input id="taskRepeated" type="number" value="1" min="1">
      </div>
    </div>
    <div class="form-row">
      <div class="form-group">
        <label>Target Selection</label>
        <div class="target-mode-toggle">
          <button id="modeGroupBtn" class="active" onclick="setTargetMode('group')">&#x1F4E6; Group Mode</button>
          <button id="modeAgentBtn" onclick="setTargetMode('agent')">&#x1F916; Agent Mode</button>
        </div>
        <!-- Group Mode -->
        <div id="targetGroupPanel">
          <div class="multi-select-wrap" id="groupSelectWrap">
            <div class="multi-select-box" id="groupSelectBox" onclick="toggleDropdown('group')">
              <span class="ms-placeholder" id="groupPlaceholder">All agents (no filter)</span>
            </div>
            <div class="ms-dropdown" id="groupDropdown">
              <div class="ms-search"><input type="text" placeholder="Search groups..." oninput="filterDropdown('group',this.value)" id="groupSearchInput"></div>
              <div id="groupOptionList"></div>
            </div>
          </div>
          <div class="form-hint">Select one or more groups. Leave empty to use all online agents.</div>
        </div>
        <!-- Agent Mode -->
        <div id="targetAgentPanel" style="display:none">
          <div class="multi-select-wrap" id="agentSelectWrap">
            <div class="multi-select-box" id="agentSelectBox" onclick="toggleDropdown('agent')">
              <span class="ms-placeholder" id="agentPlaceholder">Select specific agents...</span>
            </div>
            <div class="ms-dropdown" id="agentDropdown">
              <div class="ms-search"><input type="text" placeholder="Search agents..." oninput="filterDropdown('agent',this.value)" id="agentSearchInput"></div>
              <div id="agentOptionList"></div>
            </div>
          </div>
          <div class="form-hint">Select one or more specific agents to run the task.</div>
        </div>
      </div>
      <div class="form-group">
        <label>Distribute Mode</label>
        <select id="taskDistributeMode">
          <option value="balance">Balance — split OpenID &amp; QPS across agents</option>
          <option value="copy">Copy — each agent runs full workload</option>
        </select>
        <div class="form-hint">Balance: agents share load. Copy: every agent runs the full case independently.</div>
      </div>
    </div>
    <div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap">
      <button class="btn btn-primary" id="btnSubmit" onclick="submitTask()">&#x25B6; Submit Task</button>
      <label style="display:flex;align-items:center;gap:6px;font-size:13px;color:var(--muted);cursor:pointer">
        <input type="checkbox" id="taskRebootBefore"> Reboot target agents before running
      </label>
      <span id="submitStatus" style="font-size:13px;color:var(--muted)"></span>
    </div>
  </div>
</div>

<!-- ========== Tasks ========== -->
<div class="page" id="page-tasks">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
    <h2 style="font-size:18px">Tasks</h2>
    <div style="display:flex;gap:8px">
      <label style="font-size:13px;color:var(--muted);display:flex;align-items:center;gap:6px">
        <input type="checkbox" id="taskAutoRefresh" checked> Auto Refresh
      </label>
      <button class="btn btn-secondary btn-sm" onclick="loadTasks()">&#x21BB; Refresh</button>
    </div>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr><th>Report ID</th><th>Group</th><th>Mode</th><th>Submitted At</th><th>Status</th><th>Error</th><th>Actions</th></tr></thead>
      <tbody id="tasks-body"></tbody>
    </table>
  </div>
  <div class="empty" id="tasks-empty" style="display:none">
    <div class="icon">&#x1F4CB;</div>
    <div>No tasks submitted yet.</div>
  </div>
</div>

<!-- ========== History ========== -->
<div class="page" id="page-history">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
    <h2 style="font-size:18px">Task History</h2>
    <button class="btn btn-secondary btn-sm" onclick="loadHistory()">&#x21BB; Refresh</button>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr><th>Report ID</th><th>Group</th><th>Mode</th><th>Repeated</th><th>Submitted</th><th>Actions</th></tr></thead>
      <tbody id="history-body"></tbody>
    </table>
  </div>
  <div class="empty" id="history-empty" style="display:none">
    <div class="icon">&#x1F4DA;</div>
    <div>No task history yet.</div>
  </div>
</div>

<!-- ========== Reports ========== -->
<div class="page" id="page-reports">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px">
    <h2 style="font-size:18px">Reports</h2>
    <button class="btn btn-secondary btn-sm" onclick="loadReports()">&#x21BB; Refresh</button>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr><th>Report ID</th><th>Title</th><th>Start Time</th><th>End Time</th><th>Agents</th><th>Actions</th></tr></thead>
      <tbody id="reports-body"></tbody>
    </table>
  </div>
  <div class="empty" id="reports-empty" style="display:none">
    <div class="icon">&#x1F4CA;</div>
    <div>No reports available.</div>
  </div>
</div>

<!-- ========== Report Viewer ========== -->
<div class="page" id="page-viewer">
  <div class="report-viewer-bar">
    <h2 style="font-size:18px" id="viewerTitle">Report Viewer</h2>
    <div style="display:flex;gap:8px">
      <button class="btn btn-secondary btn-sm" onclick="regenReportAndReload(currentReportId)">&#x21BB; Regenerate</button>
      <button class="btn btn-secondary btn-sm" onclick="openReportNewTab()">&#x2197; Open in New Tab</button>
      <button class="btn btn-secondary btn-sm" onclick="switchPage('reports')">&#x2190; Back to Reports</button>
    </div>
  </div>
  <iframe class="report-frame" id="reportFrame" src="about:blank"></iframe>
</div>

</div><!-- /container -->

<script>
// ---------- State ----------
let currentPage = 'overview';
let currentReportId = '';
let agentsData = [];
let tasksData = {};
let historyData = [];
let pollTimer = null;

// ---------- Navigation ----------
document.querySelectorAll('.nav button').forEach(btn => {
  btn.addEventListener('click', () => switchPage(btn.dataset.page));
});

function switchPage(page) {
  currentPage = page;
  document.querySelectorAll('.nav button').forEach(b => b.classList.toggle('active', b.dataset.page === page));
  document.querySelectorAll('.page').forEach(p => p.classList.toggle('active', p.id === 'page-' + page));
  if (page === 'overview') { loadOverview(); }
  else if (page === 'agents') { loadAgents(); }
  else if (page === 'tasks') { loadTasks(); }
  else if (page === 'history') { loadHistory(); }
  else if (page === 'reports') { loadReports(); }
}

// ---------- Toast ----------
function toast(msg, type) {
  const el = document.createElement('div');
  el.className = 'toast ' + (type || 'info');
  el.textContent = msg;
  document.getElementById('toasts').appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// ---------- API helpers ----------
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(path, opts);
  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(text || resp.statusText);
  }
  return resp.json();
}

function fmtTime(t) {
  if (!t || t === '0001-01-01T00:00:00Z') return '-';
  const d = new Date(t);
  return d.toLocaleString('zh-CN', { hour12: false });
}

function fmtAgentIds(ids) {
  if (!ids || ids.length === 0) return '-';
  if (ids.length <= 3) return ids.map(escapeHtml).join('<br>');
  const preview = ids.slice(0, 2).map(escapeHtml).join('<br>');
  const all = escapeHtml(ids.join('\n'));
  return preview + '<br><span style="color:var(--muted);cursor:help" title="' + all + '">+' + (ids.length - 2) + ' more</span>';
}

function statusBadge(s) {
  return '<span class="badge ' + (s || '') + '">' + (s || '-') + '</span>';
}

function escapeHtml(s) {
  if (!s) return '';
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

// ---------- Overview ----------
async function loadOverview() {
  try {
    const [agents, tasks, reports] = await Promise.all([
      api('GET', '/api/agents'),
      api('GET', '/api/tasks/all'),
      api('GET', '/api/reports')
    ]);
    agentsData = agents || [];
    tasksData = {};
    const taskList = tasks || [];
    taskList.forEach(t => { tasksData[t.report_id] = t; });

    const onlineAgents = agentsData.filter(a => isOnline(a.last_seen) && (a.status === 'online' || a.status === 'busy'));
    document.getElementById('ov-agents').textContent = onlineAgents.length;
    const running = taskList.filter(t => t.status === 'running').length;
    const done = taskList.filter(t => t.status === 'done').length;
    const err = taskList.filter(t => t.status === 'error' || t.status === 'stopped').length;
    document.getElementById('ov-running').textContent = running;
    document.getElementById('ov-done').textContent = done;
    document.getElementById('ov-error').textContent = err;

    const tbody = document.getElementById('ov-tasks');
    tbody.innerHTML = taskList.slice(-10).reverse().map(t =>
      '<tr><td>' + escapeHtml(t.report_id) + '</td><td>' + statusBadge(t.status) +
      '</td><td style="color:var(--danger);font-size:12px">' + escapeHtml(t.error || '') + '</td></tr>'
    ).join('');

    document.getElementById('connStatus').textContent =
      onlineAgents.length + ' online / ' + agentsData.length + ' agent(s) | ' + taskList.length + ' task(s)';

  } catch (e) {
    document.getElementById('connStatus').textContent = 'Error: ' + e.message;
  }
}

// ---------- Agents ----------
let agentsSort = { col: 'id', asc: true };

function sortAgents(col) {
  if (agentsSort.col === col) {
    agentsSort.asc = !agentsSort.asc;
  } else {
    agentsSort.col = col;
    agentsSort.asc = true;
  }
  renderAgentsTable();
}

function renderAgentsTable() {
  const tbody = document.getElementById('agents-body');
  const empty = document.getElementById('agents-empty');
  if (!agentsData || agentsData.length === 0) {
    tbody.innerHTML = '';
    empty.style.display = 'block';
    return;
  }
  empty.style.display = 'none';
  // Update sort arrow indicators
  ['id', 'group_id', 'status', 'last_seen'].forEach(c => {
    const el = document.getElementById('sort-' + c);
    if (!el) return;
    if (c === agentsSort.col) {
      el.innerHTML = agentsSort.asc ? '&#x25B2;' : '&#x25BC;';
    } else {
      el.innerHTML = '';
    }
  });
  const sorted = [...agentsData].sort((a, b) => {
    let va = a[agentsSort.col] || '';
    let vb = b[agentsSort.col] || '';
    if (agentsSort.col === 'last_seen') {
      va = va ? new Date(va).getTime() : 0;
      vb = vb ? new Date(vb).getTime() : 0;
    } else {
      va = String(va).toLowerCase();
      vb = String(vb).toLowerCase();
    }
    if (va < vb) return agentsSort.asc ? -1 : 1;
    if (va > vb) return agentsSort.asc ? 1 : -1;
    return 0;
  });
  tbody.innerHTML = sorted.map(a => {
    const ago = timeSince(a.last_seen);
    const online = a.status === 'online' || a.status === 'busy';
    return '<tr><td><strong>' + escapeHtml(a.id) + '</strong></td>' +
      '<td>' + escapeHtml(a.group_id || '-') + '</td>' +
      '<td>' + statusBadge(a.status === 'busy' ? 'busy' : (online ? 'online' : 'offline')) + '</td>' +
      '<td style="color:var(--muted);font-size:12px">' + ago + '</td>' +
      '<td><button class="btn btn-warn btn-sm" onclick="rebootAgent(\'' + escapeHtml(a.id).replace(/'/g,"\\'") + '\')">&#x1F504; Reboot</button></td></tr>';
  }).join('');
}

async function loadAgents() {
  try {
    const agents = await api('GET', '/api/agents');
    agentsData = agents || [];
    renderAgentsTable();
  } catch (e) { toast('Load agents failed: ' + e.message, 'error'); }
}

async function rebootAllAgents() {
  if (!confirm('Reboot ALL online agents?\nThis will disconnect all active user connections on every agent.')) return;
  try {
    await api('POST', '/api/agents/reboot', {});
    toast('Reboot commands sent to all agents', 'success');
  } catch (e) {
    toast('Reboot failed: ' + e.message, 'error');
  }
}

async function rebootAgent(id) {
  if (!confirm('Reboot agent "' + id + '"?\nThis will disconnect all active user connections on this agent.')) return;
  try {
    await api('POST', '/api/agents/reboot', { agent_ids: [id] });
    toast('Reboot command sent to: ' + id, 'success');
  } catch (e) {
    toast('Reboot failed: ' + e.message, 'error');
  }
}

function isOnline(lastSeen) {
  if (!lastSeen) return false;
  return (Date.now() - new Date(lastSeen).getTime()) < 30000;
}

function timeSince(t) {
  if (!t) return '-';
  const sec = Math.floor((Date.now() - new Date(t).getTime()) / 1000);
  if (sec < 5) return 'just now';
  if (sec < 60) return sec + 's ago';
  if (sec < 3600) return Math.floor(sec / 60) + 'm ago';
  return Math.floor(sec / 3600) + 'h ago';
}

// ---------- Drag & Drop ----------
(function() {
  const ta = document.getElementById('taskContent');
  ta.addEventListener('dragover', e => { e.preventDefault(); ta.classList.add('drag-over'); });
  ta.addEventListener('dragleave', () => ta.classList.remove('drag-over'));
  ta.addEventListener('drop', e => {
    e.preventDefault();
    ta.classList.remove('drag-over');
    const file = e.dataTransfer.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => { ta.value = reader.result; toast('File loaded: ' + file.name, 'success'); };
    reader.readAsText(file);
  });
})();

// ---------- Target Multi-Select ----------
let targetMode = 'group'; // 'group' or 'agent'
let selectedGroups = [];
let selectedAgents = [];

function setTargetMode(mode) {
  targetMode = mode;
  document.getElementById('modeGroupBtn').classList.toggle('active', mode === 'group');
  document.getElementById('modeAgentBtn').classList.toggle('active', mode === 'agent');
  document.getElementById('targetGroupPanel').style.display = mode === 'group' ? '' : 'none';
  document.getElementById('targetAgentPanel').style.display = mode === 'agent' ? '' : 'none';
  if (mode === 'agent') populateAgentOptions();
  else populateGroupOptions();
}

function populateGroupOptions() {
  // 从已加载的 agentsData 中提取唯一 group
  const groups = [...new Set(agentsData.map(a => a.group_id).filter(Boolean))];
  renderOptions('group', groups.map(g => ({ id: g, label: g })));
}

function populateAgentOptions() {
  renderOptions('agent', agentsData.map(a => ({
    id: a.id,
    label: a.id + (a.group_id ? ' [' + a.group_id + ']' : '') + (a.status === 'online' || a.status === 'busy' ? '' : ' (offline)')
  })));
}

function renderOptions(type, items) {
  const listEl = document.getElementById(type + 'OptionList');
  if (!listEl) return;
  const selected = type === 'group' ? selectedGroups : selectedAgents;
  listEl.innerHTML = items.map(item => {
    const sel = selected.includes(item.id);
    return '<div class="ms-option' + (sel ? ' selected' : '') + '" data-id="' + escapeHtml(item.id) + '" onclick="toggleOption(\'' + type + '\',\'' + escapeHtml(item.id).replace(/'/g,"\\'") + '\')">'
      + '<span class="chk"></span>' + escapeHtml(item.label) + '</div>';
  }).join('');
}

function filterDropdown(type, query) {
  const listEl = document.getElementById(type + 'OptionList');
  if (!listEl) return;
  const q = query.toLowerCase();
  listEl.querySelectorAll('.ms-option').forEach(el => {
    el.style.display = el.dataset.id.toLowerCase().includes(q) || el.textContent.toLowerCase().includes(q) ? '' : 'none';
  });
}

function toggleOption(type, id) {
  const arr = type === 'group' ? selectedGroups : selectedAgents;
  const idx = arr.indexOf(id);
  if (idx === -1) arr.push(id); else arr.splice(idx, 1);
  renderOptions(type, getAllOptions(type));
  renderTags(type);
}

function getAllOptions(type) {
  const listEl = document.getElementById(type + 'OptionList');
  if (!listEl) return [];
  return [...listEl.querySelectorAll('.ms-option')].map(el => ({ id: el.dataset.id, label: el.textContent.trim() }));
}

function renderTags(type) {
  const box = document.getElementById(type + 'SelectBox');
  const ph = document.getElementById(type + 'Placeholder');
  const arr = type === 'group' ? selectedGroups : selectedAgents;
  // 移除旧 tag
  box.querySelectorAll('.ms-tag').forEach(t => t.remove());
  if (arr.length === 0) {
    ph.style.display = '';
    ph.textContent = type === 'group' ? 'All agents (no filter)' : 'Select specific agents...';
  } else {
    ph.style.display = 'none';
    arr.forEach(id => {
      const tag = document.createElement('span');
      tag.className = 'ms-tag';
      tag.innerHTML = escapeHtml(id) + '<span class="rm" onclick="event.stopPropagation();removeOpt(\'' + type + '\',\'' + escapeHtml(id).replace(/'/g,"\\'") + '\')">✕</span>';
      box.insertBefore(tag, ph);
    });
  }
}

function removeOpt(type, id) {
  const arr = type === 'group' ? selectedGroups : selectedAgents;
  const idx = arr.indexOf(id);
  if (idx !== -1) arr.splice(idx, 1);
  renderOptions(type, getAllOptions(type));
  renderTags(type);
}

function toggleDropdown(type) {
  const dd = document.getElementById(type + 'Dropdown');
  const isOpen = dd.classList.contains('open');
  // 关闭所有
  document.querySelectorAll('.ms-dropdown.open').forEach(d => d.classList.remove('open'));
  if (!isOpen) {
    if (type === 'group') populateGroupOptions();
    else populateAgentOptions();
    dd.classList.add('open');
    const inp = document.getElementById(type + 'SearchInput');
    if (inp) { inp.value = ''; filterDropdown(type, ''); inp.focus(); }
  }
}

// 点击外部关闭下拉
document.addEventListener('click', e => {
  if (!e.target.closest('.multi-select-wrap')) {
    document.querySelectorAll('.ms-dropdown.open').forEach(d => d.classList.remove('open'));
  }
});

// ---------- Submit Task ----------
async function submitTask() {
  const content = document.getElementById('taskContent').value.trim();
  if (!content) { toast('Case file content is required', 'error'); return; }
  const reportId = document.getElementById('taskReportId').value.trim();
  const repeated = parseInt(document.getElementById('taskRepeated').value) || 1;
  const distributeMode = document.getElementById('taskDistributeMode').value;
  const btn = document.getElementById('btnSubmit');
  btn.disabled = true;
  document.getElementById('submitStatus').textContent = 'Submitting...';
  try {
    const body = { case_file_content: content, repeated_time: repeated, distribute_mode: distributeMode };
    if (reportId) body.report_id = reportId;
    body.reboot_before_run = document.getElementById('taskRebootBefore').checked;
    if (targetMode === 'agent') {
      if (selectedAgents.length > 0) body.target_agents = selectedAgents.slice();
    } else {
      if (selectedGroups.length === 1) body.target_group = selectedGroups[0];
      else if (selectedGroups.length > 1) body.target_agents_by_group = selectedGroups; // 将在后端扩展
    }
    const result = await api('POST', '/api/tasks', body);
    toast('Task submitted: ' + result.report_id, 'success');
    document.getElementById('submitStatus').textContent = 'Submitted: ' + result.report_id;
    setTimeout(() => switchPage('tasks'), 1000);
  } catch (e) {
    toast('Submit failed: ' + e.message, 'error');
    document.getElementById('submitStatus').textContent = 'Failed: ' + e.message;
  } finally {
    btn.disabled = false;
  }
}

// ---------- Tasks ----------
async function loadTasks() {
  try {
    const tasks = await api('GET', '/api/tasks/all');
    const taskList = tasks || [];
    tasksData = {};
    taskList.forEach(t => { tasksData[t.report_id] = t; });
    const tbody = document.getElementById('tasks-body');
    const empty = document.getElementById('tasks-empty');
    if (taskList.length === 0) {
      tbody.innerHTML = '';
      empty.style.display = 'block';
      return;
    }
    empty.style.display = 'none';
    tbody.innerHTML = taskList.slice().reverse().map(t => {
      let actions = '';
      if (t.status === 'done' || t.status === 'stopped') {
        actions = '<button class="btn btn-sm btn-primary" onclick="viewReport(\'' +
          escapeHtml(t.report_id) + '\')">View Report</button>';
      } else if (t.status === 'running') {
        actions = '<button class="btn btn-sm btn-danger" onclick="stopTask(\'' +
          escapeHtml(t.report_id) + '\',this)">&#x23F9; Stop</button>' +
          ' <button class="btn btn-sm btn-secondary" onclick="previewReport(\'' +
          escapeHtml(t.report_id) + '\')">&#x1F50D; Preview</button>';
      }
      return '<tr><td><strong>' + escapeHtml(t.report_id) + '</strong></td>' +
        '<td>' + escapeHtml(t.target_group || 'all') + '</td>' +
        '<td>' + escapeHtml(t.distribute_mode || 'balance') + '</td>' +
        '<td style="font-size:12px;color:var(--muted)">' + fmtTime(t.submitted_at) + '</td>' +
        '<td>' + statusBadge(t.status) + '</td>' +
        '<td style="color:var(--danger);font-size:12px;max-width:300px;overflow:hidden;text-overflow:ellipsis">' +
        escapeHtml(t.error || '') + '</td>' +
        '<td>' + actions + '</td></tr>';
    }).join('');
  } catch (e) { toast('Load tasks failed: ' + e.message, 'error'); }
}

async function stopTask(id, btn) {
  if (!confirm('Stop task ' + id + '?')) return;
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/api/tasks/' + encodeURIComponent(id) + '/stop');
    toast('Task stopped: ' + id, 'success');
    loadTasks();
  } catch (e) {
    toast('Stop failed: ' + e.message, 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function previewReport(id) {
  toast('Generating preview from partial data...', 'info');
  try {
    await api('POST', '/api/reports/' + encodeURIComponent(id) + '/html');
    viewReport(id);
  } catch (e) {
    toast('Preview failed: ' + e.message, 'error');
  }
}

// ---------- History ----------
async function loadHistory() {
  try {
    const list = await api('GET', '/api/tasks/history');
    historyData = list || [];
    const tbody = document.getElementById('history-body');
    const empty = document.getElementById('history-empty');
    if (historyData.length === 0) {
      tbody.innerHTML = '';
      empty.style.display = 'block';
      return;
    }
    empty.style.display = 'none';
    tbody.innerHTML = historyData.slice().reverse().map(h =>
      '<tr><td><strong>' + escapeHtml(h.report_id) + '</strong></td>' +
      '<td>' + escapeHtml(h.target_group || 'all') + '</td>' +
      '<td>' + escapeHtml(h.distribute_mode || 'balance') + '</td>' +
      '<td>' + (h.repeated_time || 1) + '</td>' +
      '<td style="font-size:12px;color:var(--muted)">' + (h.submitted_at || '-') + '</td>' +
      '<td><button class="btn btn-sm btn-warn" onclick=\'redoHistory(' + escapeHtml(JSON.stringify(JSON.stringify(h))) + ')\'>&#x21BA; Redo</button></td></tr>'
    ).join('');
  } catch (e) { toast('Load history failed: ' + e.message, 'error'); }
}

function redoHistory(jsonStr) {
  const h = JSON.parse(jsonStr);
  document.getElementById('taskContent').value = h.case_file_content || '';
  document.getElementById('taskReportId').value = '';
  document.getElementById('taskRepeated').value = h.repeated_time || 1;
  document.getElementById('taskDistributeMode').value = h.distribute_mode || 'balance';
  // 还原 target 选择
  if (h.target_agents && h.target_agents.length > 0) {
    setTargetMode('agent');
    selectedAgents = h.target_agents.slice();
    populateAgentOptions();
    renderTags('agent');
  } else {
    setTargetMode('group');
    selectedGroups = h.target_group ? [h.target_group] : [];
    renderTags('group');
  }
  switchPage('submit');
  toast('Parameters loaded from history. Modify and submit.', 'info');
}

// ---------- Reports ----------
async function loadReports() {
  try {
    const reports = await api('GET', '/api/reports');
    const list = reports || [];
    const tbody = document.getElementById('reports-body');
    const empty = document.getElementById('reports-empty');
    if (list.length === 0) {
      tbody.innerHTML = '';
      empty.style.display = 'block';
      return;
    }
    empty.style.display = 'none';
    tbody.innerHTML = list.slice().reverse().map(r =>
      '<tr><td><strong>' + escapeHtml(r.report_id) + '</strong></td>' +
      '<td>' + escapeHtml(r.title || '-') + '</td>' +
      '<td style="font-size:12px">' + fmtTime(r.start_time) + '</td>' +
      '<td style="font-size:12px">' + fmtTime(r.end_time) + '</td>' +
      '<td style="font-size:12px">' + fmtAgentIds(r.agent_ids) + '</td>' +
      '<td><div style="display:flex;gap:6px">' +
        '<button class="btn btn-sm btn-primary" onclick="viewReport(\'' + escapeHtml(r.report_id) + '\')">View</button>' +
        '<button class="btn btn-sm btn-secondary" onclick="regenReport(\'' + escapeHtml(r.report_id) + '\',this)">Regenerate</button>' +
        '<button class="btn btn-sm btn-danger" onclick="deleteReport(\'' + escapeHtml(r.report_id) + '\',this)">&#x1F5D1; Delete</button>' +
      '</div></td></tr>'
    ).join('');
  } catch (e) { toast('Load reports failed: ' + e.message, 'error'); }
}

async function regenReport(id, btn) {
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/api/reports/' + encodeURIComponent(id) + '/html');
    toast('Report regenerated: ' + id, 'success');
  } catch (e) {
    toast('Regenerate failed: ' + e.message, 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function deleteReport(id, btn) {
  if (!confirm('Delete report "' + id + '"?\nThis will remove all Redis data and local files.')) return;
  if (btn) btn.disabled = true;
  try {
    await api('DELETE', '/api/reports/' + encodeURIComponent(id));
    toast('Report deleted: ' + id, 'success');
    loadReports();
  } catch (e) {
    toast('Delete failed: ' + e.message, 'error');
    if (btn) btn.disabled = false;
  }
}

// ---------- Report Viewer ----------
function viewReport(id) {
  currentReportId = id;
  document.getElementById('navViewer').style.display = '';
  document.getElementById('viewerTitle').textContent = 'Report: ' + id;
  document.getElementById('reportFrame').src = '/reports/' + encodeURIComponent(id) + '/view';
  switchPage('viewer');
}

function openReportNewTab() {
  if (currentReportId) {
    window.open('/reports/' + encodeURIComponent(currentReportId) + '/view', '_blank');
  }
}

function refreshReportFrame() {
  const frame = document.getElementById('reportFrame');
  if (frame && frame.src !== 'about:blank') {
    const src = frame.src;
    frame.src = 'about:blank';
    frame.src = src;
  }
}

async function regenReportAndReload(id) {
  if (!id) return;
  try {
    await api('POST', '/api/reports/' + encodeURIComponent(id) + '/html');
    toast('Report regenerated: ' + id, 'success');
    const frame = document.getElementById('reportFrame');
    if (frame) {
      const src = '/reports/' + encodeURIComponent(id) + '/view';
      frame.src = 'about:blank';
      setTimeout(function(){ frame.src = src; }, 100);
    }
  } catch(e) {
    toast('Regenerate failed: ' + e.message, 'error');
  }
}

// ---------- Auto Refresh ----------
function startPolling() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(() => {
    if (currentPage === 'overview') loadOverview();
    else if (currentPage === 'agents') loadAgents();
    else if (currentPage === 'tasks' && document.getElementById('taskAutoRefresh').checked) loadTasks();
  }, 3000);
}

// ---------- Init ----------
loadOverview();
startPolling();
</script>
</body>
</html>`
