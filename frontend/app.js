"use strict";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

async function api(path, opts = {}) {
  const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...opts });
  if (res.status === 401) {
    showLogin();
    throw new Error("인증이 필요합니다");
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).detail || msg; } catch (_) {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

const fmtPct = (v) => (v == null ? "—" : v.toFixed(2) + "%");
const fmtMs = (v) => (v == null ? "—" : Math.round(v) + " ms");
const fmtMbps = (v) => (v == null ? "—" : v.toFixed(1) + " Mbps");
function fmtDuration(s) {
  if (s == null) return "—";
  if (s < 60) return s + "초";
  if (s < 3600) return Math.floor(s / 60) + "분";
  if (s < 86400) return (s / 3600).toFixed(1) + "시간";
  return (s / 86400).toFixed(1) + "일";
}
function fmtTime(iso) { return iso ? new Date(iso).toLocaleString("ko-KR", { hour12: false }) : "—"; }
function fmtBytes(n) {
  if (n == null) return "—";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"]; let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(1) + " " + u[i];
}
const TYPE_LABELS = { router: "공유기", nas: "NAS", server: "서버", pc: "PC", ap: "AP", printer: "프린터", iot: "IoT", other: "기타" };

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------
function showLogin() { $("#loginOverlay").hidden = false; }
function hideLogin() { $("#loginOverlay").hidden = true; }

async function checkAuth() {
  try {
    const st = await api("/api/auth/state");
    // Only block when login is actually enforced (enabled AND an account exists).
    if (st.auth_required && !st.authenticated) { showLogin(); return false; }
    hideLogin();
    $("#logoutBtn").hidden = !st.authenticated;
    if (st.auth_enabled && !st.has_user) {
      $("#globalStatus").textContent = "로그인 켜짐 · 계정 없음 → [보안]에서 계정 등록 필요";
    }
    return true;
  } catch (_) { return false; }
}

$("#loginForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const msg = $("#loginMsg"); msg.textContent = "";
  const fd = new FormData(e.target);
  try {
    await api("/api/auth/login", { method: "POST", body: JSON.stringify({ username: fd.get("username"), code: fd.get("code") }) });
    hideLogin();
    boot();
  } catch (err) { msg.textContent = err.message; }
});
$("#logoutBtn").addEventListener("click", async () => {
  await api("/api/auth/logout", { method: "POST" });
  showLogin();
});

// ---------------------------------------------------------------------------
// Tabs + charts
// ---------------------------------------------------------------------------
const loaders = {};
$$(".tab").forEach((tab) => tab.addEventListener("click", () => {
  $$(".tab").forEach((t) => t.classList.remove("active"));
  $$(".page").forEach((p) => p.classList.remove("active"));
  tab.classList.add("active");
  $("#" + tab.dataset.tab).classList.add("active");
  if (loaders[tab.dataset.tab]) loaders[tab.dataset.tab]();
}));

const charts = {};
function drawChart(id, config) {
  const ctx = document.getElementById(id);
  if (!ctx) return;
  if (charts[id]) charts[id].destroy();
  charts[id] = new Chart(ctx, config);
}
const CHART_BASE = {
  responsive: true, maintainAspectRatio: false,
  interaction: { mode: "index", intersect: false },
  scales: { x: { ticks: { color: "#8aa0c0", maxTicksLimit: 8 }, grid: { color: "#2a3958" } }, y: { ticks: { color: "#8aa0c0" }, grid: { color: "#2a3958" } } },
  plugins: { legend: { labels: { color: "#e6ecf5" } } },
};

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------
async function loadDashboard() {
  try {
    const data = await api("/api/devices/overview");
    $("#globalStatus").textContent = `정상 ${data.up} / 전체 ${data.total}`;
    $("#summaryCards").innerHTML = `
      <div class="card"><div class="label">전체 장비</div><div class="value">${data.total}</div></div>
      <div class="card"><div class="label">정상</div><div class="value" style="color:var(--up)">${data.up}</div></div>
      <div class="card"><div class="label">다운</div><div class="value" style="color:var(--down)">${data.down}</div></div>`;
    $("#deviceCards").innerHTML = data.devices.map((d) => {
      const cls = d.last_status === true ? "up" : d.last_status === false ? "down" : "";
      const t = d.last_status === true ? "정상" : d.last_status === false ? "다운" : "확인 안됨";
      return `<div class="device-card ${cls}"><div class="dname">${d.name}<span class="dot ${cls}"></span></div>
        <div class="dhost">${d.host} · ${TYPE_LABELS[d.type] || d.type}</div>
        <div class="dmeta"><span>${t} · ${fmtMs(d.last_latency_ms)}</span><span>24h ${fmtPct(d.uptime_24h)}</span></div></div>`;
    }).join("") || '<p class="muted">등록된 장비가 없습니다. [장비 관리]에서 추가하세요.</p>';
    const ev = await api("/api/events?limit=15");
    $("#eventsTable tbody").innerHTML = ev.map((e) => `<tr><td>${fmtTime(e.ts)}</td><td>${e.device_name}</td>
      <td><span class="badge ${e.is_up ? "up" : "down"}">${e.is_up ? "복구" : "다운"}</span></td><td>${fmtDuration(e.prev_duration_s)}</td></tr>`).join("")
      || '<tr><td colspan="4" class="muted">이벤트 없음</td></tr>';
  } catch (err) { $("#globalStatus").textContent = "오류: " + err.message; }
}
loaders.dashboard = loadDashboard;

