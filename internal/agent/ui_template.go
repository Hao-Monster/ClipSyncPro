package agent

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ClipSync Pro</title>
<style>
:root {
  color-scheme: light;
  --bg: #f7f8fa;
  --panel: #ffffff;
  --line: #d8dee8;
  --text: #18202a;
  --muted: #667085;
  --accent: #0f766e;
  --danger: #b42318;
  --focus: #2563eb;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: "Segoe UI", "Microsoft YaHei", Arial, sans-serif;
  background: var(--bg);
  color: var(--text);
}
.app {
  min-height: 100vh;
  display: grid;
  grid-template-rows: auto auto 1fr auto;
}
header {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--line);
  background: var(--panel);
}
.brand {
  font-size: 17px;
  font-weight: 650;
  min-width: 136px;
}
#search {
  flex: 1;
  min-width: 120px;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 0 12px;
  font-size: 14px;
}
#search:focus { outline: 2px solid rgba(37, 99, 235, .18); border-color: var(--focus); }
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--line);
  background: #eef2f7;
  overflow-x: auto;
}
button {
  height: 32px;
  border: 1px solid var(--line);
  background: var(--panel);
  color: var(--text);
  border-radius: 6px;
  padding: 0 10px;
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
}
button:hover { border-color: #9aa7b8; }
button.danger { color: var(--danger); border-color: #f1b8b3; }
button.active { border-color: var(--accent); color: var(--accent); font-weight: 650; }
main { overflow: auto; }
table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
  background: var(--panel);
}
th, td {
  border-bottom: 1px solid var(--line);
  padding: 9px 10px;
  font-size: 13px;
  vertical-align: middle;
}
th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: #f2f5f9;
  text-align: left;
  color: #475467;
  font-weight: 650;
}
tr.selected { background: #e8f3ff; }
tr:hover td { background: #f8fafc; }
.idx { width: 56px; text-align: right; color: var(--muted); }
.check { width: 38px; text-align: center; }
.content { width: auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.time { width: 146px; color: var(--muted); }
.count { width: 74px; text-align: right; color: var(--muted); }
.status { width: 88px; color: var(--muted); }
.actions { width: 120px; text-align: right; }
.tag {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 5px;
  border: 1px solid #bdd7d2;
  border-radius: 4px;
  color: var(--accent);
  font-size: 12px;
}
footer {
  min-height: 34px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 16px;
  border-top: 1px solid var(--line);
  background: var(--panel);
  color: var(--muted);
  font-size: 12px;
}
.empty {
  padding: 36px 16px;
  text-align: center;
  color: var(--muted);
}
@media (max-width: 720px) {
  header { height: auto; flex-wrap: wrap; }
  .brand { width: 100%; }
  .time, .status { display: none; }
  .actions { width: 84px; }
  .count { width: 58px; }
}
</style>
</head>
<body>
<div class="app">
  <header>
    <div class="brand">ClipSync Pro</div>
    <input id="search" autocomplete="off" placeholder="搜索">
  </header>
  <nav class="toolbar">
    <button id="allBtn" class="active" title="全部">全部</button>
    <button id="sensitiveBtn" title="账户密码">账户密码</button>
    <button id="batchBtn" title="批量删除">批量删除</button>
    <button id="dateBtn" title="按时间筛选">按时间筛选</button>
    <button id="deleteBtn" class="danger" title="删除">删除</button>
    <button id="passwordBtn" title="密码">密码</button>
    <button id="accountBtn" title="账户">账户</button>
  </nav>
  <main>
    <table>
      <thead>
        <tr>
          <th class="check"></th>
          <th class="idx">序号</th>
          <th>内容</th>
          <th class="time">时间</th>
          <th class="count">字符</th>
          <th class="status">同步</th>
          <th class="actions"></th>
        </tr>
      </thead>
      <tbody id="rows"></tbody>
    </table>
    <div id="empty" class="empty" hidden>暂无记录</div>
  </main>
  <footer>
    <div id="state">未选择</div>
    <div id="count">0 条</div>
  </footer>
</div>
<script>
const state = {
  keyword: "",
  date: "",
  sensitive: false,
  batch: false,
  selected: "",
  selectedSet: new Set(),
  records: [],
  timer: 0
};

const $ = id => document.getElementById(id);
const rows = $("rows");

function qs() {
  const p = new URLSearchParams();
  if (state.keyword) p.set("keyword", state.keyword);
  if (state.date) p.set("date", state.date);
  if (state.sensitive) p.set("sensitive", "true");
  return p.toString();
}

async function load() {
  const res = await fetch("/api/records?" + qs());
  const data = await res.json();
  if (!res.ok) {
    alert(data.error || "请求失败");
    return;
  }
  state.records = data.records || [];
  render();
}

function render() {
  rows.innerHTML = "";
  $("empty").hidden = state.records.length !== 0;
  state.records.forEach((rec, idx) => {
    const tr = document.createElement("tr");
    tr.dataset.id = rec.id;
    if (state.selected === rec.id) tr.classList.add("selected");
    tr.addEventListener("click", () => select(rec.id));
    tr.addEventListener("dblclick", () => paste(rec));
    const checked = state.selectedSet.has(rec.id) ? "checked" : "";
    const sensitive = rec.is_sensitive ? "<span class=\"tag\">敏感</span>" : "";
    const action = rec.is_sensitive ? "<button data-action=\"view\">查看</button>" : "";
    const checkbox = state.batch ? "<input type=\"checkbox\" " + checked + ">" : "";
    tr.innerHTML =
      "<td class=\"check\">" + checkbox + "</td>" +
      "<td class=\"idx\">" + (idx + 1) + "</td>" +
      "<td class=\"content\" title=\"" + escapeAttr(rec.summary || "") + "\">" + escapeHTML(rec.summary || "") + sensitive + "</td>" +
      "<td class=\"time\">" + (rec.display_time || "") + "</td>" +
      "<td class=\"count\">" + (rec.char_count || 0) + "</td>" +
      "<td class=\"status\">" + (rec.sync_status || "") + "</td>" +
      "<td class=\"actions\">" + action + "</td>";
    const checkboxEl = tr.querySelector("input");
    if (checkboxEl) {
      checkboxEl.addEventListener("click", ev => {
        ev.stopPropagation();
        if (checkboxEl.checked) state.selectedSet.add(rec.id);
        else state.selectedSet.delete(rec.id);
        updateFooter();
      });
    }
    const viewBtn = tr.querySelector("[data-action=\"view\"]");
    if (viewBtn) {
      viewBtn.addEventListener("click", ev => {
        ev.stopPropagation();
        reveal(rec.id);
      });
    }
    rows.appendChild(tr);
  });
  updateFooter();
}

function select(id) {
  state.selected = id;
  render();
}

function selectedRecord() {
  return state.records.find(r => r.id === state.selected) || null;
}

function updateFooter() {
  const rec = selectedRecord();
  $("state").textContent = rec ? "已选择 " + (rec.display_time || "") : "未选择";
  $("count").textContent = state.records.length + " 条";
  $("allBtn").classList.toggle("active", !state.sensitive && !state.date && !state.keyword);
  $("sensitiveBtn").classList.toggle("active", state.sensitive);
  $("batchBtn").classList.toggle("active", state.batch);
}

async function reveal(id) {
  const res = await fetch("/api/records/" + encodeURIComponent(id) + "/reveal", { method: "POST" });
  const data = await res.json();
  if (!res.ok) {
    alert(data.error || "查看失败");
    return;
  }
  await load();
}

async function deleteSelected() {
  const ids = state.batch ? Array.from(state.selectedSet) : (state.selected ? [state.selected] : []);
  if (!ids.length) return;
  const res = await fetch("/api/records/batch-delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ids: ids })
  });
  const data = await res.json();
  if (!res.ok) {
    alert(data.error || "删除失败");
    return;
  }
  state.selected = "";
  state.selectedSet.clear();
  await load();
}

async function markPassword() {
  const rec = selectedRecord();
  if (!rec) return;
  const account = prompt("账户", "");
  if (account === null) return;
  const remark = prompt("备注", "") || "";
  await postJSON("/api/records/" + encodeURIComponent(rec.id) + "/mark-password", { account: account, remark: remark });
  await load();
}

async function markAccount() {
  const rec = selectedRecord();
  if (!rec) return;
  const password = prompt("密码", "");
  if (password === null) return;
  const remark = prompt("备注", "") || "";
  await postJSON("/api/records/" + encodeURIComponent(rec.id) + "/mark-account", { password: password, remark: remark });
  await load();
}

async function copy(rec) {
  if (!rec) return;
  const field = rec.is_sensitive ? chooseField() : "content";
  if (!field) return;
  await postJSON("/api/records/" + encodeURIComponent(rec.id) + "/copy", { field: field });
}

async function paste(rec) {
  if (!rec) return;
  const field = rec.is_sensitive ? chooseField() : "content";
  if (!field) return;
  await postJSON("/api/records/" + encodeURIComponent(rec.id) + "/paste", { field: field });
}

function chooseField() {
  const value = prompt("字段", "password");
  if (value === null) return "";
  return value;
}

async function postJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {})
  });
  const data = await res.json();
  if (!res.ok) {
    alert(data.error || "操作失败");
    throw new Error(data.error || "request failed");
  }
  return data;
}

