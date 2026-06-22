"use strict";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
const $ = (sel, el = document) => el.querySelector(sel);
const $$ = (sel, el = document) => [...el.querySelectorAll(sel)];

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).detail || msg; } catch (_) {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

function fmtPct(v) { return v == null ? "—" : v.toFixed(2) + "%"; }
function fmtMs(v) { return v == null ? "—" : Math.round(v) + " ms"; }
function fmtMbps(v) { return v == null ? "—" : v.toFixed(1) + " Mbps"; }

function fmtDuration(sec) {
  if (sec == null) return "—";
  if (sec < 60) return sec + "초";
  if (sec < 3600) return Math.floor(sec / 60) + "분";
  if (sec < 86400) return (sec / 3600).toFixed(1) + "시간";
  return (sec / 86400).toFixed(1) + "일";
}

function fmtTime(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  return d.toLocaleString("ko-KR", { hour12: false });
}

function fmtBytes(n) {
  if (n == null) return "—";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return n.toFixed(1) + " " + units[i];
}

const TYPE_LABELS = {
  router: "공유기", nas: "NAS", server: "서버", pc: "PC",
  ap: "AP", printer: "프린터", iot: "IoT", other: "기타",
};

// ---------------------------------------------------------------------------
// Tab navigation
// ---------------------------------------------------------------------------
const loaders = {};
$$(".tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    $$(".tab").forEach((t) => t.classList.remove("active"));
    $$(".page").forEach((p) => p.classList.remove("active"));
    tab.classList.add("active");
    const id = tab.dataset.tab;
    $("#" + id).classList.add("active");
    if (loaders[id]) loaders[id]();
  });
});

// ---------------------------------------------------------------------------
// Charts registry
// ---------------------------------------------------------------------------
const charts = {};
function drawChart(id, config) {
  const ctx = document.getElementById(id);
  if (!ctx) return;
  if (charts[id]) charts[id].destroy();
  charts[id] = new Chart(ctx, config);
}