// ---------------------------------------------------------------------------
// 장비 상태 (표 + IP 클릭 → 분/시간/날짜 상세)
// ---------------------------------------------------------------------------
const GRAN_RANGES = { minute: ["1h", "6h", "24h"], hour: ["7d", "30d"], day: ["90d", "1y", "3y"] };
const RANGE_LABEL = { "1h": "1시간", "6h": "6시간", "24h": "24시간", "7d": "7일", "30d": "30일", "90d": "90일", "1y": "1년", "3y": "3년" };
let detailDevice = null, detailGran = "minute", detailRange = "24h";

async function loadStatus() {
  const data = await api("/api/devices/overview");
  $("#statusTable tbody").innerHTML = data.devices.map((d) => {
    const cls = d.last_status === true ? "up" : d.last_status === false ? "down" : "";
    const t = d.last_status === true ? "사용중" : d.last_status === false ? "꺼짐" : "—";
    return `<tr class="clickable" data-id="${d.id}" data-name="${d.name}">
      <td><span class="badge ${cls}">${t}</span></td><td>${d.name}</td>
      <td><a class="iplink" data-id="${d.id}" data-name="${d.name}">${d.host}</a></td>
      <td>${TYPE_LABELS[d.type] || d.type}</td><td>${fmtMs(d.last_latency_ms)}</td>
      <td>${fmtTime(d.last_checked)}</td><td>${fmtPct(d.uptime_24h)}</td></tr>`;
  }).join("") || '<tr><td colspan="7" class="muted">장비가 없습니다.</td></tr>';
  $$("#statusTable .clickable, #statusTable .iplink").forEach((el) =>
    el.addEventListener("click", () => openDetail(el.dataset.id, el.dataset.name)));
}
loaders.status = loadStatus;

function openDetail(id, name) {
  detailDevice = id;
  $("#uptimeDetail").hidden = false;
  $("#detailTitle").textContent = `업타임 상세 — ${name}`;
  setGran(detailGran, true);
  $("#uptimeDetail").scrollIntoView({ behavior: "smooth" });
}

function setGran(gran, force) {
  detailGran = gran;
  $$("#granBtns button").forEach((b) => b.classList.toggle("active", b.dataset.gran === gran));
  const ranges = GRAN_RANGES[gran];
  if (force || !ranges.includes(detailRange)) detailRange = ranges[ranges.length - 1];
  $("#granRanges").innerHTML = ranges.map((r) => `<button data-range="${r}" class="${r === detailRange ? "active" : ""}">${RANGE_LABEL[r]}</button>`).join("");
  $$("#granRanges button").forEach((b) => b.addEventListener("click", () => { detailRange = b.dataset.range; loadDetail(); }));
  loadDetail();
}
$$("#granBtns button").forEach((b) => b.addEventListener("click", () => setGran(b.dataset.gran, true)));

async function loadDetail() {
  if (!detailDevice) return;
  const s = await api(`/api/devices/${detailDevice}/uptime?granularity=${detailGran}&range=${detailRange}`);
  $("#detailStats").innerHTML = `
    <div class="card"><div class="label">가동률</div><div class="value" style="color:var(--up)">${fmtPct(s.uptime_pct)}</div></div>
    <div class="card"><div class="label">평균 지연</div><div class="value small">${fmtMs(s.latency_avg_ms)}</div></div>
    <div class="card"><div class="label">샘플 수</div><div class="value small">${s.samples.toLocaleString()}</div></div>
    <div class="card"><div class="label">단위</div><div class="value small">${detailGran === "minute" ? "분" : detailGran === "hour" ? "시간" : "날짜"}</div></div>`;
  const labels = s.series.map((p) => fmtTime(p.t.length === 10 ? p.t + "T00:00:00Z" : p.t));
  const isRaw = s.source === "raw";
  const up = s.series.map((p) => (isRaw ? (p.up ? 100 : 0) : p.uptime));
  const lat = s.series.map((p) => p.latency);
  drawChart("uptimeChart", { type: "line", data: { labels, datasets: [{ label: "가동률 (%)", data: up, borderColor: "#34d399", backgroundColor: "rgba(52,211,153,.15)", fill: true, stepped: isRaw, pointRadius: 0, tension: 0.2 }] }, options: { ...CHART_BASE, scales: { ...CHART_BASE.scales, y: { ...CHART_BASE.scales.y, min: 0, max: 100 } } } });
  drawChart("latencyChart", { type: "line", data: { labels, datasets: [{ label: "지연시간 (ms)", data: lat, borderColor: "#4f8cff", backgroundColor: "rgba(79,140,255,.12)", fill: true, pointRadius: 0, tension: 0.2 }] }, options: CHART_BASE });
  const ev = await api(`/api/devices/${detailDevice}/events?limit=30`);
  $("#detailEvents tbody").innerHTML = ev.map((e) => `<tr><td>${fmtTime(e.ts)}</td><td><span class="badge ${e.is_up ? "up" : "down"}">${e.is_up ? "복구" : "다운"}</span></td><td>${fmtDuration(e.prev_duration_s)}</td></tr>`).join("") || '<tr><td colspan="3" class="muted">이벤트 없음</td></tr>';
}

