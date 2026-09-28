'use strict';

/* Server Manager 前端。纯 vanilla JS —— 没有 npm、没有构建步骤。
   改完直接 go build 就生效。 */

const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = (sel) => document.querySelector(sel);

let S = {
  has_vault: false, unlocked: false, servers: [],
  results: {}, ssid: '', vault_path: '', remembered: false, revision: '',
};
let editingName = null;
let toastTimer = null;
/* 上次看到的 vault 版本标记。变了说明有别的进程写过（AI 用 CLI 动了，
   或者另一台机器同步过来），界面需要重新拉列表。 */
let lastRevision = '';

/* ───────── 断线检测 ─────────
   浏览器对"请求没拿到任何响应"只会给一句 TypeError: Failed to fetch，
   对用户完全没有指导意义。下面把它翻译成能照着做的说明。 */

let offline = false;

function markOffline(why) {
  if (why) $('#offline-why').textContent = why;
  offline = true;
  $('#offline').classList.remove('hidden');
}

function markOnline() {
  if (!offline) return;
  offline = false;
  $('#offline').classList.add('hidden');
}

const OFFLINE_HINT =
  '请重新双击 srvctl-gui.exe，用新打开的页面继续操作。\n' +
  '（每次启动端口和令牌都会重新生成，所以旧标签页无法继续使用）';

/* 定期探活 + 变更检测。
   - 程序退出 / 令牌失效 → 顶部横幅
   - vault 被别的进程改过 → 自动重新拉列表并静默重测 */
let pollBusy = false;

async function pollAlive() {
  if (document.hidden || pollBusy) return;
  pollBusy = true;
  try {
    const res = await fetch('/api/ping', { headers: { 'X-Srvctl-Token': TOKEN } });

    if (res.status === 403) {
      markOffline('页面令牌已失效 —— 程序可能重启过。');
      return;
    }
    if (!res.ok) {
      markOffline('本地服务返回了异常状态。');
      return;
    }

    const p = await res.json();
    markOnline();

    // 锁定状态在别处变了（比如另一个标签页点了锁定）
    if (!!p.unlocked !== !!S.unlocked) {
      await loadState();
      if (S.unlocked) await runTest({ silent: true });
      return;
    }

    // vault 被改过 —— 典型场景：AI 通过 CLI 加了一台服务器
    if (p.revision && p.revision !== lastRevision) {
      await refreshFromVault();
    }
  } catch {
    markOffline('本地服务没有响应 —— 程序可能已退出。');
  } finally {
    pollBusy = false;
  }
}

/* ───────── API ───────── */

