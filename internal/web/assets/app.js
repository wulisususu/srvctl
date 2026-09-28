'use strict';

/* Server Manager 前端。纯 vanilla JS —— 没有 npm、没有构建步骤。
   改完直接 go build 就生效。 */

const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = (sel) => document.querySelector(sel);

let S = {
  has_vault: false, unlocked: false, servers: [],
  results: {}, ssid: '', vault_path: '', remembered: false,
};
let editingName = null;
let toastTimer = null;

/* ───────── API ───────── */

async function api(path, body) {
  const opts = { headers: { 'X-Srvctl-Token': TOKEN } };
  if (body !== undefined) {
    opts.method = 'POST';
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  let data = {};
  try { data = await res.json(); } catch { /* 可能没有响应体 */ }
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function toast(msg, kind) {
  const el = $('#toast');
  el.textContent = msg;
  el.className = 'toast ' + (kind || '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast hidden'; }, kind === 'error' ? 6000 : 2600);
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // 剪贴板 API 不可用时的兜底（127.0.0.1 属于安全上下文，一般用不到）
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { ok = false; }
    ta.remove();
    return ok;
  }
}

/* ───────── 状态点 ───────── */

const DOT = {
  ok:        { cls: 'green',   title: '可连接' },
  down:      { cls: 'red',     title: '不可连接' },
  wrong_net: { cls: 'grey',    title: '当前网络环境不满足' },
  skipped:   { cls: 'none',    title: '不检测' },
  unknown:   { cls: 'unknown', title: '尚未检测' },
};

function dotFor(srv) {
  const r = S.results[srv.name];
  let key = 'unknown', note = '';
  if (r) { key = r.state; note = r.note || (r.state === 'down' ? r.err || '' : ''); }
  else if (srv.check_mode === 'none') key = 'skipped';
  const d = DOT[key] || DOT.unknown;
  const title = note ? `${d.title} —— ${note}` : d.title;
  return `<i class="dot ${d.cls}" title="${esc(title)}"></i>`;
}

/* 灰色/红色时把原因写出来，否则用户不知道为什么 */
function noteFor(srv) {
  const r = S.results[srv.name];
  if (!r) return '';
  if (r.note) return r.note;
  if (r.state === 'down' && r.err) return r.err;
  return '';
}

function latencyFor(srv) {
  const r = S.results[srv.name];
  if (!r || r.state !== 'ok') return '—';
  return r.latency_ms + ' ms';
}

/* ───────── 渲染 ───────── */

function render() {
  if (!S.unlocked) {
    $('#main').classList.add('hidden');
    $('#gate').classList.remove('hidden');
    updateGate();
    return;
  }

  $('#gate').classList.add('hidden');
  $('#main').classList.remove('hidden');
  $('#ssid').textContent = S.ssid || '未知（有线或无 WLAN 网卡）';

  const list = [...S.servers].sort((a, b) => {
    const c = (a.category || '').localeCompare(b.category || '', 'zh');
    return c !== 0 ? c : a.name.localeCompare(b.name, 'zh');
  });

  $('#count').textContent = list.length ? `共 ${list.length} 台` : '';

  $('#rows').innerHTML = list.map((s) => `
    <tr>
      <td>${dotFor(s)}</td>
      <td class="name">${esc(s.name)}</td>
      <td class="mono">${esc(s.host)}:${s.port || 22}</td>
      <td>${s.platform === 'windows' ? 'Windows' : 'Linux'}</td>
      <td>${esc(s.category || '')}</td>
      <td>${esc(s.required_ssid || '')}</td>
      <td>${latencyFor(s)}</td>
      <td class="muted small">${esc(noteFor(s))}</td>
      <td class="act">
        <button data-copy="${esc(s.name)}">复制给 AI</button>
        <button data-edit="${esc(s.name)}">编辑</button>
      </td>
    </tr>`).join('');

  $('#empty').classList.toggle('hidden', list.length > 0);
}

function updateGate() {
  const fresh = !S.has_vault;
  $('#gate-title').textContent = fresh ? '创建 vault' : '解锁';
  $('#gate-hint').textContent = fresh
    ? '第一次运行。请设置一个主密码 —— 所有服务器信息会用 AES-256-GCM 加密后保存在本地文件里。'
    : '输入主密码以解锁。';
  $('#gate-pw2').classList.toggle('hidden', !fresh);
  $('#gate-remember-wrap').classList.toggle('hidden', false);
  $('#gate-go').textContent = fresh ? '创建' : '解锁';
  $('#gate-path').textContent = 'vault 位置：' + (S.vault_path || '');
}