// ---------------------------------------------------------------------------
// IP 스캔
// ---------------------------------------------------------------------------
async function loadScan() {
  const d = await api("/api/scan/hosts");
  const lr = d.last_run;
  $("#scanInfo").textContent = `서브넷 ${d.subnet}` + (lr ? ` · 최근 스캔 ${fmtTime(lr.ts)} (${lr.up}/${lr.total} 사용중, 신규 ${lr.new})` : " · 스캔 기록 없음");
  $("#scanTable tbody").innerHTML = d.hosts.map((h) => {
    const cls = h.last_up ? "up" : "down";
    const reg = h.device_id ? '<span class="badge up">등록됨</span>'
      : `<button class="btn small" data-ip="${h.ip}" data-host="${h.hostname || ""}" data-act="reg">장비로 추가</button>`;
    return `<tr><td><span class="badge ${cls}">${h.last_up ? "사용중" : "꺼짐"}</span></td><td>${h.ip}</td>
      <td>${h.mac || "—"}</td><td>${h.hostname || "—"}</td><td>${fmtTime(h.first_seen)}</td>
      <td>${fmtTime(h.last_seen)}</td><td>${h.times_seen}</td><td>${reg}</td></tr>`;
  }).join("") || '<tr><td colspan="8" class="muted">발견된 호스트가 없습니다. [지금 스캔]을 눌러보세요.</td></tr>';
  $$('#scanTable [data-act="reg"]').forEach((b) => b.addEventListener("click", () => {
    openDeviceModal({ host: b.dataset.ip, name: b.dataset.host || b.dataset.ip, type: "other", check_method: "icmp", enabled: true });
  }));
}
loaders.scan = loadScan;
$("#runScan").addEventListener("click", async () => {
  $("#scanInfo").textContent = "스캔 중…";
  try {
    await api("/api/scan/run", { method: "POST" });
    setTimeout(loadScan, 4000);
  } catch (err) { $("#scanInfo").textContent = "오류: " + err.message; }
});

// ---------------------------------------------------------------------------
// 인터넷 속도
// ---------------------------------------------------------------------------
let speedDays = 30;
async function loadSpeed() {
  const [hist, stats] = await Promise.all([api(`/api/speedtest?days=${speedDays}&limit=2000`), api(`/api/speedtest/stats?days=${speedDays}`)]);
  $("#speedStats").innerHTML = `
    <div class="card"><div class="label">평균 다운로드</div><div class="value">${fmtMbps(stats.download_avg)}</div></div>
    <div class="card"><div class="label">평균 업로드</div><div class="value">${fmtMbps(stats.upload_avg)}</div></div>
    <div class="card"><div class="label">최소/최대 다운</div><div class="value small">${fmtMbps(stats.download_min)} / ${fmtMbps(stats.download_max)}</div></div>
    <div class="card"><div class="label">평균 핑</div><div class="value small">${fmtMs(stats.ping_avg)}</div></div>
    <div class="card"><div class="label">측정 횟수</div><div class="value small">${stats.count}</div></div>`;
  const ok = hist.filter((h) => h.ok); const labels = ok.map((h) => fmtTime(h.ts));
  drawChart("speedChart", { type: "line", data: { labels, datasets: [
    { label: "다운로드 (Mbps)", data: ok.map((h) => h.download_mbps), borderColor: "#34d399", backgroundColor: "rgba(52,211,153,.12)", fill: true, pointRadius: 0, tension: 0.25 },
    { label: "업로드 (Mbps)", data: ok.map((h) => h.upload_mbps), borderColor: "#4f8cff", backgroundColor: "rgba(79,140,255,.12)", fill: true, pointRadius: 0, tension: 0.25 }] }, options: CHART_BASE });
  drawChart("pingChart", { type: "line", data: { labels, datasets: [{ label: "핑 (ms)", data: ok.map((h) => h.ping_ms), borderColor: "#fbbf24", backgroundColor: "rgba(251,191,36,.12)", fill: true, pointRadius: 0, tension: 0.25 }] }, options: CHART_BASE });
}
loaders.speed = loadSpeed;
$("#runSpeed").addEventListener("click", async () => {
  const msg = $("#speedMsg"); msg.textContent = "측정 중… (최대 1분)";
  try { await api("/api/speedtest/run", { method: "POST" });
    let n = 0; const t = setInterval(async () => { n++; await loadSpeed(); if (n >= 12) { clearInterval(t); msg.textContent = ""; } }, 5000);
    msg.textContent = "측정을 시작했습니다. 잠시 후 갱신됩니다.";
  } catch (err) { msg.textContent = "오류: " + err.message; }
});
$$("#speedRanges button").forEach((b) => b.addEventListener("click", () => {
  $$("#speedRanges button").forEach((x) => x.classList.remove("active")); b.classList.add("active");
  speedDays = parseInt(b.dataset.days, 10); loadSpeed();
}));

// ---------------------------------------------------------------------------
// 시놀로지 (다중 NAS)
// ---------------------------------------------------------------------------
async function loadSynology() {
  const list = await api("/api/synology/nas");
  if (!list.length) { $("#nasCards").innerHTML = '<p class="muted">등록된 NAS가 없습니다. [+ NAS 추가]를 눌러 등록하세요.</p>'; return; }
  $("#nasCards").innerHTML = list.map((n) => `
    <div class="device-card ${n.last_connected ? "up" : n.last_connected === false ? "down" : ""}" id="nas-${n.id}">
      <div class="dname">${n.name}<span class="dot ${n.last_connected ? "up" : n.last_connected === false ? "down" : ""}"></span></div>
      <div class="dhost">${n.host}:${n.port}${n.has_device_token ? " · 신뢰기기✓" : ""}</div>
      <div class="nas-detail muted" style="margin-top:8px">상태 조회 전</div>
      <div class="toolbar" style="margin-top:10px">
        <button class="btn small" data-id="${n.id}" data-act="status">상태 조회</button>
        <button class="btn small" data-id="${n.id}" data-act="edit">수정</button>
        <button class="btn small danger" data-id="${n.id}" data-act="del">삭제</button>
      </div></div>`).join("");
  $$('#nasCards [data-act]').forEach((b) => b.addEventListener("click", () => nasAction(b.dataset.act, b.dataset.id, list)));
}
loaders.synology = loadSynology;