function debounceLoad() {
  clearTimeout(state.timer);
  state.timer = setTimeout(load, 120);
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" }[ch]));
}

function escapeAttr(s) {
  return escapeHTML(s).replace(/\x60/g, "&#96;");
}

$("search").addEventListener("input", ev => {
  state.keyword = ev.target.value;
  debounceLoad();
});
$("allBtn").addEventListener("click", () => {
  state.keyword = "";
  state.date = "";
  state.sensitive = false;
  $("search").value = "";
  load();
});
$("sensitiveBtn").addEventListener("click", () => {
  state.sensitive = !state.sensitive;
  load();
});
$("batchBtn").addEventListener("click", () => {
  state.batch = !state.batch;
  state.selectedSet.clear();
  render();
});
$("dateBtn").addEventListener("click", async () => {
  const value = prompt("yyyyMMdd", state.date || "");
  if (value === null) return;
  state.date = value.trim();
  await load();
});
$("deleteBtn").addEventListener("click", deleteSelected);
$("passwordBtn").addEventListener("click", markPassword);
$("accountBtn").addEventListener("click", markAccount);
document.addEventListener("keydown", ev => {
  if (ev.key === "Enter") {
    const rec = selectedRecord();
    if (rec) copy(rec);
  }
});

load();
setInterval(load, 4000);
</script>
</body>
</html>`