const CHART_BASE = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: "index", intersect: false },
  scales: {
    x: { ticks: { color: "#8aa0c0", maxTicksLimit: 8 }, grid: { color: "#2a3958" } },
    y: { ticks: { color: "#8aa0c0" }, grid: { color: "#2a3958" } },
  },
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
      <div class="card"><div class="label">다운</div><div class="value" style="color:var(--down)">${data.down}</div></div>
    `;

    $("#deviceCards").innerHTML = data.devices.map((d) => {
      const cls = d.last_status === true ? "up" : d.last_status === false ? "down" : "";
      const statusText = d.last_status === true ? "정상" : d.last_status === false ? "다운" : "확인 안됨";
      return `
        <div class="device-card ${cls}">
          <div class="dname">${d.name}<span class="dot ${cls}"></span></div>
          <div class="dhost">${d.host} · ${TYPE_LABELS[d.type] || d.type}</div>
          <div class="dmeta">
            <span>${statusText} · ${fmtMs(d.last_latency_ms)}</span>
            <span>24h ${fmtPct(d.uptime_24h)}</span>
          </div>
        </div>`;
    }).join("") || '<p class="muted">등록된 장비가 없습니다. [장비 관리]에서 추가하세요.</p>';

    const events = await api("/api/events?limit=15");
    $("#eventsTable tbody").innerHTML = events.map((e) => `
      <tr>
        <td>${fmtTime(e.ts)}</td>
        <td>${e.device_name}</td>
        <td><span class="badge ${e.is_up ? "up" : "down"}">${e.is_up ? "복구" : "다운"}</span></td>
        <td>${fmtDuration(e.prev_duration_s)}</td>
      </tr>`).join("") || '<tr><td colspan="4" class="muted">이벤트 없음</td></tr>';
  } catch (err) {
    $("#globalStatus").textContent = "오류: " + err.message;
  }
}
loaders.dashboard = loadDashboard;

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------
let histRange = "30d";
async function populateDeviceSelect() {
  const devices = await api("/api/devices");
  const sel = $("#histDevice");
  const cur = sel.value;
  sel.innerHTML = devices.map((d) => `<option value="${d.id}">${d.name}</option>`).join("");
  if (cur) sel.value = cur;
  return devices;
}

async function loadHistory() {
  const devices = await populateDeviceSelect();
  if (!devices.length) {
    $("#histStats").innerHTML = '<p class="muted">장비를 먼저 추가하세요.</p>';
    return;
  }
  const id = $("#histDevice").value || devices[0].id;
  const summary = await api(`/api/devices/${id}/uptime?range=${histRange}`);

  $("#histStats").innerHTML = `
    <div class="card"><div class="label">가동률 (${histRange})</div><div class="value" style="color:var(--up)">${fmtPct(summary.uptime_pct)}</div></div>
    <div class="card"><div class="label">평균 지연</div><div class="value small">${fmtMs(summary.latency_avg_ms)}</div></div>
    <div class="card"><div class="label">샘플 수</div><div class="value small">${summary.samples.toLocaleString()}</div></div>
    <div class="card"><div class="label">데이터 소스</div><div class="value small">${summary.source}</div></div>
  `;

  const labels = summary.series.map((p) => fmtTime(p.t));
  const isRaw = summary.source === "raw";
  const uptimeData = summary.series.map((p) => isRaw ? (p.up ? 100 : 0) : p.uptime);
  const latData = summary.series.map((p) => p.latency);

  drawChart("uptimeChart", {
    type: "line",
    data: { labels, datasets: [{
      label: "가동률 (%)", data: uptimeData,
      borderColor: "#34d399", backgroundColor: "rgba(52,211,153,.15)",
      fill: true, stepped: isRaw, pointRadius: 0, tension: 0.2,
    }] },
    options: { ...CHART_BASE, scales: { ...CHART_BASE.scales, y: { ...CHART_BASE.scales.y, min: 0, max: 100 } } },
  });

  drawChart("latencyChart", {
    type: "line",
    data: { labels, datasets: [{
      label: "지연시간 (ms)", data: latData,
      borderColor: "#4f8cff", backgroundColor: "rgba(79,140,255,.12)",
      fill: true, pointRadius: 0, tension: 0.2,
    }] },
    options: CHART_BASE,
  });

  const events = await api(`/api/devices/${id}/events?limit=30`);
  $("#histEvents tbody").innerHTML = events.map((e) => `
    <tr>
      <td>${fmtTime(e.ts)}</td>
      <td><span class="badge ${e.is_up ? "up" : "down"}">${e.is_up ? "복구" : "다운"}</span></td>
      <td>${fmtDuration(e.prev_duration_s)}</td>
    </tr>`).join("") || '<tr><td colspan="3" class="muted">이벤트 없음</td></tr>';
}
loaders.history = loadHistory;

$("#histDevice").addEventListener("change", loadHistory);
$$("#histRanges button").forEach((b) => b.addEventListener("click", () => {
  $$("#histRanges button").forEach((x) => x.classList.remove("active"));
  b.classList.add("active");
  histRange = b.dataset.range;
  loadHistory();
}));

// ---------------------------------------------------------------------------
// Speed test
// ---------------------------------------------------------------------------
let speedDays = 30;
async function loadSpeed() {
  const [history, stats] = await Promise.all([
    api(`/api/speedtest?days=${speedDays}&limit=2000`),
    api(`/api/speedtest/stats?days=${speedDays}`),
  ]);

  $("#speedStats").innerHTML = `
    <div class="card"><div class="label">평균 다운로드</div><div class="value">${fmtMbps(stats.download_avg)}</div></div>
    <div class="card"><div class="label">평균 업로드</div><div class="value">${fmtMbps(stats.upload_avg)}</div></div>
    <div class="card"><div class="label">최소/최대 다운</div><div class="value small">${fmtMbps(stats.download_min)} / ${fmtMbps(stats.download_max)}</div></div>
    <div class="card"><div class="label">평균 핑</div><div class="value small">${fmtMs(stats.ping_avg)}</div></div>
    <div class="card"><div class="label">측정 횟수</div><div class="value small">${stats.count}</div></div>
  `;

  const ok = history.filter((h) => h.ok);
  const labels = ok.map((h) => fmtTime(h.ts));
  drawChart("speedChart", {
    type: "line",
    data: { labels, datasets: [
      { label: "다운로드 (Mbps)", data: ok.map((h) => h.download_mbps), borderColor: "#34d399", backgroundColor: "rgba(52,211,153,.12)", fill: true, pointRadius: 0, tension: 0.25 },
      { label: "업로드 (Mbps)", data: ok.map((h) => h.upload_mbps), borderColor: "#4f8cff", backgroundColor: "rgba(79,140,255,.12)", fill: true, pointRadius: 0, tension: 0.25 },
    ] },
    options: CHART_BASE,
  });

  drawChart("pingChart", {
    type: "line",
    data: { labels, datasets: [
      { label: "핑 (ms)", data: ok.map((h) => h.ping_ms), borderColor: "#fbbf24", backgroundColor: "rgba(251,191,36,.12)", fill: true, pointRadius: 0, tension: 0.25 },
    ] },
    options: CHART_BASE,
  });
}
loaders.speed = loadSpeed;

$("#runSpeed").addEventListener("click", async () => {
  const msg = $("#speedMsg");
  msg.textContent = "측정 중… (최대 1분 소요)";
  try {
    await api("/api/speedtest/run", { method: "POST" });
    let tries = 0;
    const poll = setInterval(async () => {
      tries++;
      await loadSpeed();
      if (tries >= 12) { clearInterval(poll); msg.textContent = ""; }
    }, 5000);
    msg.textContent = "측정을 시작했습니다. 결과는 잠시 후 갱신됩니다.";
  } catch (err) {
    msg.textContent = "오류: " + err.message;
  }
});
$$("#speedRanges button").forEach((b) => b.addEventListener("click", () => {
  $$("#speedRanges button").forEach((x) => x.classList.remove("active"));
  b.classList.add("active");
  speedDays = parseInt(b.dataset.days, 10);
  loadSpeed();
}));

// ---------------------------------------------------------------------------
// Synology
// ---------------------------------------------------------------------------
async function loadSynology() {
  const body = $("#synoBody");
  try {
    const s = await api("/api/synology/status");
    if (!s.enabled) {
      body.innerHTML = '<p class="muted">시놀로지가 비활성화되어 있습니다. [설정]에서 사용 설정하세요.</p>';
      return;
    }
    if (!s.connected) {
      body.innerHTML = '<p class="muted">시놀로지에 연결할 수 없습니다.</p>';
      return;
    }
    const volumes = (s.volumes || []).map((v) => `
      <div class="item">
        <div class="k">${v.id || "볼륨"} (${v.fs_type || ""})</div>
        <div class="v">${v.used_pct != null ? v.used_pct + "%" : "—"}</div>
        <div class="progress"><span style="width:${v.used_pct || 0}%"></span></div>
        <div class="k">${fmtBytes(v.used_bytes)} / ${fmtBytes(v.total_bytes)}</div>
      </div>`).join("");

    body.innerHTML = `
      <div class="kv">
        <div class="item"><div class="k">모델</div><div class="v">${s.model || "—"}</div></div>
        <div class="item"><div class="k">DSM</div><div class="v" style="font-size:14px">${s.dsm_version || "—"}</div></div>
        <div class="item"><div class="k">CPU 부하</div><div class="v">${s.cpu_load != null ? s.cpu_load + "%" : "—"}</div></div>
        <div class="item"><div class="k">메모리 사용</div><div class="v">${s.mem_usage != null ? s.mem_usage + "%" : "—"}</div></div>
        <div class="item"><div class="k">온도</div><div class="v">${s.temperature_c != null ? s.temperature_c + "°C" : "—"}</div></div>
        <div class="item"><div class="k">가동시간</div><div class="v" style="font-size:16px">${fmtDuration(s.uptime_s)}</div></div>
      </div>
      <h2>볼륨</h2>
      <div class="kv">${volumes || '<p class="muted">볼륨 정보 없음</p>'}</div>
    `;

    const hist = await api("/api/synology/history?hours=24");
    const labels = hist.map((h) => fmtTime(h.ts));
    drawChart("synoChart", {
      type: "line",
      data: { labels, datasets: [
        { label: "CPU (%)", data: hist.map((h) => h.cpu_load), borderColor: "#4f8cff", pointRadius: 0, tension: 0.25 },
        { label: "메모리 (%)", data: hist.map((h) => h.mem_usage), borderColor: "#34d399", pointRadius: 0, tension: 0.25 },
        { label: "온도 (°C)", data: hist.map((h) => h.temp_c), borderColor: "#fbbf24", pointRadius: 0, tension: 0.25 },
      ] },
      options: CHART_BASE,
    });
  } catch (err) {
    body.innerHTML = `<p class="muted">오류: ${err.message}</p>`;
  }
}
loaders.synology = loadSynology;
$("#refreshSyno").addEventListener("click", loadSynology);

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------
async function loadRouter() {
  const body = $("#routerBody");
  try {
    const r = await api("/api/router/status");
    if (!r.enabled) {
      body.innerHTML = '<p class="muted">공유기가 비활성화되어 있습니다. [설정]에서 사용 설정하세요.</p>';
      return;
    }
    const admin = r.admin_url ? `<a href="${r.admin_url}" target="_blank" class="btn small">관리 페이지 열기</a>` : "";
    body.innerHTML = `
      <div class="kv">
        <div class="item"><div class="k">호스트</div><div class="v" style="font-size:16px">${r.host}</div></div>
        <div class="item"><div class="k">연결</div><div class="v"><span class="badge ${r.reachable ? "up" : "down"}">${r.reachable ? "정상" : "다운"}</span></div></div>
        <div class="item"><div class="k">지연</div><div class="v">${fmtMs(r.latency_ms)}</div></div>
        ${r.uptime_s != null ? `<div class="item"><div class="k">가동시간</div><div class="v" style="font-size:16px">${fmtDuration(r.uptime_s)}</div></div>` : ""}
        ${r.wan_down_mbps != null ? `<div class="item"><div class="k">WAN 다운</div><div class="v">${fmtMbps(r.wan_down_mbps)}</div></div>` : ""}
        ${r.wan_up_mbps != null ? `<div class="item"><div class="k">WAN 업</div><div class="v">${fmtMbps(r.wan_up_mbps)}</div></div>` : ""}
      </div>
      ${r.sys_descr ? `<p class="muted" style="margin-top:12px">${r.sys_descr}</p>` : ""}
      ${!r.snmp ? '<p class="muted" style="margin-top:12px">SNMP 미사용 — 자세한 정보를 보려면 공유기에서 SNMP를 켜고 [설정]에서 활성화하세요.</p>' : ""}
      <div style="margin-top:12px">${admin}</div>
    `;
  } catch (err) {
    body.innerHTML = `<p class="muted">오류: ${err.message}</p>`;
  }
}
loaders.routerinfo = loadRouter;
$("#refreshRouter").addEventListener("click", loadRouter);

// ---------------------------------------------------------------------------
// Devices management
// ---------------------------------------------------------------------------
async function loadDevices() {
  const devices = await api("/api/devices");
  $("#deviceTable tbody").innerHTML = devices.map((d) => {
    const cls = d.last_status === true ? "up" : d.last_status === false ? "down" : "";
    const statusText = d.last_status === true ? "정상" : d.last_status === false ? "다운" : "—";
    const chk = d.check_method.toUpperCase() + (d.check_port ? ":" + d.check_port : "");
    return `
      <tr>
        <td>${d.name}</td>
        <td>${d.host}</td>
        <td>${TYPE_LABELS[d.type] || d.type}</td>
        <td>${chk}</td>
        <td><span class="badge ${cls}">${statusText}</span></td>
        <td>${fmtMs(d.last_latency_ms)}</td>
        <td>${d.enabled ? "✔" : "✖"}</td>
        <td style="white-space:nowrap">
          <button class="btn small" data-act="check" data-id="${d.id}">체크</button>
          <button class="btn small" data-act="edit" data-id="${d.id}">수정</button>
          <button class="btn small danger" data-act="del" data-id="${d.id}">삭제</button>
        </td>
      </tr>`;
  }).join("") || '<tr><td colspan="8" class="muted">등록된 장비가 없습니다.</td></tr>';

  $$('#deviceTable [data-act]').forEach((btn) => {
    btn.addEventListener("click", () => deviceAction(btn.dataset.act, btn.dataset.id, devices));
  });
}
loaders.devices = loadDevices;

async function deviceAction(act, id, devices) {
  if (act === "del") {
    if (!confirm("이 장비를 삭제할까요? 기록도 함께 삭제됩니다.")) return;
    await api(`/api/devices/${id}`, { method: "DELETE" });
    loadDevices();
  } else if (act === "check") {
    const r = await api(`/api/devices/${id}/check`, { method: "POST" });
    alert(r.is_up ? `정상 (${fmtMs(r.latency_ms)})` : `다운 (${r.detail || ""})`);
    loadDevices();
  } else if (act === "edit") {
    openDeviceModal(devices.find((d) => d.id == id));
  }
}

const modal = $("#deviceModal");
const deviceForm = $("#deviceForm");
function openDeviceModal(device) {
  $("#deviceModalTitle").textContent = device ? "장비 수정" : "장비 추가";
  deviceForm.reset();
  deviceForm.id.value = device ? device.id : "";
  if (device) {
    deviceForm.name.value = device.name;
    deviceForm.host.value = device.host;
    deviceForm.type.value = device.type;
    deviceForm.check_method.value = device.check_method;
    deviceForm.check_port.value = device.check_port || "";
    deviceForm.note.value = device.note || "";
    deviceForm.enabled.checked = device.enabled;
  }
  modal.classList.add("show");
}
$("#addDevice").addEventListener("click", () => openDeviceModal(null));
$("#cancelDevice").addEventListener("click", () => modal.classList.remove("show"));
modal.addEventListener("click", (e) => { if (e.target === modal) modal.classList.remove("show"); });

deviceForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = deviceForm.id.value;
  const payload = {
    name: deviceForm.name.value.trim(),
    host: deviceForm.host.value.trim(),
    type: deviceForm.type.value,
    check_method: deviceForm.check_method.value,
    check_port: deviceForm.check_port.value ? parseInt(deviceForm.check_port.value, 10) : null,
    note: deviceForm.note.value.trim() || null,
    enabled: deviceForm.enabled.checked,
  };
  try {
    if (id) {
      await api(`/api/devices/${id}`, { method: "PATCH", body: JSON.stringify(payload) });
    } else {
      await api("/api/devices", { method: "POST", body: JSON.stringify(payload) });
    }
    modal.classList.remove("show");
    loadDevices();
  } catch (err) {
    alert("저장 실패: " + err.message);
  }
});

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------
const SETTINGS_SCHEMA = [
  { group: "monitoring", title: "모니터링", fields: [
    { key: "interval_seconds", label: "체크 주기 (초)", type: "number" },
    { key: "timeout_seconds", label: "타임아웃 (초)", type: "number" },
    { key: "concurrency", label: "동시 검사 수", type: "number" },
  ]},
  { group: "retention", title: "데이터 보관", fields: [
    { key: "raw_days", label: "원본 기록 보관 (일)", type: "number" },
    { key: "hourly_days", label: "시간별 집계 보관 (일)", type: "number" },
  ]},
  { group: "speedtest", title: "인터넷 속도 측정", fields: [
    { key: "enabled", label: "자동 측정 사용", type: "checkbox" },
    { key: "interval_minutes", label: "측정 주기 (분, 0=끔)", type: "number" },
    { key: "method", label: "방식", type: "select", options: ["auto", "python", "ookla"] },
  ]},
  { group: "synology", title: "시놀로지", fields: [
    { key: "enabled", label: "사용", type: "checkbox" },
    { key: "host", label: "호스트/IP", type: "text" },
    { key: "port", label: "포트", type: "number" },
    { key: "https", label: "HTTPS", type: "checkbox" },
    { key: "verify_ssl", label: "SSL 인증서 검증", type: "checkbox" },
    { key: "username", label: "계정", type: "text" },
    { key: "password", label: "비밀번호", type: "password" },
    { key: "poll_seconds", label: "폴링 주기 (초)", type: "number" },
  ]},
  { group: "update", title: "자동 업그레이드", fields: [
    { key: "auto_check", label: "자동 업데이트 확인", type: "checkbox" },
    { key: "check_interval_hours", label: "확인 주기 (시간)", type: "number" },
    { key: "auto_apply", label: "새 버전 자동 설치+재시작", type: "checkbox" },
    { key: "allow_manual", label: "수동 업그레이드 허용", type: "checkbox" },
  ]},
  { group: "router", title: "공유기", fields: [
    { key: "enabled", label: "사용", type: "checkbox" },
    { key: "host", label: "호스트/IP", type: "text" },
    { key: "admin_url", label: "관리 페이지 URL", type: "text" },
    { key: "snmp_enabled", label: "SNMP 사용", type: "checkbox" },
    { key: "snmp_community", label: "SNMP community", type: "text" },
    { key: "snmp_port", label: "SNMP 포트", type: "number" },
    { key: "wan_if_index", label: "WAN ifIndex", type: "number" },
  ]},
];

// --- Version & auto-upgrade ---
async function loadVersion() {
  try {
    const v = await api("/api/system/version");
    let txt = "v" + v.version + (v.commit ? " · " + v.commit : "");
    $("#appVersion").textContent = txt;
    const cur = $("#curVersion");
    if (cur) cur.textContent = txt + (v.branch ? " (" + v.branch + ")" : "");
    return v;
  } catch (_) { /* ignore */ }
}

async function checkUpdate(busy, fetch = true) {
  const state = $("#updateState");
  const badge = $("#appVersion");
  if (busy) state.textContent = "확인 중…";
  try {
    const r = await api("/api/system/update-check?fetch=" + (fetch ? "true" : "false"));
    if (r.supported === false) { state.textContent = "git 저장소가 아니어서 업데이트를 확인할 수 없습니다."; return; }
    if (r.error) { state.textContent = "오류: " + r.error; return; }
    if (r.up_to_date) {
      state.textContent = "최신 버전입니다.";
      badge.classList.remove("update");
    } else {
      state.innerHTML = `<span style="color:var(--warn)">새 버전 ${r.behind}개 커밋 사용 가능</span>` +
        (r.latest_message ? ` — 최신: ${r.latest_message}` : "");
      badge.classList.add("update");
      badge.title = "업데이트 가능";
    }
  } catch (err) {
    if (busy) state.textContent = "오류: " + err.message;
  }
}

let upgradePoll = null;
async function pollUpgrade() {
  const logEl = $("#upgradeLog");
  try {
    const s = await api("/api/system/upgrade-status");
    logEl.hidden = false;
    logEl.textContent = s.log || "";
    logEl.scrollTop = logEl.scrollHeight;
    if (s.status === "success") $("#updateState").textContent = "업그레이드 완료 — 재시작 중일 수 있습니다.";
    if (s.status === "error") $("#updateState").textContent = "업그레이드 실패 — 로그를 확인하세요.";
  } catch (_) {
    logEl.textContent += "\n(서버 재시작 중…)";
  }
}

$("#checkUpdate").addEventListener("click", () => checkUpdate(true, true));
$("#doUpgrade").addEventListener("click", async () => {
  if (!confirm("최신 버전으로 업그레이드할까요? 완료 후 서버가 재시작됩니다.")) return;
  const logEl = $("#upgradeLog");
  logEl.hidden = false;
  logEl.textContent = "업그레이드 시작…";
  try {
    const r = await api("/api/system/upgrade", { method: "POST" });
    if (r.status === "error") { logEl.textContent = "오류: " + r.error; return; }
    if (r.status === "running") { logEl.textContent = "이미 업그레이드가 진행 중입니다."; }
    if (upgradePoll) clearInterval(upgradePoll);
    let ticks = 0;
    upgradePoll = setInterval(async () => {
      ticks++;
      await pollUpgrade();
      if (ticks > 90) { clearInterval(upgradePoll); loadVersion(); checkUpdate(false, false); }
    }, 2000);
  } catch (err) {
    logEl.textContent = "오류: " + err.message;
  }
});

let settingsData = {};
async function loadSettings() {
  loadVersion();
  checkUpdate(false, false);
  settingsData = await api("/api/settings");
  const form = $("#settingsForm");
  form.innerHTML = SETTINGS_SCHEMA.map((grp) => {
    const data = settingsData[grp.group] || {};
    const rows = grp.fields.map((f) => {
      const val = data[f.key];
      let input;
      if (f.type === "checkbox") {
        input = `<input type="checkbox" data-group="${grp.group}" data-key="${f.key}" ${val ? "checked" : ""}/>`;
      } else if (f.type === "select") {
        input = `<select data-group="${grp.group}" data-key="${f.key}">${f.options.map((o) => `<option ${o === val ? "selected" : ""}>${o}</option>`).join("")}</select>`;
      } else {
        input = `<input type="${f.type}" data-group="${grp.group}" data-key="${f.key}" value="${val == null ? "" : val}"/>`;
      }
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
    const g = el.dataset.group, k = el.dataset.key;
    let v;
    if (el.type === "checkbox") v = el.checked;
    else if (el.type === "number") v = el.value === "" ? null : Number(el.value);
    else v = el.value;
    payload[g][k] = v;
  });
  // Don't resend an unchanged (masked) password.
  if (payload.synology && payload.synology.password === "********") delete payload.synology.password;
  try {
    await api("/api/settings", { method: "PUT", body: JSON.stringify(payload) });
    $("#settingsMsg").textContent = "저장되었습니다. (주기 변경은 최대 30초 후 반영)";
    setTimeout(() => ($("#settingsMsg").textContent = ""), 4000);
    loadSettings();
  } catch (err) {
    $("#settingsMsg").textContent = "오류: " + err.message;
  }
});

// ---------------------------------------------------------------------------
// Boot
// ---------------------------------------------------------------------------
loadVersion();
loadDashboard();
setInterval(() => {
  if ($("#dashboard").classList.contains("active")) loadDashboard();
}, 30000);