async function nasAction(act, id, list) {
  if (act === "del") {
    if (!confirm("이 NAS를 삭제할까요?")) return;
    await api(`/api/synology/nas/${id}`, { method: "DELETE" }); loadSynology();
  } else if (act === "edit") {
    openNasModal(list.find((n) => n.id == id));
  } else if (act === "status") {
    const box = $(`#nas-${id} .nas-detail`); box.textContent = "조회 중…";
    try {
      const s = await api(`/api/synology/nas/${id}/status`);
      if (!s.connected) {
        box.innerHTML = `<span style="color:var(--down)">${s.error || "연결 실패"}</span>` +
          (s.otp_required ? '<br>→ [수정]에서 OTP 코드를 입력하세요.' : "");
        return;
      }
      const vols = (s.volumes || []).map((v) => `${v.id || "vol"}: ${v.used_pct != null ? v.used_pct + "%" : "—"} (${fmtBytes(v.used_bytes)}/${fmtBytes(v.total_bytes)})`).join("<br>");
      box.innerHTML = `<div>모델 ${s.model || "—"} · DSM ${s.dsm_version || "—"}</div>
        <div>CPU ${s.cpu_load ?? "—"}% · 메모리 ${s.mem_usage ?? "—"}% · 온도 ${s.temperature_c ?? "—"}°C</div>
        <div>가동시간 ${fmtDuration(s.uptime_s)}</div>${vols ? "<div style='margin-top:4px'>" + vols + "</div>" : ""}`;
    } catch (err) { box.innerHTML = `<span style="color:var(--down)">오류: ${err.message}</span>`; }
  }
}

const nasModal = $("#nasModal"), nasForm = $("#nasForm");
function openNasModal(nas) {
  $("#nasModalTitle").textContent = nas ? "NAS 수정" : "NAS 추가";
  nasForm.reset(); nasForm.id.value = nas ? nas.id : "";
  if (nas) {
    nasForm.name.value = nas.name; nasForm.host.value = nas.host; nasForm.port.value = nas.port;
    nasForm.https.checked = nas.https; nasForm.verify_ssl.checked = nas.verify_ssl;
    nasForm.username.value = nas.username || ""; nasForm.password.value = nas.password === "********" ? "" : (nas.password || "");
    nasForm.enabled.checked = nas.enabled;
  }
  nasModal.classList.add("show");
}
$("#addNas").addEventListener("click", () => openNasModal(null));
$("#cancelNas").addEventListener("click", () => nasModal.classList.remove("show"));
nasModal.addEventListener("click", (e) => { if (e.target === nasModal) nasModal.classList.remove("show"); });
nasForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = nasForm.id.value;
  const p = { name: nasForm.name.value.trim(), host: nasForm.host.value.trim(), port: parseInt(nasForm.port.value || "5001", 10),
    https: nasForm.https.checked, verify_ssl: nasForm.verify_ssl.checked, username: nasForm.username.value.trim(),
    enabled: nasForm.enabled.checked };
  if (nasForm.password.value) p.password = nasForm.password.value;
  if (nasForm.otp_code.value.trim()) p.otp_code = nasForm.otp_code.value.trim();
  try {
    if (id) await api(`/api/synology/nas/${id}`, { method: "PATCH", body: JSON.stringify(p) });
    else await api("/api/synology/nas", { method: "POST", body: JSON.stringify(p) });
    nasModal.classList.remove("show"); loadSynology();
  } catch (err) { alert("저장 실패: " + err.message); }
});

// ---------------------------------------------------------------------------
// 공유기
// ---------------------------------------------------------------------------
async function loadRouter() {
  const body = $("#routerBody");
  try {
    const r = await api("/api/router/status");
    if (!r.enabled) { body.innerHTML = '<p class="muted">공유기가 비활성화되어 있습니다. [설정]에서 켜세요.</p>'; return; }
    const admin = r.admin_url ? `<a href="${r.admin_url}" target="_blank" class="btn small">관리 페이지 열기</a>` : "";
    body.innerHTML = `<div class="kv">
      <div class="item"><div class="k">호스트</div><div class="v" style="font-size:16px">${r.host}</div></div>
      <div class="item"><div class="k">연결</div><div class="v"><span class="badge ${r.reachable ? "up" : "down"}">${r.reachable ? "정상" : "다운"}</span></div></div>
      <div class="item"><div class="k">지연</div><div class="v">${fmtMs(r.latency_ms)}</div></div>
      ${r.uptime_s != null ? `<div class="item"><div class="k">가동시간</div><div class="v" style="font-size:16px">${fmtDuration(r.uptime_s)}</div></div>` : ""}
      ${r.wan_down_mbps != null ? `<div class="item"><div class="k">WAN 다운</div><div class="v">${fmtMbps(r.wan_down_mbps)}</div></div>` : ""}
      ${r.wan_up_mbps != null ? `<div class="item"><div class="k">WAN 업</div><div class="v">${fmtMbps(r.wan_up_mbps)}</div></div>` : ""}</div>
      ${r.sys_descr ? `<p class="muted" style="margin-top:12px">${r.sys_descr}</p>` : ""}
      ${!r.snmp ? '<p class="muted" style="margin-top:12px">SNMP 미사용 — 자세한 정보를 보려면 공유기 SNMP를 켜고 [설정]에서 활성화하세요.</p>' : ""}
      <div style="margin-top:12px">${admin}</div>`;
  } catch (err) { body.innerHTML = `<p class="muted">오류: ${err.message}</p>`; }
}
loaders.routerinfo = loadRouter;
$("#refreshRouter").addEventListener("click", loadRouter);