async function api(path, body) {
  const opts = { headers: { 'X-Srvctl-Token': TOKEN } };
  if (body !== undefined) {
    opts.method = 'POST';
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(path, opts);
  } catch {
    // fetch 抛异常 = 连接层就失败了，拿不到任何 HTTP 状态码
    markOffline('请求没有到达本地服务。');
    throw new Error('与本地服务断开连接，请求没有发出去。\n' + OFFLINE_HINT);
  }

  if (res.status === 403) {
    markOffline('页面令牌已失效 —— 程序可能重启过。');
    throw new Error('页面令牌已失效。\n' + OFFLINE_HINT);
  }

  markOnline();

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

/* 与 Go 端 model.Server.EffectivePort() 保持一致：
   端口留空表示"用平台默认值" —— Linux 走 SSH(22)，Windows 走 RDP(3389)。
   不能一律按 22 处理，否则 Windows 服务器会显示成 :22，是错的。 */
function effectivePort(srv) {
  if (srv.port > 0) return srv.port;
  return srv.platform === 'windows' ? 3389 : 22;
}

/* 后端理论上永远返回数组，但一个 null 就能让整个界面白屏
   （"S.servers is not iterable"），而且会连带把"重新检测"也一起弄挂。
   这里兜一层，UI 不该因为一个字段类型不符就整个不可用。 */
function asArray(v) {
  return Array.isArray(v) ? v : [];
}

/* ───────── 分类下拉 ─────────
   候选 = 内置常用值 + 你已经用过的所有分类 + 「＋ 自定义…」

   为什么用 <select> 而不是 <input list="...">（datalist）：
   datalist 在不少浏览器里要等你开始打字才显示候选，外观又和普通文本框
   一模一样，用户会以为这里没得选 —— 这正是用户反馈的问题。
   select 有原生下拉箭头，一眼就知道能点。自定义需求用额外一个输入框兜住，
   所以既好发现又不失灵活。 */
const DEFAULT_CATEGORIES = ['公网', '内网', '测试', '生产'];
const CAT_CUSTOM = '__custom__';
let lastCategoryKey = '';

function categoryOptions() {
  const used = asArray(S.servers)
    .map((s) => (s.category || '').trim())
    .filter(Boolean);
  return [...new Set([...used, ...DEFAULT_CATEGORIES])]
    .sort((a, b) => a.localeCompare(b, 'zh'));
}

function updateCategoryList() {
  const sel = document.getElementById('cat-pick');
  if (!sel) return;

  const opts = categoryOptions();
  const key = opts.join('\u0000');
  if (key === lastCategoryKey) return; // 没变就别重建，免得把用户正在选的项冲掉
  lastCategoryKey = key;

  const keep = sel.value;
  sel.innerHTML =
    '<option value="">（不填）</option>' +
    opts.map((c) => `<option value="${esc(c)}">${esc(c)}</option>`).join('') +
    `<option value="${CAT_CUSTOM}">＋ 自定义…</option>`;

  const valid = keep === CAT_CUSTOM || opts.includes(keep);
  sel.value = valid ? keep : '';
}

/* 选中「自定义」时才露出输入框 */
function syncCategoryFields() {
  const sel = document.getElementById('cat-pick');
  const wrap = document.getElementById('cat-custom-wrap');
  if (!sel || !wrap) return;
  wrap.classList.toggle('hidden', sel.value !== CAT_CUSTOM);
}

/* 文本框中填了内容就优先用它，否则用下拉选中的值 */
function currentCategory() {
  const sel = document.getElementById('cat-pick');
  if (!sel) return '';
  if (sel.value === CAT_CUSTOM) {
    return document.getElementById('cat-custom').value.trim();
  }
  return sel.value;
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

  updateCategoryList();

  const list = asArray(S.servers).slice().sort((a, b) => {
    const c = (a.category || '').localeCompare(b.category || '', 'zh');
    return c !== 0 ? c : a.name.localeCompare(b.name, 'zh');
  });

  $('#count').textContent = list.length ? `共 ${list.length} 台` : '';

  $('#rows').innerHTML = list.map((s) => `
    <tr>
      <td>${dotFor(s)}</td>
      <td class="name">
        <div>${esc(s.name)}</div>
        ${s.notes ? `<div class="sub" title="${esc(s.notes)}">${esc(s.notes)}</div>` : ''}
      </td>
      <td class="mono">${esc(s.host)}:${effectivePort(s)}</td>
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

  // 配置了 vault 路径（比如云盘共享目录）但文件不在 —— 多半是同步还没下来。
  // 这时绝不能顺手引导用户"创建"：新建的是一个空库，同步回去有可能
  // 覆盖掉别的机器上的数据。
  const sharedMissing = fresh && !!S.vault_configured;

  if (sharedMissing) {
    $('#gate-title').textContent = '找不到配置的 vault';
    $('#gate-hint').textContent =
      '这个路径下暂时没有 vault 文件。\n\n' +
      '如果它在云盘目录里，通常是同步还没完成 —— 稍等一会儿再打开本页。\n\n' +
      '⚠️ 请不要在这里新建：那会生成一个空库，同步回去有可能覆盖掉已有数据。';
    $('#gate-go').textContent = '仍要在此新建';
  } else {
    $('#gate-title').textContent = fresh ? '创建 vault' : '解锁';
    $('#gate-hint').textContent = fresh
      ? '第一次运行。请设置一个主密码 —— 所有服务器信息会用 AES-256-GCM 加密后保存在本地文件里。'
      : '输入主密码以解锁。';
    $('#gate-go').textContent = fresh ? '创建' : '解锁';
  }

  $('#gate-pw2').classList.toggle('hidden', !fresh);
  $('#gate-remember-wrap').classList.toggle('hidden', false);
  $('#gate-path').textContent = 'vault 位置：' + (S.vault_path || '');
}

/* ───────── 流程 ───────── */

async function loadState() {
  S = await api('/api/state');
  lastRevision = S.revision || '';
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

  // 配置的共享 vault 不存在时，再确认一次 —— 这个操作有可能覆盖别人的数据
  if (fresh && S.vault_configured) {
    const ok = confirm(
      '这里配置的是一个共享位置的 vault，但文件不存在。\n\n' +
      '现在新建会生成一个空库。如果它同步回其它机器，可能覆盖已有服务器数据。\n\n' +
      '确定要新建吗？'
    );
    if (!ok) return;
  }

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

async function runTest(opts = {}) {
  if (!S.unlocked) return;

  // silent: 后台刷新用，不闪按钮、不弹成功提示
  // only:   只测指定的那一条（保存后用它，免得改一台就全量重测）
  const silent = opts.silent === true;
  const only = opts.only || '';
  const btn = $('#btn-test');

  if (!silent) {
    btn.disabled = true;
    btn.textContent = '检测中…';
  }
  try {
    const path = '/api/test' + (only ? '?name=' + encodeURIComponent(only) : '');
    const r = await api(path, {});
    S.ssid = r.ssid;
    S.results = r.results || {};
    render();

    if (!silent) {
      const all = Object.values(S.results);
      const ok = all.filter((x) => x.state === 'ok').length;
      const wrongNet = all.filter((x) => x.state === 'wrong_net').length;
      let msg = `${ok}/${all.length} 可连接`;
      if (wrongNet) msg += `，${wrongNet} 台网络环境不满足`;
      toast('检测完成：' + msg, 'ok');
    }
  } catch (e) {
    if (!silent) toast('检测失败：' + e.message, 'error');
    else markOffline('后台刷新失败：' + e.message);
  } finally {
    if (!silent) {
      btn.disabled = false;
      btn.textContent = '重新检测';
    }
  }
}

/* vault 变了：重拉列表；如果确实变了，再静默测一遍让新条目立刻有状态 */
async function refreshFromVault() {
  const before = lastRevision;
  await loadState();
  if (lastRevision === before) return; // 没真的变，不白花 2 秒
  await runTest({ silent: true });
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

/* ───────── 密码可见性切换 ─────────
   给页面上每一个 password 输入框自动挂一个小眼睛。
   用自动挂载而不是逐个写标签，是为了让主密码框、服务器密码框、
   私钥口令框全都一致 —— 少写一堆重复的 HTML。 */

const EYE_OPEN =
  '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"' +
  ' stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>' +
  '<circle cx="12" cy="12" r="3"/></svg>';

const EYE_OFF =
  '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"' +
  ' stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/>' +
  '<path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19"/>' +
  '<path d="M14.12 14.12a3 3 0 1 1-4.24-4.24"/>' +
  '<line x1="1" y1="1" x2="23" y2="23"/></svg>';

function addEyeToggles() {
  document.querySelectorAll('input[type="password"]').forEach((input) => {
    if (input.dataset.eyeDone) return;
    input.dataset.eyeDone = '1';

    const wrap = document.createElement('span');
    wrap.className = 'pw-wrap';
    input.parentNode.insertBefore(wrap, input);
    wrap.appendChild(input);

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'eye';
    btn.tabIndex = -1;
    btn.innerHTML = EYE_OPEN;
    btn.title = '显示';
    btn.setAttribute('aria-label', '显示密码');

    btn.addEventListener('click', () => {
      const show = input.type === 'password';
      input.type = show ? 'text' : 'password';
      btn.innerHTML = show ? EYE_OFF : EYE_OPEN;
      btn.classList.toggle('on', show);
      btn.title = show ? '隐藏' : '显示';
      btn.setAttribute('aria-label', show ? '隐藏密码' : '显示密码');
    });

    wrap.appendChild(btn);
  });
}

function syncAuthFields() {
  const mode = formEls()['auth_mode'].value;
  document.querySelectorAll('#form [data-when]').forEach((el) => {
    el.classList.toggle('hidden', el.dataset.when !== mode);
  });
}

function openEditor(name) {
  editingName = name || null;
  const srv = name ? asArray(S.servers).find((x) => x.name === name) : null;
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
  // 分类：能匹配到候选就直接选中，否则走「自定义」
  const cat = (srv ? (srv.category || '') : '').trim();
  const catOpts = categoryOptions();
  const pick = document.getElementById('cat-pick');
  const catCustom = document.getElementById('cat-custom');
  if (cat && catOpts.includes(cat)) {
    pick.value = cat;
    catCustom.value = '';
  } else if (cat) {
    pick.value = CAT_CUSTOM;
    catCustom.value = cat;
  } else {
    pick.value = '';
    catCustom.value = '';
  }
  syncCategoryFields();

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
    category: currentCategory(),
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
    // 只重测这一条 —— 改了一台机器没必要把全部重新拨一遍
    await runTest({ silent: true, only: srv.name });
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
  addEyeToggles();

  $('#gate-go').addEventListener('click', submitGate);
  ['#gate-pw', '#gate-pw2'].forEach((sel) => {
    $(sel).addEventListener('keydown', (e) => { if (e.key === 'Enter') submitGate(); });
  });

  $('#btn-add').addEventListener('click', () => openEditor(null));
  $('#btn-test').addEventListener('click', () => runTest());
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

  document.getElementById('cat-pick').addEventListener('change', (e) => {
    syncCategoryFields();
    // 只有用户主动选「自定义」时才抢焦点 —— openEditor 里也会调用
    // syncCategoryFields，那时该聚焦的是名称框。
    if (e.target.value === CAT_CUSTOM) {
      document.getElementById('cat-custom').focus();
    }
  });

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
    // 断线时 api() 已经把横幅显示出来了，这里不再叠一个 toast
    if (!offline) toast('加载失败：' + e.message, 'error');
  }
  setInterval(pollAlive, 4000);
}

init();