/* ───────── 流程 ───────── */

async function loadState() {
  S = await api('/api/state');
  render();
}

async function submitGate() {
  const pw = $('#gate-pw').value;
  const pw2 = $('#gate-pw2').value;
  const remember = $('#gate-remember').checked;
  const fresh = !S.has_vault;
  const errEl = $('#gate-err');
  errEl.textContent = '';

  if (!pw) { errEl.textContent = '请输入主密码'; return; }
  if (fresh && pw !== pw2) { errEl.textContent = '两次输入不一致'; return; }

  const btn = $('#gate-go');
  btn.disabled = true;
  try {
    await api(fresh ? '/api/init' : '/api/unlock', { password: pw, remember });
    $('#gate-pw').value = '';
    $('#gate-pw2').value = '';
    await loadState();
    await runTest();
  } catch (e) {
    errEl.textContent = e.message;
  } finally {
    btn.disabled = false;
  }
}

async function runTest() {
  if (!S.unlocked) return;
  const btn = $('#btn-test');
  btn.disabled = true;
  btn.textContent = '检测中…';
  try {
    const r = await api('/api/test', {});
    S.ssid = r.ssid;
    S.results = r.results || {};
    render();

    const all = Object.values(S.results);
    const ok = all.filter((x) => x.state === 'ok').length;
    const wrongNet = all.filter((x) => x.state === 'wrong_net').length;
    let msg = `${ok}/${all.length} 可连接`;
    if (wrongNet) msg += `，${wrongNet} 台网络环境不满足`;
    toast('检测完成：' + msg, 'ok');
  } catch (e) {
    toast('检测失败：' + e.message, 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = '重新检测';
  }
}

async function copyOne(name) {
  try {
    const r = await api('/api/snippet?name=' + encodeURIComponent(name));
    const ok = await copyText(r.text);
    toast(ok ? `已复制 ${name} 的连接说明，直接粘给 AI 即可` : '复制失败，请手动复制', ok ? 'ok' : 'error');
  } catch (e) {
    toast('生成失败：' + e.message, 'error');
  }
}

async function copyAll() {
  try {
    const r = await api('/api/snippet');
    const ok = await copyText(r.text);
    toast(ok ? '已复制全部服务器的索引，直接粘给 AI 即可' : '复制失败，请手动复制', ok ? 'ok' : 'error');
  } catch (e) {
    toast('生成失败：' + e.message, 'error');
  }
}

async function lockNow() {
  try {
    await api('/api/lock', {});
  } catch { /* 忽略 */ }
  S.unlocked = false;
  S.results = {};
  render();
}

async function quit() {
  if (!confirm('退出后本地界面服务会停止。\n如果 AI 还要用 srvctl 命令，可以留着不退出。\n\n确定退出？')) return;
  try { await api('/api/quit', {}); } catch { /* 服务已停 */ }
  document.body.innerHTML =
    '<div class="gate"><div class="card"><h1>已退出</h1>' +
    '<p class="muted">这个标签页可以关掉了。</p></div></div>';
}

/* ───────── 编辑器 ───────── */

function formEls() { return $('#form').elements; }

function syncAuthFields() {
  const mode = formEls()['auth_mode'].value;
  document.querySelectorAll('#form [data-when]').forEach((el) => {
    el.classList.toggle('hidden', el.dataset.when !== mode);
  });
}

function openEditor(name) {
  editingName = name || null;
  const srv = name ? S.servers.find((x) => x.name === name) : null;
  const el = formEls();

  $('#ed-title').textContent = srv ? '编辑：' + srv.name : '新增服务器';
  $('#ed-delete').classList.toggle('hidden', !srv);
  $('#ed-err').textContent = '';

  el['name'].value = srv ? srv.name : '';
  el['host'].value = srv ? (srv.host || '') : '';
  el['port'].value = srv && srv.port ? srv.port : '';
  el['platform'].value = srv ? (srv.platform || 'linux') : 'linux';
  el['username'].value = srv ? (srv.username || '') : '';
  el['auth_mode'].value = srv ? (srv.auth_mode || 'password') : 'password';
  el['password'].value = srv ? (srv.password || '') : '';
  el['private_key'].value = srv ? (srv.private_key || '') : '';
  el['passphrase'].value = srv ? (srv.passphrase || '') : '';
  el['category'].value = srv ? (srv.category || '') : '';
  el['tags'].value = srv ? (srv.tags || []).join(', ') : '';
  el['required_ssid'].value = srv ? (srv.required_ssid || '') : '';
  el['check_mode'].value = srv
    ? (srv.check_mode || 'auto')
    : 'auto';
  el['notes'].value = srv ? (srv.notes || '') : '';

  syncAuthFields();
  $('#editor').showModal();
  el['name'].focus();
}

function collect() {
  const el = formEls();
  return {
    name: el['name'].value.trim(),
    host: el['host'].value.trim(),
    port: parseInt(el['port'].value, 10) || 0,
    platform: el['platform'].value,
    username: el['username'].value.trim(),
    auth_mode: el['auth_mode'].value,
    password: el['password'].value,
    private_key: el['private_key'].value,
    passphrase: el['passphrase'].value,
    category: el['category'].value.trim(),
    tags: el['tags'].value.split(',').map((x) => x.trim()).filter(Boolean),
    required_ssid: el['required_ssid'].value.trim(),
    check_mode: el['check_mode'].value,
    notes: el['notes'].value,
  };
}

async function saveEditor() {
  const errEl = $('#ed-err');
  errEl.textContent = '';

  const srv = collect();
  if (!srv.name) return void (errEl.textContent = '名称不能为空');
  if (!srv.host) return void (errEl.textContent = '地址不能为空');
  if (srv.auth_mode === 'password' && !srv.password) return void (errEl.textContent = '密码不能为空');
  if (srv.auth_mode === 'key' && !srv.private_key.trim()) return void (errEl.textContent = '私钥不能为空');

  // 改名 = 删旧 + 存新
  if (editingName && editingName !== srv.name) {
    try { await api('/api/delete?name=' + encodeURIComponent(editingName), {}); }
    catch { /* 旧记录可能已不存在 */ }
  }

  try {
    await api('/api/servers', srv);
    $('#editor').close();
    await loadState();
    toast('已保存：' + srv.name, 'ok');
  } catch (e) {
    errEl.textContent = e.message;
  }
}

async function deleteCurrent() {
  if (!editingName) return;
  const name = editingName;
  if (!confirm(`确定删除「${name}」？此操作不可撤销。`)) return;
  try {
    await api('/api/delete?name=' + encodeURIComponent(name), {});
    $('#editor').close();
    await loadState();
    toast('已删除：' + name, 'ok');
  } catch (e) {
    $('#ed-err').textContent = e.message;
  }
}

/* ───────── 事件绑定 ───────── */

function wire() {
  $('#gate-go').addEventListener('click', submitGate);
  ['#gate-pw', '#gate-pw2'].forEach((sel) => {
    $(sel).addEventListener('keydown', (e) => { if (e.key === 'Enter') submitGate(); });
  });

  $('#btn-add').addEventListener('click', () => openEditor(null));
  $('#btn-test').addEventListener('click', runTest);
  $('#btn-copy-all').addEventListener('click', copyAll);
  $('#btn-lock').addEventListener('click', lockNow);
  $('#btn-quit').addEventListener('click', quit);

  $('#rows').addEventListener('click', (e) => {
    const btn = e.target.closest('button');
    if (!btn) return;
    if (btn.dataset.copy) copyOne(btn.dataset.copy);
    else if (btn.dataset.edit) openEditor(btn.dataset.edit);
  });

  $('#ed-save').addEventListener('click', saveEditor);
  $('#ed-cancel').addEventListener('click', () => $('#editor').close());
  $('#ed-delete').addEventListener('click', deleteCurrent);
  $('#form').addEventListener('submit', (e) => e.preventDefault());
  formEls()['auth_mode'].addEventListener('change', syncAuthFields);

  // 新增时切换平台自动带出默认检测策略；编辑时不覆盖用户的选择
  formEls()['platform'].addEventListener('change', (e) => {
    if (editingName) return;
    formEls()['check_mode'].value = e.target.value === 'windows' ? 'none' : 'auto';
  });
}

async function init() {
  wire();
  try {
    await loadState();
    if (S.unlocked) await runTest();
  } catch (e) {
    toast('加载失败：' + e.message, 'error');
  }
}

init();