// ---------------------------------------------------------------------------
// 원격접속 (Tailscale)
// ---------------------------------------------------------------------------
async function loadTailscale() {
  const body = $("#tsBody"); const btn = $("#tsConnect"); btn.hidden = true;
  try {
    const t = await api("/api/tailscale/status");
    if (!t.installed) {
      body.innerHTML = `<p class="muted">Tailscale 이 설치되어 있지 않습니다. 라즈베리파이에서 설치하세요:</p>
        <pre class="upgrade-log">curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up</pre>
        <p class="muted">설치 후 새로고침하면 어디서나 접속할 수 있는 주소가 표시됩니다.</p>`;
      return;
    }
    if (t.error) { body.innerHTML = `<p class="muted">오류: ${t.error}</p>`; }
    const urls = (t.access_urls || []).map((u) => `<a href="${u}" target="_blank" class="btn small">${u}</a>`).join(" ");
    const peers = (t.peers || []).map((p) => `<tr><td><span class="dot ${p.online ? "up" : "down"}"></span></td><td>${p.hostname || "—"}</td><td>${p.ip || "—"}</td><td>${p.os || ""}</td></tr>`).join("");
    body.innerHTML = `<div class="kv">
      <div class="item"><div class="k">상태</div><div class="v"><span class="badge ${t.running ? "up" : "down"}">${t.state || "—"}</span></div></div>
      <div class="item"><div class="k">호스트명</div><div class="v" style="font-size:15px">${t.hostname || "—"}</div></div>
      <div class="item"><div class="k">Tailscale IP</div><div class="v" style="font-size:15px">${t.ip || "—"}</div></div>
      <div class="item"><div class="k">테일넷</div><div class="v" style="font-size:13px">${t.tailnet || "—"}</div></div></div>
      ${urls ? `<h2>어디서나 접속 주소</h2><div>${urls}</div>` : ""}
      ${t.auth_url ? `<p style="margin-top:12px">로그인이 필요합니다: <a href="${t.auth_url}" target="_blank">${t.auth_url}</a></p>` : ""}
      ${peers ? `<h2>테일넷 기기</h2><table class="data-table"><thead><tr><th></th><th>호스트명</th><th>IP</th><th>OS</th></tr></thead><tbody>${peers}</tbody></table>` : ""}`;
    if (!t.running) btn.hidden = false;
  } catch (err) { body.innerHTML = `<p class="muted">오류: ${err.message}</p>`; }
}
loaders.tailscale = loadTailscale;
$("#refreshTs").addEventListener("click", loadTailscale);
$("#tsConnect").addEventListener("click", async () => {
  $("#tsBody").innerHTML = '<p class="muted">연결 시도 중… (로그인 URL이 표시되면 다른 기기에서 인증하세요)</p>';
  try { const r = await api("/api/tailscale/up", { method: "POST" });
    if (r.auth_url) { $("#tsBody").innerHTML = `<p>아래 주소로 로그인하세요:</p><p><a href="${r.auth_url}" target="_blank">${r.auth_url}</a></p>`; }
    setTimeout(loadTailscale, 3000);
  } catch (err) { $("#tsBody").innerHTML = `<p class="muted">오류: ${err.message}</p>`; }
});

