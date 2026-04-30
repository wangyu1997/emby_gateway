package admin

var adminPage = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Emby 302 Proxy</title>
<style>
:root{--bg:#0f1117;--card:#1a1d27;--border:#2a2d3a;--primary:#6c5ce7;--primary-hover:#7c6cf7;--success:#00b894;--warning:#fdcb6e;--danger:#e17055;--text:#e2e2e2;--text-dim:#8b8d9a;--input-bg:#141620;--radius:12px}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
.header{background:linear-gradient(135deg,#6c5ce7 0%,#a29bfe 100%);padding:24px 32px;display:flex;justify-content:space-between;align-items:center}
.header h1{font-size:24px;font-weight:600;color:#fff}
.header p{color:rgba(255,255,255,0.8);font-size:14px;margin-top:4px}
.stats-bar{display:flex;gap:16px}
.stat-chip{background:rgba(255,255,255,0.15);backdrop-filter:blur(10px);padding:8px 16px;border-radius:8px;text-align:center}
.stat-chip .val{font-size:18px;font-weight:700;color:#fff}
.stat-chip .label{font-size:11px;color:rgba(255,255,255,0.7);text-transform:uppercase}
.container{max-width:960px;margin:32px auto;padding:0 20px}
.card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:24px;margin-bottom:24px}
.card-header{display:flex;align-items:center;gap:10px;margin-bottom:20px;padding-bottom:16px;border-bottom:1px solid var(--border)}
.card-header .icon{width:36px;height:36px;background:var(--primary);border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:18px}
.card-header h2{font-size:18px;font-weight:600}
.card-header span{color:var(--text-dim);font-size:13px}
.form-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:16px}
.form-group{display:flex;flex-direction:column;gap:6px}
.form-group.full{grid-column:1/-1}
.form-group label{font-size:13px;color:var(--text-dim);font-weight:500}
.form-group input,.form-group select{background:var(--input-bg);border:1px solid var(--border);border-radius:8px;padding:10px 14px;color:var(--text);font-size:14px;outline:none;transition:border-color 0.2s}
.form-group input:focus,.form-group select:focus{border-color:var(--primary)}
.btn{padding:10px 24px;border:none;border-radius:8px;font-size:14px;font-weight:600;cursor:pointer;transition:all 0.2s}
.btn-primary{background:var(--primary);color:#fff}
.btn-primary:hover{background:var(--primary-hover)}
.btn-secondary{background:var(--border);color:var(--text)}
.actions{display:flex;gap:12px;justify-content:flex-end;margin-top:20px;padding-top:20px;border-top:1px solid var(--border)}
.toast{position:fixed;bottom:32px;right:32px;background:var(--success);color:#fff;padding:14px 24px;border-radius:10px;font-weight:500;box-shadow:0 8px 32px rgba(0,0,0,0.3);transform:translateY(100px);opacity:0;transition:all 0.3s;z-index:100}
.toast.show{transform:translateY(0);opacity:1}
.toast.error{background:var(--danger)}
.tracker-table{width:100%;border-collapse:separate;border-spacing:0 8px}
.tracker-table th{text-align:left;padding:8px 12px;font-size:12px;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.5px}
.tracker-table td{padding:12px;background:var(--input-bg);font-size:13px}
.tracker-table tr td:first-child{border-radius:8px 0 0 8px}
.tracker-table tr td:last-child{border-radius:0 8px 8px 0}
.badge{display:inline-block;padding:3px 10px;border-radius:20px;font-size:12px;font-weight:600}
.badge-redirect{background:rgba(0,184,148,0.15);color:var(--success)}
.badge-proxy{background:rgba(253,203,110,0.15);color:var(--warning)}
.badge-fallback{background:rgba(225,112,85,0.15);color:var(--danger)}
.empty-state{text-align:center;padding:40px;color:var(--text-dim)}

/* Login */
.login-page{display:flex;align-items:center;justify-content:center;min-height:100vh}
.login-card{background:var(--card);border:1px solid var(--border);border-radius:16px;padding:48px;width:100%;max-width:400px;text-align:center}
.login-card h2{font-size:24px;margin-bottom:8px}
.login-card p{color:var(--text-dim);margin-bottom:32px}
.login-card .form-group{margin-bottom:16px;text-align:left}
.login-card .btn{width:100%;margin-top:8px}

@media(max-width:640px){.form-grid{grid-template-columns:1fr}.stats-bar{flex-wrap:wrap}.header{flex-direction:column;align-items:flex-start;gap:16px}}
</style>
</head>
<body>

<div id="login-view" class="login-page">
  <div class="login-card">
    <h2>Emby 302 Proxy</h2>
    <p>管理后台登录</p>
    <div class="form-group"><label>用户名</label><input id="login-user" placeholder="admin"></div>
    <div class="form-group"><label>密码</label><input id="login-pass" type="password" placeholder=""></div>
    <button class="btn btn-primary" onclick="doLogin()">登录</button>
  </div>
</div>

<div id="app-view" style="display:none">
<div class="header">
  <div><h1>Emby 302 Proxy</h1><p>IP 分城路由 · STRM 302 转发</p></div>
  <div class="stats-bar">
    <div class="stat-chip"><div class="val" id="sHits">0</div><div class="label">缓存命中</div></div>
    <div class="stat-chip"><div class="val" id="sRate">0%</div><div class="label">命中率</div></div>
    <div class="stat-chip"><div class="val" id="sConns">0</div><div class="label">活跃连接</div></div>
  </div>
</div>

<div class="container">
  <div class="card">
    <div class="card-header"><div class="icon" style="background:#00b894">📡</div><div><h2>活跃客户端</h2><span>正在连接 / 播放的客户端及路由策略</span></div></div>
    <div id="trackers-container"><div class="empty-state"><p>暂无活跃客户端</p></div></div>
  </div>

  <div class="card">
    <div class="card-header"><div class="icon">🖥️</div><div><h2>代理服务</h2><span>代理监听端口与签名密钥</span></div></div>
    <div class="form-grid">
      <div class="form-group"><label>代理端口</label><input id="server_port" type="number"></div>
      <div class="form-group"><label>管理后台端口</label><input id="server_admin_port" type="number"></div>
      <div class="form-group full"><label>签名密钥</label><input id="server_secret"></div>
    </div>
  </div>

  <div class="card">
    <div class="card-header"><div class="icon">📺</div><div><h2>Emby 服务器</h2><span>源 Emby 地址与 API Key</span></div></div>
    <div class="form-grid">
      <div class="form-group"><label>Emby 地址</label><input id="emby_url" placeholder="http://127.0.0.1:8096"></div>
      <div class="form-group"><label>API Key</label><input id="emby_api_key"></div>
    </div>
  </div>

  <div class="card">
    <div class="card-header"><div class="icon">👤</div><div><h2>管理后台账号</h2><span>修改登录用户名和密码</span></div></div>
    <div class="form-grid">
      <div class="form-group"><label>用户名</label><input id="admin_username"></div>
      <div class="form-group"><label>密码</label><input id="admin_password" type="password"></div>
    </div>
  </div>

  <div class="card">
    <div class="card-header"><div class="icon">🌍</div><div><h2>GeoIP 配置</h2><span>地理位置数据库与查询策略</span></div></div>
    <div class="form-grid">
      <div class="form-group"><label>数据库路径</label><input id="geoip_db_path"></div>
      <div class="form-group"><label>服务器所在城市</label><input id="geoip_server_city" placeholder="北京"></div>
      <div class="form-group"><label>自动下载</label><select id="geoip_auto_download"><option value="true">启用</option><option value="false">禁用</option></select></div>
      <div class="form-group"><label>定时更新间隔</label><input id="geoip_auto_update" placeholder="24h"></div>
      <div class="form-group"><label>IP 缓存 TTL</label><input id="geoip_ip_cache_ttl" placeholder="1h"></div>
      <div class="form-group"><label>API 备用地址</label><input id="geoip_api_fallback_url" placeholder="留空不使用"></div>
    </div>
  </div>

  <div class="card">
    <div class="card-header"><div class="icon">🔄</div><div><h2>路由策略</h2><span>同城与异地流量处理方式</span></div></div>
    <div class="form-grid">
      <div class="form-group"><label>同城策略</label><select id="routing_same_city"><option value="redirect">302 直链</option><option value="proxy">本地代理</option></select></div>
      <div class="form-group"><label>异地策略</label><select id="routing_different_city"><option value="proxy">本地代理</option><option value="redirect">302 直链</option></select></div>
      <div class="form-group"><label>兜底策略</label><select id="routing_fallback"><option value="proxy">本地代理</option><option value="redirect">302 直链</option></select></div>
    </div>
  </div>

  <div class="actions">
    <button class="btn btn-secondary" onclick="loadConfig()">重置</button>
    <button class="btn btn-primary" onclick="saveConfig()">保存配置</button>
  </div>
</div>
</div>

<div class="toast" id="toast"></div>

<script>
let token = localStorage.getItem('token');

if (token) showApp(); else showLogin();

function showLogin() { document.getElementById('login-view').style.display=''; document.getElementById('app-view').style.display='none'; }
function showApp() { document.getElementById('login-view').style.display='none'; document.getElementById('app-view').style.display=''; loadConfig(); loadStats(); setInterval(loadStats, 5000); }

async function doLogin() {
  try {
    const r = await fetch('/api/login', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({username: document.getElementById('login-user').value, password: document.getElementById('login-pass').value}) });
    const d = await r.json();
    if (d.error) { toast(d.error, true); return; }
    token = d.token;
    localStorage.setItem('token', token);
    showApp();
  } catch(e) { toast('登录失败', true); }
}

function headers() { return {'Content-Type': 'application/json', 'Authorization': token}; }

async function loadConfig() {
  try {
    const r = await fetch('/api/config', {headers: headers()});
    if (r.status === 401) { localStorage.removeItem('token'); showLogin(); return; }
    const c = await r.json();
    V('server_port', c.server?.port || 8095);
    V('server_admin_port', c.server?.admin_port || 8098);
    V('server_secret', c.server?.secret || '');
    V('emby_url', c.emby?.url || '');
    V('emby_api_key', c.emby?.api_key || '');
    V('admin_username', c.admin?.username || '');
    V('admin_password', c.admin?.password || '');
    V('geoip_db_path', c.geoip?.db_path || '');
    V('geoip_server_city', c.geoip?.server_city || '');
    V('geoip_auto_download', String(c.geoip?.auto_download ?? true));
    V('geoip_auto_update', c.geoip?.auto_update || '24h');
    V('geoip_ip_cache_ttl', c.geoip?.ip_cache_ttl || '1h');
    V('geoip_api_fallback_url', c.geoip?.api_fallback_url || '');
    V('routing_same_city', c.routing?.same_city || 'redirect');
    V('routing_different_city', c.routing?.different_city || 'proxy');
    V('routing_fallback', c.routing?.fallback || 'proxy');
  } catch(e) { toast('加载失败', true); }
}

function V(id, val) { const el = document.getElementById(id); if(el) el.value = val; }

async function saveConfig() {
  const cfg = {
    server: { port: parseInt(document.getElementById('server_port').value)||8095, admin_port: parseInt(document.getElementById('server_admin_port').value)||8098, secret: document.getElementById('server_secret').value },
    admin: { username: document.getElementById('admin_username').value, password: document.getElementById('admin_password').value },
    emby: { url: document.getElementById('emby_url').value, api_key: document.getElementById('emby_api_key').value },
    geoip: { db_path: document.getElementById('geoip_db_path').value, server_city: document.getElementById('geoip_server_city').value, auto_download: document.getElementById('geoip_auto_download').value==='true', auto_update: document.getElementById('geoip_auto_update').value, ip_cache_ttl: document.getElementById('geoip_ip_cache_ttl').value, api_fallback_url: document.getElementById('geoip_api_fallback_url').value },
    routing: { same_city: document.getElementById('routing_same_city').value, different_city: document.getElementById('routing_different_city').value, fallback: document.getElementById('routing_fallback').value }
  };
  try {
    const r = await fetch('/api/config', { method: 'POST', headers: headers(), body: JSON.stringify(cfg) });
    if (r.status === 401) { localStorage.removeItem('token'); showLogin(); return; }
    const d = await r.json();
    if (d.error) { toast(d.error, true); return; }
    toast('配置已保存');
  } catch(e) { toast('保存失败', true); }
}

async function loadStats() {
  try {
    const r = await fetch('/api/stats', {headers: headers()});
    const d = await r.json();
    document.getElementById('sHits').textContent = d.cache_hits;
    document.getElementById('sRate').textContent = d.cache_rate + '%';
  } catch(e) {}
  try {
    const r = await fetch('/api/trackers', {headers: headers()});
    const data = await r.json();
    document.getElementById('sConns').textContent = data.length;
    renderTrackers(data);
  } catch(e) {}
}

function renderTrackers(list) {
  const c = document.getElementById('trackers-container');
  if (!list || list.length === 0) { c.innerHTML = '<div class="empty-state"><p>暂无活跃客户端</p></div>'; return; }
  var h = '<table class="tracker-table"><thead><tr><th>客户端 IP</th><th>城市</th><th>策略</th><th>媒体路径</th><th>连接时间</th></tr></thead><tbody>';
  for (var i = 0; i < list.length; i++) {
    var t = list[i];
    var cls = t.strategy==='redirect'?'badge-redirect':(t.strategy==='proxy'?'badge-proxy':'badge-fallback');
    h += '<tr><td><code>'+esc(t.ip)+'</code></td><td>'+esc(t.city||'-')+' '+esc(t.province||'')+'</td><td><span class="badge '+cls+'">'+esc(t.strategy)+'</span></td><td style="max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(t.media_url)+'">'+esc(trunc(t.media_url,60))+'</td><td>'+esc(t.started||'-')+'</td></tr>';
  }
  h += '</tbody></table>';
  c.innerHTML = h;
}

function esc(s) { if(!s) return ''; return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function trunc(s, n) { return s && s.length>n ? s.substring(0,n)+'...' : (s||''); }
function toast(msg, err) { const t = document.getElementById('toast'); t.textContent = msg; t.className = 'toast show'+(err?' error':''); setTimeout(()=>t.className='toast', 3000); }
</script>
</body>
</html>`)