// ---------------------------------------------------------------------------
// 장비 관리 (CRUD)
// ---------------------------------------------------------------------------
async function loadDevices() {
  const devices = await api("/api/devices");
  $("#deviceTable tbody").innerHTML = devices.map((d) => {
    const cls = d.last_status === true ? "up" : d.last_status === false ? "down" : "";
    const t = d.last_status === true ? "정상" : d.last_status === false ? "다운" : "—";
    const chk = d.check_method.toUpperCase() + (d.check_port ? ":" + d.check_port : "");
    return `<tr><td>${d.name}</td><td>${d.host}</td><td>${TYPE_LABELS[d.type] || d.type}</td><td>${chk}</td>
      <td><span class="badge ${cls}">${t}</span></td><td>${fmtMs(d.last_latency_ms)}</td><td>${d.enabled ? "✔" : "✖"}</td>
      <td style="white-space:nowrap"><button class="btn small" data-act="check" data-id="${d.id}">체크</button>
      <button class="btn small" data-act="edit" data-id="${d.id}">수정</button>
      <button class="btn small danger" data-act="del" data-id="${d.id}">삭제</button></td></tr>`;
  }).join("") || '<tr><td colspan="8" class="muted">등록된 장비가 없습니다.</td></tr>';
  $$('#deviceTable [data-act]').forEach((b) => b.addEventListener("click", () => deviceAction(b.dataset.act, b.dataset.id, devices)));
}
loaders.devices = loadDevices;
async function deviceAction(act, id, devices) {
  if (act === "del") { if (!confirm("이 장비를 삭제할까요? 기록도 함께 삭제됩니다.")) return; await api(`/api/devices/${id}`, { method: "DELETE" }); loadDevices(); }
  else if (act === "check") { const r = await api(`/api/devices/${id}/check`, { method: "POST" }); alert(r.is_up ? `정상 (${fmtMs(r.latency_ms)})` : `다운 (${r.detail || ""})`); loadDevices(); }
  else if (act === "edit") openDeviceModal(devices.find((d) => d.id == id));
}
const modal = $("#deviceModal"), deviceForm = $("#deviceForm");
function openDeviceModal(device) {
  $("#deviceModalTitle").textContent = device && device.id ? "장비 수정" : "장비 추가";
  deviceForm.reset(); deviceForm.id.value = device && device.id ? device.id : "";
  if (device) {
    deviceForm.name.value = device.name || ""; deviceForm.host.value = device.host || "";
    deviceForm.type.value = device.type || "other"; deviceForm.check_method.value = device.check_method || "icmp";
    deviceForm.check_port.value = device.check_port || ""; deviceForm.note.value = device.note || "";
    deviceForm.enabled.checked = device.enabled !== false;
  }
  modal.classList.add("show");
}
$("#addDevice").addEventListener("click", () => openDeviceModal(null));
$("#cancelDevice").addEventListener("click", () => modal.classList.remove("show"));
modal.addEventListener("click", (e) => { if (e.target === modal) modal.classList.remove("show"); });
deviceForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = deviceForm.id.value;
  const p = { name: deviceForm.name.value.trim(), host: deviceForm.host.value.trim(), type: deviceForm.type.value,
    check_method: deviceForm.check_method.value, check_port: deviceForm.check_port.value ? parseInt(deviceForm.check_port.value, 10) : null,
    note: deviceForm.note.value.trim() || null, enabled: deviceForm.enabled.checked };
  try {
    if (id) await api(`/api/devices/${id}`, { method: "PATCH", body: JSON.stringify(p) });
    else await api("/api/devices", { method: "POST", body: JSON.stringify(p) });
    modal.classList.remove("show"); loadDevices();
  } catch (err) { alert("저장 실패: " + err.message); }
});

// ---------------------------------------------------------------------------
// 보안 (로그인 계정 / IP 차단 / fail2ban)
// ---------------------------------------------------------------------------
async function loadSecurity() {
  const set = await api("/api/settings");
  const sec = set.security || {};
  $("#authEnabled").checked = !!sec.auth_enabled;
  $("#secMax").value = sec.max_attempts ?? 3; $("#secBan").value = sec.ban_minutes ?? 15; $("#secWindow").value = sec.window_minutes ?? 10;
  const users = await api("/api/auth/users");
  $("#userTable tbody").innerHTML = users.map((u) => `<tr><td>${u.username}</td>
    <td><span class="badge ${u.enabled ? "up" : ""}">${u.enabled ? "활성" : "미인증"}</span></td>
    <td>${fmtTime(u.last_login)}</td><td><button class="btn small danger" data-uid="${u.id}">삭제</button></td></tr>`).join("")
    || '<tr><td colspan="4" class="muted">계정이 없습니다.</td></tr>';
  $$('#userTable [data-uid]').forEach((b) => b.addEventListener("click", async () => { if (confirm("계정을 삭제할까요?")) { await api(`/api/auth/users/${b.dataset.uid}`, { method: "DELETE" }); loadSecurity(); } }));
  await loadBans();
  await loadF2b(set.fail2ban || {});
}
loaders.security = loadSecurity;

async function loadBans() {
  const bans = await api("/api/auth/bans?include_inactive=true");
  $("#banTable tbody").innerHTML = bans.map((b) => `<tr><td>${b.ip}</td><td>${b.reason || ""}</td><td>${b.source}</td>
    <td>${b.active ? (b.expires_at ? fmtTime(b.expires_at) : "영구") : "해제됨"}</td>
    <td>${b.active ? `<button class="btn small" data-ip="${b.ip}">해제</button>` : ""}</td></tr>`).join("")
    || '<tr><td colspan="5" class="muted">차단된 IP가 없습니다.</td></tr>';
  $$('#banTable [data-ip]').forEach((b) => b.addEventListener("click", async () => { await api(`/api/auth/bans/${b.dataset.ip}`, { method: "DELETE" }); loadBans(); }));
}
$("#authEnabled").addEventListener("change", async (e) => {
  try { await api("/api/settings", { method: "PUT", body: JSON.stringify({ security: { auth_enabled: e.target.checked } }) }); checkAuth(); }
  catch (err) { alert(err.message); e.target.checked = !e.target.checked; }
});
$("#saveSecPolicy").addEventListener("click", async () => {
  try {
    await api("/api/settings", { method: "PUT", body: JSON.stringify({ security: { max_attempts: +$("#secMax").value, ban_minutes: +$("#secBan").value, window_minutes: +$("#secWindow").value } }) });
    $("#secMsg").textContent = "저장됨"; setTimeout(() => ($("#secMsg").textContent = ""), 3000);
  } catch (err) { $("#secMsg").textContent = "오류: " + err.message; }
});
$("#addBan").addEventListener("click", async () => {
  const ip = $("#banIp").value.trim(); if (!ip) return;
  await api("/api/auth/bans", { method: "POST", body: JSON.stringify({ ip, minutes: null, reason: "수동 차단" }) });
  $("#banIp").value = ""; loadBans();
});

async function loadF2b(cfg) {
  $("#f2bEnabled").checked = !!cfg.enabled; $("#f2bJail").value = cfg.jail || "sshd";
  $("#f2bMax").value = cfg.maxretry ?? 3; $("#f2bBan").value = cfg.bantime_minutes ?? 15; $("#f2bFind").value = cfg.findtime_minutes ?? 10;
  try {
    const s = await api("/api/security/fail2ban/status");
    if (!s.installed) { $("#f2bStatus").innerHTML = 'fail2ban 미설치 — <code>sudo apt install fail2ban</code> 후 사용하세요.'; return; }
    const st = s.status || {};
    $("#f2bStatus").innerHTML = st.error ? `상태: ${st.error}` :
      `jail <b>${st.jail}</b> · 현재 차단 ${st.currently_banned ?? "—"} · 누적 차단 ${st.total_banned ?? "—"} · 차단IP: ${(st.banned_ips || []).join(", ") || "없음"}`;
  } catch (err) { $("#f2bStatus").textContent = "오류: " + err.message; }
}
$("#saveF2b").addEventListener("click", async () => {
  const out = $("#f2bOut");
  try {
    const r = await api("/api/security/fail2ban/policy", { method: "PUT", body: JSON.stringify({
      enabled: $("#f2bEnabled").checked, jail: $("#f2bJail").value.trim(), maxretry: +$("#f2bMax").value,
      bantime_minutes: +$("#f2bBan").value, findtime_minutes: +$("#f2bFind").value }) });
    const a = r.apply || {};
    $("#f2bMsg").textContent = a.ok ? "적용됨" : (a.need_sudo ? "권한 부족 — 아래 내용을 수동 적용하세요" : (a.error || "완료"));
    if (a.content) { out.hidden = false; out.textContent = `# ${a.path || "/etc/fail2ban/jail.d/homelab-monitor.local"}\n${a.content}`; }
    else out.hidden = true;
    setTimeout(() => ($("#f2bMsg").textContent = ""), 5000);
  } catch (err) { $("#f2bMsg").textContent = "오류: " + err.message; }
});

// 계정 추가 (QR) 모달
const userModal = $("#userModal");
function resetUserModal() { $("#userStep1").hidden = false; $("#userStep2").hidden = true; $("#newUsername").value = ""; $("#verifyCode").value = ""; $("#userMsg").textContent = ""; }
$("#addUser").addEventListener("click", () => { resetUserModal(); userModal.classList.add("show"); });
$("#cancelUser").addEventListener("click", () => userModal.classList.remove("show"));
$("#cancelUser2").addEventListener("click", () => userModal.classList.remove("show"));
userModal.addEventListener("click", (e) => { if (e.target === userModal) userModal.classList.remove("show"); });
let pendingUserId = null;
$("#createUser").addEventListener("click", async () => {
  const username = $("#newUsername").value.trim(); if (!username) return;
  try {
    const r = await api("/api/auth/users", { method: "POST", body: JSON.stringify({ username }) });
    pendingUserId = r.id; $("#qrBox").innerHTML = r.qr_svg; $("#secretText").textContent = r.secret;
    $("#userStep1").hidden = true; $("#userStep2").hidden = false;
  } catch (err) { alert(err.message); }
});
$("#verifyUser").addEventListener("click", async () => {
  try {
    await api(`/api/auth/users/${pendingUserId}/verify`, { method: "POST", body: JSON.stringify({ code: $("#verifyCode").value.trim() }) });
    userModal.classList.remove("show"); loadSecurity();
  } catch (err) { $("#userMsg").textContent = err.message; }
});

// ---------------------------------------------------------------------------
// 설정 + 버전/업그레이드
// ---------------------------------------------------------------------------
const SETTINGS_SCHEMA = [
  { group: "monitoring", title: "모니터링", fields: [
    { key: "interval_seconds", label: "체크 주기 (초)", type: "number" },
    { key: "timeout_seconds", label: "타임아웃 (초)", type: "number" },
    { key: "concurrency", label: "동시 검사 수", type: "number" }] },
  { group: "scan", title: "IP 스캔", fields: [
    { key: "enabled", label: "자동 스캔 사용", type: "checkbox" },
    { key: "subnet", label: "서브넷 (비우면 자동감지)", type: "text" },
    { key: "interval_minutes", label: "스캔 주기 (분)", type: "number" },
    { key: "method", label: "방식", type: "select", options: ["icmp", "tcp", "http"] }] },
  { group: "retention", title: "데이터 보관", fields: [
    { key: "raw_days", label: "원본 기록 보관 (일)", type: "number" },
    { key: "hourly_days", label: "시간별 집계 보관 (일)", type: "number" }] },
  { group: "speedtest", title: "인터넷 속도 측정", fields: [
    { key: "enabled", label: "자동 측정 사용", type: "checkbox" },
    { key: "interval_minutes", label: "측정 주기 (분, 0=끔)", type: "number" },
    { key: "method", label: "방식", type: "select", options: ["auto", "python", "ookla"] }] },
  { group: "synology", title: "시놀로지 폴링", fields: [
    { key: "poll_seconds", label: "NAS 상태 폴링 주기 (초)", type: "number" }] },
  { group: "router", title: "공유기", fields: [
    { key: "enabled", label: "사용", type: "checkbox" },
    { key: "host", label: "호스트/IP", type: "text" },
    { key: "admin_url", label: "관리 페이지 URL", type: "text" },
    { key: "snmp_enabled", label: "SNMP 사용", type: "checkbox" },
    { key: "snmp_community", label: "SNMP community", type: "text" },
    { key: "wan_if_index", label: "WAN ifIndex", type: "number" }] },
  { group: "update", title: "자동 업그레이드", fields: [
    { key: "auto_check", label: "자동 업데이트 확인", type: "checkbox" },
    { key: "check_interval_hours", label: "확인 주기 (시간)", type: "number" },
    { key: "auto_apply", label: "새 버전 자동 설치+재시작", type: "checkbox" },
    { key: "allow_manual", label: "수동 업그레이드 허용", type: "checkbox" }] },
];
let settingsData = {};
async function loadSettings() {
  loadVersion(); checkUpdate(false, false);
  settingsData = await api("/api/settings");
  $("#settingsForm").innerHTML = SETTINGS_SCHEMA.map((grp) => {
    const data = settingsData[grp.group] || {};
    const rows = grp.fields.map((f) => {
      const v = data[f.key]; let input;
      if (f.type === "checkbox") input = `<input type="checkbox" data-group="${grp.group}" data-key="${f.key}" ${v ? "checked" : ""}/>`;
      else if (f.type === "select") input = `<select data-group="${grp.group}" data-key="${f.key}">${f.options.map((o) => `<option ${o === v ? "selected" : ""}>${o}</option>`).join("")}</select>`;
      else input = `<input type="${f.type}" data-group="${grp.group}" data-key="${f.key}" value="${v == null ? "" : v}"/>`;
      return `<div class="settings-row"><label>${f.label}</label>${input}</div>`;
    }).join("");
    return `<div class="settings-group"><h3>${grp.title}</h3>${rows}</div>`;
  }).join("");
}
loaders.settings = loadSettings;
$("#saveSettings").addEventListener("click", async () => {
  const payload = {};
  SETTINGS_SCHEMA.forEach((grp) => { payload[grp.group] = { ...(settingsData[grp.group] || {}) }; });
  $$("#settingsForm [data-group]").forEach((el) => {
    const g = el.dataset.group, k = el.dataset.key; let v;
    if (el.type === "checkbox") v = el.checked; else if (el.type === "number") v = el.value === "" ? null : Number(el.value); else v = el.value;
    payload[g][k] = v;
  });
  try {
    await api("/api/settings", { method: "PUT", body: JSON.stringify(payload) });
    $("#settingsMsg").textContent = "저장되었습니다. (주기 변경은 최대 30초 후 반영)";
    setTimeout(() => ($("#settingsMsg").textContent = ""), 4000); loadSettings();
  } catch (err) { $("#settingsMsg").textContent = "오류: " + err.message; }
});

async function loadVersion() {
  try { const v = await api("/api/system/version");
    const txt = "v" + v.version + (v.commit ? " · " + v.commit : "");
    $("#appVersion").textContent = txt;
    if ($("#curVersion")) $("#curVersion").textContent = txt + (v.branch ? " (" + v.branch + ")" : "");
  } catch (_) {}
}
async function checkUpdate(busy, fetch = true) {
  const state = $("#updateState"), badge = $("#appVersion");
  if (busy) state.textContent = "확인 중…";
  try {
    const r = await api("/api/system/update-check?fetch=" + (fetch ? "true" : "false"));
    if (r.supported === false) { state.textContent = "git 저장소가 아니어서 확인 불가"; return; }
    if (r.error) { state.textContent = "오류: " + r.error; return; }
    if (r.up_to_date) { state.textContent = "최신 버전입니다."; badge.classList.remove("update"); }
    else { state.innerHTML = `<span style="color:var(--warn)">새 버전 ${r.behind}개 사용 가능</span>` + (r.latest_message ? ` — ${r.latest_message}` : ""); badge.classList.add("update"); }
  } catch (err) { if (busy) state.textContent = "오류: " + err.message; }
}
let upgradePoll = null;
async function pollUpgrade() {
  const logEl = $("#upgradeLog");
  try { const s = await api("/api/system/upgrade-status"); logEl.hidden = false; logEl.textContent = s.log || ""; logEl.scrollTop = logEl.scrollHeight;
    if (s.status === "success") $("#updateState").textContent = "업그레이드 완료 — 재시작 중일 수 있습니다.";
    if (s.status === "error") $("#updateState").textContent = "업그레이드 실패 — 로그 확인.";
  } catch (_) { logEl.textContent += "\n(서버 재시작 중…)"; }
}
$("#checkUpdate").addEventListener("click", () => checkUpdate(true, true));
$("#doUpgrade").addEventListener("click", async () => {
  if (!confirm("최신 버전으로 업그레이드할까요? 완료 후 서버가 재시작됩니다.")) return;
  const logEl = $("#upgradeLog"); logEl.hidden = false; logEl.textContent = "업그레이드 시작…";
  try { const r = await api("/api/system/upgrade", { method: "POST" });
    if (r.status === "error") { logEl.textContent = "오류: " + r.error; return; }
    if (upgradePoll) clearInterval(upgradePoll);
    let n = 0; upgradePoll = setInterval(async () => { n++; await pollUpgrade(); if (n > 90) { clearInterval(upgradePoll); loadVersion(); } }, 2000);
  } catch (err) { logEl.textContent = "오류: " + err.message; }
});

// ---------------------------------------------------------------------------
// Boot
// ---------------------------------------------------------------------------
async function boot() {
  const ok = await checkAuth();
  if (!ok) return;
  loadVersion();
  loadDashboard();
}
setInterval(() => { if ($("#dashboard").classList.contains("active")) loadDashboard(); }, 30000);
boot();
