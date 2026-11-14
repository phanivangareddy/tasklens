// The key arrives in the address once; keep it in this tab only and take it
// out of the address bar, so it isn't saved in browser history or synced.
const token = (() => {
  const fromUrl = new URLSearchParams(location.search).get("t");
  try {
    if (fromUrl) { sessionStorage.setItem("tasklens-key", fromUrl); history.replaceState(null, "", location.pathname + location.hash); }
    return fromUrl || sessionStorage.getItem("tasklens-key") || "";
  } catch (e) { return fromUrl || ""; }
})();
const $ = id => document.getElementById(id);
let snap = null, frozen = false, sortKey = "mem", sortDir = -1, filter = "all", failures = 0;
// Rows hold still while the pointer is over the list, so they can't move under a click.
let overList = false, rowsStale = false;

async function api(path, body) {
  const r = await fetch(path, {
    method: body ? "POST" : "GET",
    headers: { "X-Token": token, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = r.headers.get("Content-Type")?.includes("json") ? await r.json() : await r.text();
  if (!r.ok) throw new Error(data.error || data || r.statusText);
  return data;
}

const GB = 1024 ** 3;
function bytes(n) {
  if (n >= GB) return (n / GB).toFixed(n >= 10 * GB ? 0 : 1) + " GB";
  if (n >= 1024 ** 2) return (n / 1024 ** 2).toFixed(0) + " MB";
  return (n / 1024).toFixed(0) + " KB";
}
function dur(sec) {
  sec = Math.max(0, Math.floor(sec));
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}
const esc = s => String(s).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const level = (v, warn, bad) => v >= bad ? "bad" : v >= warn ? "warn" : "";

function card(label, value, sub, pct, link) {
  const bar = pct == null ? "" :
    `<div class="bar"><i class="${level(pct, 70, 90)}" style="width:${Math.min(pct, 100)}%"></i></div>`;
  return `<div class="card${link ? " link" : ""}"${link ? ` data-go="${link}"` : ""}><div class="label">${label}</div><div class="value">${value}</div>${bar}<div class="sub">${sub}</div></div>`;
}

function renderCards(s) {
  const memPct = s.memUsed / s.memTotal * 100;
  const swapPct = s.swapTotal ? s.swapUsed / s.swapTotal * 100 : 0;
  const diskPct = s.diskTotal ? (1 - s.diskFree / s.diskTotal) * 100 : 0;
  $("cards").innerHTML =
    card("CPU", s.cpu.toFixed(0) + "%", `${s.cores} cores` + (s.load ? ` · load ${s.load[0].toFixed(1)}` : ""), s.cpu) +
    card("Memory", memPct.toFixed(0) + "%", `${bytes(s.memUsed)} of ${bytes(s.memTotal)}`, memPct) +
    card("Swap", bytes(s.swapUsed), s.swapTotal ? `of ${bytes(s.swapTotal)} · memory moved to disk` : "not in use", s.swapTotal ? swapPct : null) +
    card("Disk", bytes(s.diskFree) + " free", `of ${bytes(s.diskTotal)} · click to see what's using it`, diskPct, "disk") +
    card("Up for", dur(s.uptime), s.os, null);
  $("host").textContent = `${s.host} · ${s.os}`;
}

function renderInsights(s, procs) {
  const items = [];
  const memPct = s.memUsed / s.memTotal * 100;
  const link = p => `<a data-find="${p.pid}">${esc(p.name)}</a>`;
  const byMem = [...procs].sort((a, b) => b.mem - a.mem);
  const byCpu = [...procs].sort((a, b) => b.cpu - a.cpu);
  if (memPct >= 85)
    items.push(["bad", `Memory is ${memPct.toFixed(0)}% full, so everything slows down. Biggest users: ` +
      byMem.slice(0, 3).map(p => `${link(p)} (${bytes(p.mem)})`).join(", ") + "."]);
  if (s.swapUsed > 4 * GB || (s.swapTotal && s.swapUsed / s.swapTotal > .6))
    items.push(["warn", `${bytes(s.swapUsed)} of memory has been moved to disk (swap). Closing big apps, or a restart, clears it.`]);
  if (s.cpu >= 80)
    items.push(["bad", `The processor is ${s.cpu.toFixed(0)}% busy. Top users: ` +
      byCpu.slice(0, 3).map(p => `${link(p)} (${p.cpu.toFixed(0)}%)`).join(", ") + "."]);
  for (const p of byMem.slice(0, 5)) {
    if (p.mem / s.memTotal < .15) break;
    const share = p.mem >= s.memTotal
      ? "more than all of your memory, so the rest has been pushed to disk"
      : `${(p.mem / s.memTotal * 100).toFixed(0)}% of memory`;
    items.push(["warn", `${link(p)} is using ${bytes(p.mem)} (${share}) and has been running for ${dur((snap.time - p.started) / 1000)}.`]);
  }
  for (const p of byCpu.slice(0, 3)) {
    if (p.cpu < 25) break;
    items.push(["warn", `${link(p)} is taking ${p.cpu.toFixed(0)}% of the whole processor.`]);
  }
  if (s.diskTotal && s.diskFree / s.diskTotal < .1)
    items.push(["bad", `The disk is almost full: only ${bytes(s.diskFree)} free.`]);
  if (!items.length) items.push(["ok", "Everything looks healthy right now."]);
  $("insights").innerHTML = items.map(([c, t]) => `<li class="${c}">${t}</li>`).join("");
}

function visible(procs) {
  const q = $("search").value.trim().toLowerCase();
  const total = snap.system.memTotal;
  return procs.filter(p => {
    if (filter === "mine" && p.system) return false;
    if (filter === "heavy" && !(p.cpu >= 10 || p.mem / total >= .03)) return false;
    if (filter === "paused" && !p.paused) return false;
    if (!q) return true;
    return String(p.pid) === q || p.name.toLowerCase().includes(q) ||
      p.user.toLowerCase().includes(q) || p.cmd.toLowerCase().includes(q);
  }).sort((a, b) => {
    let x = a[sortKey], y = b[sortKey];
    if (sortKey === "started") { x = -x; y = -y; }  // "running for" grows as start time shrinks
    if (typeof x === "string") return sortDir * x.localeCompare(y);
    return sortDir * (x - y);
  });
}

function renderRows() {
  const total = snap.system.memTotal;
  const list = visible(snap.procs);
  $("count").textContent = `${list.length} of ${snap.procs.length} processes`;
  document.querySelectorAll("th[data-key]").forEach(th => {
    th.classList.toggle("sorted", th.dataset.key === sortKey);
    th.textContent = th.textContent.replace(/ [▲▼]$/, "") + (th.dataset.key === sortKey ? (sortDir < 0 ? " ▼" : " ▲") : "");
  });
  $("rows").innerHTML = list.map(p => {
    const cpuHeat = p.cpu >= 50 ? 3 : p.cpu >= 15 ? 2 : 0;
    const memHeat = p.mem / total >= .2 ? 3 : p.mem / total >= .05 ? 2 : 0;
    const cmd = p.cmd && p.cmd !== p.name ? `<small title="${esc(p.cmd)}">${esc(p.cmd)}</small>` : "";
    return `<tr>
      <td class="name" title="${esc(p.name)}">${esc(p.name)}${p.paused ? '<span class="badge paused">paused</span>' : ""}${p.system ? '<span class="badge">system</span>' : ""}${cmd}</td>
      <td class="num">${p.pid}</td>
      <td class="num heat-${cpuHeat}">${p.cpu.toFixed(1)}%</td>
      <td class="num heat-${memHeat}">${bytes(p.mem)}</td>
      <td class="num">${dur((snap.time - p.started) / 1000)}</td>
      <td class="num opt">${dur(p.cpuSec)}</td>
      <td class="opt">${esc(p.user || "—")}</td>
      <td class="actions">
        <button class="small" data-act="${p.paused ? "resume" : "pause"}" data-pid="${p.pid}">${p.paused ? "Resume" : "Pause"}</button>
        <button class="small danger" data-act="end" data-pid="${p.pid}">End</button>
        <button class="small danger" data-act="force" data-pid="${p.pid}" title="Stop immediately, without letting it save">Force</button>
      </td></tr>`;
  }).join("") || `<tr><td colspan="8" class="gone">No matching processes.</td></tr>`;
}

async function refresh() {
  try {
    const next = await api("/api/snapshot");
    failures = 0;
    $("live").classList.remove("off");
    $("liveText").textContent = frozen ? "Frozen" : "Live";
    if (frozen && snap) return;
    snap = next;
    renderCards(snap.system);
    renderInsights(snap.system, snap.procs);
    if (overList || $("modal").classList.contains("show")) rowsStale = true;
    else renderRows();
  } catch (e) {
    if (++failures >= 3) {
      $("live").classList.add("off");
      $("liveText").textContent = "Disconnected";
    }
  }
}

function toast(msg) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(t._h);
  t._h = setTimeout(() => t.classList.remove("show"), 5000);
}

const verbs = {
  end:    ["End", "It will be asked to close, so it can save its work first."],
  force:  ["Force end", "It will be stopped immediately. Anything unsaved will be lost."],
  pause:  ["Pause", "It will be frozen in place: no processor use, nothing lost. Its memory stays reserved until you resume or end it."],
  resume: ["Resume", "It will carry on from where it was paused."],
};

function confirmAction(p, act) {
  const [verb, explain] = verbs[act];
  $("mTitle").textContent = `${verb} ${p.name}?`;
  $("mBody").textContent = `PID ${p.pid} · ${bytes(p.mem)} memory · running for ${dur((snap.time - p.started) / 1000)}. ${explain}`;
  $("mWarn").textContent = p.system && act !== "resume"
    ? `This process belongs to ${p.user || "the system"}. Stopping system processes can make apps or the whole computer misbehave.` : "";
  $("mOk").textContent = verb;
  $("mOk").className = act === "resume" ? "" : "danger solid";
  $("modal").classList.add("show");
  $("mOk").onclick = async () => {
    $("modal").classList.remove("show");
    try {
      await api("/api/action", { pid: p.pid, action: act });
      toast(`${verb}: ${p.name} (PID ${p.pid}) done.`);
      setTimeout(refresh, 500);
    } catch (e) { toast(e.message); }
  };
}

$("mCancel").onclick = () => $("modal").classList.remove("show");
$("modal").onclick = e => { if (e.target === $("modal")) $("modal").classList.remove("show"); };
document.addEventListener("keydown", e => { if (e.key === "Escape") $("modal").classList.remove("show"); });

const listEl = document.querySelector(".tablewrap");
listEl.addEventListener("mouseenter", () => { overList = true; });
listEl.addEventListener("mouseleave", () => {
  overList = false;
  if (rowsStale && !$("modal").classList.contains("show")) { rowsStale = false; renderRows(); }
});

$("rows").addEventListener("click", e => {
  const b = e.target.closest("button[data-act]");
  if (!b) return;
  const p = snap.procs.find(x => x.pid === +b.dataset.pid);
  if (p) confirmAction(p, b.dataset.act);
});
$("insights").addEventListener("click", e => {
  const a = e.target.closest("a[data-find]");
  if (!a) return;
  $("search").value = a.dataset.find;
  renderRows();
  $("search").scrollIntoView({ behavior: "smooth", block: "start" });
});
document.querySelectorAll("th[data-key]").forEach(th => th.addEventListener("click", () => {
  const k = th.dataset.key;
  if (sortKey === k) sortDir = -sortDir;
  else { sortKey = k; sortDir = (k === "name" || k === "user") ? 1 : -1; }
  renderRows();
}));
document.querySelectorAll(".chip").forEach(c => c.addEventListener("click", () => {
  document.querySelectorAll(".chip").forEach(x => x.classList.remove("on"));
  c.classList.add("on");
  filter = c.dataset.filter;
  renderRows();
}));
$("search").addEventListener("input", () => snap && renderRows());
$("pauseBtn").onclick = () => {
  frozen = !frozen;
  $("pauseBtn").textContent = frozen ? "Unfreeze" : "Freeze";
  $("liveText").textContent = frozen ? "Frozen" : "Live";
  if (!frozen) refresh();
};
$("quitBtn").onclick = async () => {
  try { await api("/api/quit", {}); } catch (e) {}
  document.body.innerHTML = '<div class="gone"><h2>TaskLens has closed.</h2><p>Open it again from your apps whenever you need it.</p></div>';
};

// ---------------------------------------------------------------- Disk view
const isMac = /Mac/.test(navigator.platform), isWin = /Win/.test(navigator.platform);
const revealLabel = isMac ? "Show in Finder" : isWin ? "Show in Explorer" : "Open folder";
let dview = null, dpathShown = "", dpoll = null, placesList = [];

// Plain-English notes for folders that are often big, and whether they're safe to clear.
const hints = [
  [/^huggingface$/i, "AI models downloaded by Hugging Face tools. Models you no longer use can be deleted; they download again if needed."],
  [/^models--/i, "One downloaded AI model."],
  [/^\.cache$/i, "Caches made by apps and developer tools. Usually safe to clear: they rebuild when needed."],
  [/^uv$/i, "Python package cache (uv). Clear it with: uv cache clean"],
  [/^pip$/i, "Python package cache (pip). Clear it with: pip cache purge"],
  [/^caches$/i, "App caches. Usually safe to clear."],
  [/^homebrew$/i, "Homebrew downloads. Clear with: brew cleanup --prune=all"],
  [/^go-build$/i, "Go build cache. Clear with: go clean -cache"],
  [/^node_modules$/i, "JavaScript packages for one project. Safe to delete; npm install brings them back."],
  [/^\.?venv$/i, "Python environment for one project. Safe to delete; it can be recreated."],
  [/^\.gradle$/i, "Gradle/Android build cache. Safe to clear; builds re-download what they need."],
  [/^\.android$/i, "Android emulator images and settings. Delete emulators you don't use from Android Studio."],
  [/^\.npm$/i, "npm package cache. Clear with: npm cache clean --force"],
  [/^\.ollama$/i, "Local AI models for Ollama. Remove unused ones with: ollama rm <name>"],
  [/^deriveddata$/i, "Xcode build files. Safe to delete."],
  [/^\.trash$|^\$recycle\.bin$/i, "The Trash. Empty it to get this space back."],
  [/^downloads$/i, "Downloads. Old installers, videos and zip files pile up here."],
  [/^vm_bundles$/i, "The Claude app's built-in virtual machine."],
  [/^docker$|^com\.docker\.docker$/i, "Docker images and containers. Clear unused ones with: docker system prune"],
  [/^library$/i, "Settings, caches and data that apps keep for you."],
  [/^application support$/i, "Data that apps keep for you. Big folders here belong to the app they're named after."],
  [/^checkpoints$/i, "Saved AI model weights."],
  [/^output$|^renders?$/i, "Generated output. Old renders can be archived or deleted."],
];
const hintFor = name => (hints.find(([re]) => re.test(name)) || [, ""])[1];

function ago(ms) {
  if (!ms) return "";
  const d = (Date.now() - ms) / 86400000;
  if (d < 1) return "today";
  if (d < 2) return "yesterday";
  if (d < 60) return `${Math.floor(d)} days ago`;
  if (d < 730) return `${Math.floor(d / 30)} months ago`;
  return `${Math.floor(d / 365)} years ago`;
}
const num = n => (n || 0).toLocaleString();

function showTab(tab) {
  document.querySelectorAll(".tabs button").forEach(b => b.classList.toggle("on", b.dataset.tab === tab));
  $("procView").hidden = tab !== "procs";
  $("diskView").hidden = tab !== "disk";
  $("alertsView").hidden = tab !== "alerts";
  $("reportView").hidden = tab !== "report";
  history.replaceState(null, "", location.pathname + (tab === "procs" ? "" : "#" + tab));
  if (tab === "disk") openDisk();
  if (tab === "alerts") openAlerts();
  if (tab === "report") openReport();
}

async function openDisk() {
  if (!placesList.length) {
    try { placesList = await api("/api/disk/places"); } catch (e) {}
    $("places").innerHTML = placesList.map((p, i) =>
      `<button class="chip" data-place="${i}" title="${esc(p.path)}">${esc(p.label)}${p.total && i ? ` · ${bytes(p.total - p.free)} used` : ""}</button>`).join(" ");
  }
  const v = await loadDisk(dpathShown);
  if (v && v.state === "none") startScan(placesList[0] ? placesList[0].path : "");
}

async function startScan(path) {
  try {
    await api("/api/disk/scan", { path });
    dpathShown = "";
    loadDisk("");
  } catch (e) { toast(e.message); }
}

async function loadDisk(path) {
  let v;
  try { v = await api("/api/disk?path=" + encodeURIComponent(path || "")); }
  catch (e) { toast(e.message); return null; }
  dview = v;
  clearTimeout(dpoll);
  if (v.state === "scanning") dpoll = setTimeout(() => loadDisk(path), 1000);
  else if (v.state === "done") dpathShown = v.path;
  renderDisk(v);
  return v;
}

function renderDisk(v) {
  document.querySelectorAll("[data-place]").forEach(b =>
    b.classList.toggle("on", !!v.root && placesList[+b.dataset.place] && (v.label === placesList[+b.dataset.place].label)));
  const st = [];
  if (v.state === "none") { $("dstatus").innerHTML = "<li>Pick a place to scan.</li>"; return; }
  if (v.state === "scanning") {
    $("dhead").textContent = `Scanning ${v.label}…`;
    $("dstatus").innerHTML = `<li class="ok"><span class="spin"></span>${num(v.scanned)} files, ${bytes(v.bytes)} so far · ${dur(v.elapsed)}. ` +
      `Big folders take a minute or two; the computer stays usable meanwhile.</li>`;
    $("crumbs").innerHTML = ""; $("drows").innerHTML = ""; $("dbig").innerHTML = "";
    return;
  }
  $("dhead").textContent = "Where your disk space went";
  const top = v.items.filter(i => i.dir).slice(0, 3);
  st.push(["ok", `<b>${esc(v.crumbs[v.crumbs.length - 1].name)}</b> uses <b>${bytes(v.size)}</b> in ${num(v.files)} files.` +
    (top.length ? ` Biggest: ` + top.map(i => `<a data-open="${esc(i.path)}">${esc(i.name)}</a> (${bytes(i.size)})`).join(", ") + "." : "")]);
  if (v.path === v.root && v.volTotal) {
    const other = v.volUsed - v.size;
    st.push(["ok", `The disk has ${bytes(v.volUsed)} used of ${bytes(v.volTotal)}. This scan accounts for ${bytes(v.size)}` +
      (other > GB ? `; the other ${bytes(other)} is outside ${esc(v.label === "Whole disk" ? "the scanned area" : "this folder")}` +
        (v.label === "Whole disk" ? " (the operating system itself, swap, and space the system has reserved for snapshots)." : ". Scan Whole disk to see it.") : ".")]);
  }
  if (v.denied)
    st.push(["warn", `${num(v.denied)} folders couldn't be read, so they're not counted.` +
      (isMac ? " To include them, add TaskLens in System Settings → Privacy & Security → Full Disk Access." :
        isWin ? " Run TaskLens as administrator to include them." : " Run TaskLens with sudo to include them.")]);
  st.push(["ok", `Scanned in ${dur(v.elapsed)}. Click any folder to look inside it.`]);
  $("dstatus").innerHTML = st.map(([c, t]) => `<li class="${c}">${t}</li>`).join("");

  $("crumbs").innerHTML = v.crumbs.map((c, i) => i === v.crumbs.length - 1
    ? `<b>${esc(c.name)}</b>` : `<a data-open="${esc(c.path)}">${esc(c.name)}</a><span>›</span>`).join("");

  const parent = v.size || 1;
  $("drows").innerHTML = v.items.map(i => {
    const pct = i.size / parent * 100;
    const heat = i.size >= 50 * GB ? 3 : i.size >= 10 * GB ? 2 : 0;
    const name = i.other ? `${num(i.files)} smaller files` : esc(i.name);
    const hint = i.dir ? hintFor(i.name) : "";
    const note = i.denied ? "Couldn't be read" : hint;
    return `<tr class="${i.dir && !i.denied ? "drow" : ""}" ${i.dir && !i.denied ? `data-open="${esc(i.path)}"` : ""}>
      <td class="dname" title="${esc(i.path)}"><span class="ic">${i.dir ? "📁" : i.other ? "…" : "📄"}</span>${name}${note ? `<small>${esc(note)}</small>` : ""}</td>
      <td class="num heat-${heat}">${bytes(i.size)}</td>
      <td class="share"><div class="bar"><i class="${i.dir ? "" : "f"}" style="width:${Math.max(pct, .5)}%"></i></div>
        <small style="color:var(--muted)">${pct >= 1 ? pct.toFixed(0) : "<1"}%</small></td>
      <td class="num opt">${i.dir ? num(i.files) : ""}</td>
      <td class="num opt">${ago(i.mod)}</td>
      <td>${i.other ? "" : `<button class="small" data-reveal="${esc(i.path)}">${revealLabel}</button>`}</td></tr>`;
  }).join("") || `<tr><td colspan="6" class="gone">This folder is empty.</td></tr>`;

  const base = v.path.endsWith(v.sep) ? v.path : v.path + v.sep;
  $("dbig").innerHTML = v.largest.map(f => {
    const rel = f.path.startsWith(base) ? f.path.slice(base.length) : f.path;
    const dir = rel.includes(v.sep) ? rel.slice(0, rel.lastIndexOf(v.sep)) : "";
    const heat = f.size >= 10 * GB ? 3 : f.size >= 2 * GB ? 2 : 0;
    return `<tr><td class="dname" title="${esc(f.path)}">📄 ${esc(f.name)}${dir ? `<small>in ${esc(dir)}</small>` : ""}</td>
      <td class="num heat-${heat}">${bytes(f.size)}</td><td class="num opt">${ago(f.mod)}</td>
      <td><button class="small" data-reveal="${esc(f.path)}">${revealLabel}</button></td></tr>`;
  }).join("") || `<tr><td colspan="4" class="gone">No files.</td></tr>`;
}

document.querySelectorAll(".tabs button").forEach(b => b.onclick = () => showTab(b.dataset.tab));
$("cards").addEventListener("click", e => { if (e.target.closest("[data-go=disk]")) showTab("disk"); });
$("diskView").addEventListener("click", async e => {
  const rv = e.target.closest("[data-reveal]");
  if (rv) {
    e.stopPropagation();
    try { await api("/api/disk/reveal", { path: rv.dataset.reveal }); } catch (err) { toast(err.message); }
    return;
  }
  const pl = e.target.closest("[data-place]");
  if (pl) return startScan(placesList[+pl.dataset.place].path);
  const op = e.target.closest("[data-open]");
  if (op && dview && dview.state === "done") {
    await loadDisk(op.dataset.open);
    $("crumbs").scrollIntoView({ behavior: "smooth", block: "nearest" });
  }
});
$("dscan").onclick = () => { const p = $("dpath").value.trim(); if (p) startScan(p); else toast("Type a folder path first, or pick a place."); };
$("dpath").addEventListener("keydown", e => { if (e.key === "Enter") $("dscan").click(); });

// ---------------------------------------------------------------- Alerts view
let cfg = null, cfgMeta = null, saveTimer = null;
const ruleText = {
  memForecast:  ["Early warning: memory will run out within {forecastMin} minutes", true],
  memHigh:      ["Memory is more than {memPct}% full for a minute", true],
  appHigh:      ["One app uses more than {appPct}% of memory", false],
  swapHigh:     ["Memory pushed to disk (swap) is more than {swapPct}% of RAM", false],
  cpuHigh:      ["Processor more than {cpuPct}% busy for {cpuMin} minutes", false],
  diskLow:      ["Less than {diskFreePct}% of the disk is free", false],
  diskForecast: ["The disk will be full within {diskDays} days, at the current rate", false],
};
const provHelp = {
  gmail: `<b>Gmail needs an app password</b> (your normal password won't work):<ol>
    <li>Turn on 2-Step Verification for your Google account, if it isn't on already.</li>
    <li>Open <a href="https://myaccount.google.com/apppasswords" target="_blank" rel="noopener">myaccount.google.com/apppasswords</a>.</li>
    <li>Type <b>TaskLens</b> as the name and click <b>Create</b>.</li>
    <li>Copy the 16-letter password into the box above and click <b>Save password</b>.</li></ol>`,
  icloud: `<b>iCloud Mail needs an app-specific password</b>:<ol>
    <li>Sign in at <a href="https://account.apple.com" target="_blank" rel="noopener">account.apple.com</a> → <b>Sign-In and Security</b> → <b>App-Specific Passwords</b>.</li>
    <li>Click <b>+</b>, name it <b>TaskLens</b>, and copy the password it shows.</li>
    <li>Paste it above and click <b>Save password</b>. Use your @icloud.com address as the sender.</li></ol>`,
  yahoo: `<b>Yahoo Mail needs an app password</b>:<ol>
    <li>Open Yahoo <b>Account Info</b> → <b>Account Security</b> → <b>Generate app password</b>.</li>
    <li>Name it <b>TaskLens</b>, copy the password, paste it above and click <b>Save password</b>.</li></ol>`,
  custom: `Enter your provider's outgoing (SMTP) server. Port <b>587</b> (STARTTLS) or <b>465</b> (SSL) both work;
    TaskLens always encrypts the connection. Use an app password if your provider offers one.
    Outlook.com and Hotmail personal accounts no longer accept app sign-ins like this; use Gmail, iCloud or Yahoo instead.`,
};

async function openAlerts() {
  try {
    const v = await api("/api/settings");
    cfg = v.settings; cfgMeta = v;
  } catch (e) { toast(e.message); return; }
  if (cfgMeta.os === "windows" || cfgMeta.os === "linux") document.querySelectorAll(".trayname").forEach(x => x.textContent = "notification area (tray)");
  renderAlerts();
  loadAlertLog();
}

function renderAlerts() {
  const L = cfg.limits;
  $("sPopups").checked = cfg.popups;
  $("sEmail").checked = cfg.email.enabled;
  $("sBg").checked = cfg.background;
  $("sLogin").checked = cfg.startAtLogin;
  document.querySelectorAll("#sens button").forEach(b => b.classList.toggle("on", b.dataset.sens === cfg.sensitivity));
  $("rules").innerHTML = cfgMeta.rules.map(r => {
    const [text, urgent] = ruleText[r];
    const body = esc(text).replace(/\{(\w+)\}/g, (_, k) => `<input class="n" type="number" data-lim="${k}" value="${L[k]}">`);
    return `<label class="row"><input type="checkbox" data-rule="${r}" ${cfg.rules[r] ? "checked" : ""}>
      <span>${body}<span class="tag ${urgent ? "urgent" : ""}">${urgent ? "urgent" : "warning"}</span></span></label>`;
  }).join("") + `<label class="row"><span style="margin-left:24px">Don't repeat the same alert within
      <input class="n" type="number" data-lim="cooldownMin" value="${L.cooldownMin}"> minutes</span></label>`;
  const e = cfg.email;
  $("eProv").value = e.provider || "gmail";
  $("eHost").value = e.host || ""; $("ePort").value = e.port || "";
  $("eUser").value = e.username || "";
  document.querySelectorAll(".cust").forEach(x => x.hidden = $("eProv").value !== "custom");
  $("eHelp").innerHTML = provHelp[$("eProv").value];
  $("pwState").innerHTML = cfgMeta.hasPassword
    ? `<span class="ok-text">✓ Saved${cfgMeta.keychain ? (isMac ? " in Keychain" : isWin ? " in Credential Manager" : " in the keyring") : " (in TaskLens settings; no keychain found)"}</span>`
    : `<span class="bad-text">Not saved yet</span>`;
  $("people").innerHTML = (e.recipients || []).map((r, i) => `<div class="person">
      <span class="em">${esc(r.email)}</span>
      <label class="switch"><input type="checkbox" data-crit="${i}" ${r.criticalOnly ? "checked" : ""}> Urgent alerts only</label>
      <button class="small danger" data-rm="${i}">Remove</button></div>`).join("")
    || `<div style="color:var(--muted)">Nobody yet. Add yourself and anyone else who should know.</div>`;
}

function scheduleSave() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(saveSettings, 500);
}
async function saveSettings() {
  try {
    const v = await api("/api/settings", cfg);
    cfg = v.settings; cfgMeta = v;
    renderAlerts();
    toast("Saved.");
  } catch (e) {
    toast(e.message);
    openAlerts(); // put the page back to what is really saved
  }
}

async function loadAlertLog() {
  try {
    const list = await api("/api/alerts");
    if (!list.length) return;
    $("alog").innerHTML = list.slice(0, 30).map(a => {
      const when = new Date(a.time).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit", day: "numeric", month: "short" });
      const mail = a.emailed === "sent" ? ' · <span class="ok-text">emailed</span>' : a.emailed ? ` · <span class="bad-text">email ${esc(a.emailed)}</span>` : "";
      return `<li class="${a.critical ? "bad" : "warn"}"><span class="when">${when}${mail}</span><b>${esc(a.title)}</b>
        <span class="body">${esc(a.body)}</span></li>`;
    }).join("");
  } catch (e) {}
}

$("alertsView").addEventListener("change", e => {
  const t = e.target;
  if (!cfg) return;
  if (t.dataset.rule) cfg.rules[t.dataset.rule] = t.checked;
  else if (t.dataset.lim) {
    const v = parseFloat(t.value);
    if (!isFinite(v)) return toast("Please enter a number.");
    cfg.limits[t.dataset.lim] = v;
    cfg.sensitivity = "custom";
  }
  else if (t.dataset.crit) cfg.email.recipients[+t.dataset.crit].criticalOnly = t.checked;
  else if (t.id === "sPopups") cfg.popups = t.checked;
  else if (t.id === "sEmail") cfg.email.enabled = t.checked;
  else if (t.id === "sBg") cfg.background = t.checked;
  else if (t.id === "sLogin") cfg.startAtLogin = t.checked;
  else if (t.id === "eProv") {
    cfg.email.provider = t.value;
    const p = cfgMeta.providers[t.value];
    if (p) { cfg.email.host = p.Host; cfg.email.port = p.Port; }
    renderAlerts();
  }
  else if (t.id === "eHost") cfg.email.host = t.value.trim();
  else if (t.id === "ePort") cfg.email.port = parseInt(t.value) || 0;
  else if (t.id === "eUser") cfg.email.username = t.value.trim();
  else return;
  scheduleSave();
});
$("alertsView").addEventListener("click", async e => {
  const b = e.target.closest("button");
  if (!b || !cfg) return;
  if (b.dataset.sens) { cfg.sensitivity = b.dataset.sens; cfg.limits = cfgMeta.presets[b.dataset.sens]; renderAlerts(); scheduleSave(); }
  else if (b.dataset.rm) { cfg.email.recipients.splice(+b.dataset.rm, 1); renderAlerts(); scheduleSave(); }
  else if (b.id === "addPerson") {
    const em = $("newPerson").value.trim();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(em)) return toast("That doesn't look like an email address.");
    cfg.email.recipients = [...(cfg.email.recipients || []), { email: em, criticalOnly: false }];
    $("newPerson").value = "";
    renderAlerts(); scheduleSave();
  }
  else if (b.id === "eSavePw") {
    const user = $("eUser").value.trim(), pw = $("ePass").value;
    if (!user) return toast("Enter the sender email address first.");
    if (!pw) return toast("Paste the app password first.");
    cfg.email.username = user;
    try {
      await api("/api/settings", cfg);
      await api("/api/settings/password", { user, password: pw });
      $("ePass").value = "";
      await openAlerts();
      toast("Password saved securely.");
    } catch (err) { toast(err.message); }
  }
  else if (b.id === "tEmail") {
    $("emailState").innerHTML = '<span class="spin"></span>Sending…';
    try {
      await api("/api/alerts/test-email", {});
      $("emailState").innerHTML = `<span class="ok-text">✓ Sent. Check the inbox (and spam folder) of everyone on the list.</span>`;
    } catch (err) { $("emailState").innerHTML = `<span class="bad-text">${esc(err.message)}</span>`; }
  }
  else if (b.id === "tWarn") { api("/api/alerts/test-popup", {}).catch(err => toast(err.message)); toast("A test notification is on its way."); }
  else if (b.id === "tCrit") { api("/api/alerts/test-popup?critical=1", {}).catch(err => toast(err.message)); toast("A test pop-up is on its way."); }
});
$("newPerson").addEventListener("keydown", e => { if (e.key === "Enter") $("addPerson").click(); });

// ---------------------------------------------------------------- report
let rDays = 14, report = null;
const fmtWhen = ms => new Date(ms).toLocaleString([], { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
const fmtTime = ms => new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
function span(ms) {
  const m = Math.round(ms / 60000);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} h ${m % 60 ? (m % 60) + " min" : ""}`.trim();
  return `${Math.round(h / 24)} days`;
}
const kindLabel = { normal: "Normal", update: "Update", crash: "Crash", unexpected: "Unexpected", unknown: "Unknown",
  memory: "Out of memory", limit: "Memory limit", hang: "Not responding", cpu: "Heavy processor", disk: "Heavy disk", system: "System crash" };

async function openReport() {
  try {
    const v = await api("/api/settings");
    cfg = v.settings; cfgMeta = v;
    $("sHist").checked = cfg.history; $("sWeekly").checked = cfg.email.weekly;
  } catch (e) {}
  loadReport();
}

async function loadReport() {
  document.querySelectorAll("#rDays button").forEach(b => b.classList.toggle("on", +b.dataset.days === rDays));
  try { report = await api("/api/report?days=" + rDays); } catch (e) { toast(e.message); return; }
  const r = report;
  for (const k of ["series", "gaps", "apps", "slow", "restarts", "problems", "summary"]) r[k] = r[k] || [];
  $("rSummary").innerHTML = r.summary.map(s => `<li>${esc(s)}</li>`).join("");
  drawChart(r);
  $("rRestarts").innerHTML = r.restarts.map(x => {
    const off = x.down ? `<span class="sub">Went down about ${fmtWhen(x.down)} · off for ${x.boot - x.down < 90000 ? "about a minute" : span(x.boot - x.down)}</span>` : "";
    return `<li><span class="kind ${x.kind}">${kindLabel[x.kind] || x.kind}</span><span class="when">${fmtWhen(x.boot)}</span>${esc(x.reason)}
      ${off}${x.before ? `<span class="sub">${esc(x.before)}</span>` : ""}${x.note ? `<span class="sub">${esc(x.note)}</span>` : ""}</li>`;
  }).join("") || `<li class="empty">No restarts in this period.</li>`;
  $("rSlow").innerHTML = r.slow.map(s => `<tr><td>${fmtWhen(s.from)} – ${fmtTime(s.to)}</td><td>${span(s.to - s.from)}</td>
      <td>${s.kind === "cpu" ? "Processor" : "Memory"} up to ${Math.round(s.peak)}%</td>
      <td class="wrap">${s.app ? `<b>${esc(s.app)}</b> was using ${esc(s.appUse)}` : ""}</td></tr>`).join("")
    || `<tr><td colspan="4" class="empty">${r.hours >= 1 ? "No slow periods. Memory and processor stayed under 90%." : "No history recorded yet."}</td></tr>`;
  $("rApps").innerHTML = r.apps.map(a => `<tr><td class="name">${esc(a.name)}</td>
      <td class="share"><div class="bar"><i style="width:${Math.min(100, a.share)}%"></i></div> ${a.share.toFixed(a.share < 10 ? 1 : 0)}%</td>
      <td class="num">${a.cpuMin >= 60 ? (a.cpuMin / 60).toFixed(1) + " h" : Math.round(a.cpuMin) + " min"}</td>
      <td class="num">${bytes(a.peakMem)}</td><td class="opt">${fmtWhen(a.peakAt)}</td></tr>`).join("")
    || `<tr><td colspan="5" class="empty">No history recorded yet. It fills in every 5 minutes while TaskLens runs.</td></tr>`;
  $("rProblems").innerHTML = r.problems.map(p => {
    const times = p.count > 1 ? ` <span class="badge">${p.count} times, until ${fmtTime(p.last)}</span>` : "";
    return `<li><span class="kind ${p.kind}">${kindLabel[p.kind] || p.kind}</span><span class="when">${fmtWhen(p.time)}</span>${esc(p.title)}${times}
      ${p.detail ? `<span class="sub">${esc(p.detail)}</span>` : ""}</li>`;
  }).join("") || `<li class="empty">Nothing recorded. No crashes, hangs or memory shortages.</li>`;
}

function drawChart(r) {
  const W = 1000, H = 230, L = 34, R = 8, T = 10, B = 24;
  const x = t => L + (t - r.from) / (r.to - r.from) * (W - L - R);
  const y = v => T + (1 - Math.min(100, v) / 100) * (H - T - B);
  let g = "";
  for (const v of [0, 50, 100]) g += `<line class="grid" x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}"/><text class="axis" x="${L - 6}" y="${y(v) + 4}" text-anchor="end">${v}%</text>`;
  const day = 86400000, start = new Date(r.from); start.setHours(0, 0, 0, 0);
  const every = r.days > 7 ? 2 : 1;
  for (let t = start.getTime() + day, i = 0; t < r.to; t += day, i++) {
    if (r.days === 1) break;
    g += `<line class="grid" x1="${x(t)}" x2="${x(t)}" y1="${T}" y2="${H - B}" stroke-dasharray="2 3"/>`;
    if (i % every === 0) g += `<text class="axis" x="${x(t)}" y="${H - 7}" text-anchor="middle">${new Date(t).toLocaleDateString([], { day: "numeric", month: "short" })}</text>`;
  }
  if (r.days === 1) for (let t = Math.ceil(r.from / (3 * 3600000)) * 3 * 3600000; t < r.to; t += 3 * 3600000)
    g += `<text class="axis" x="${x(t)}" y="${H - 7}" text-anchor="middle">${fmtTime(t)}</text>`;
  const band = (a, b, fill) => `<rect x="${x(a)}" y="${T}" width="${Math.max(1, x(b) - x(a))}" height="${H - T - B}" fill="${fill}"/>`;
  let bands = r.gaps.map(([a, b]) => band(a, b, "var(--gap)")).join("") + r.slow.map(s => band(s.from, s.to, "var(--hot)")).join("");
  // Break the lines at gaps so a night with the computer off isn't drawn as a straight line.
  const line = (key, color) => {
    let d = "", prev = 0;
    const broken = (a, b) => r.gaps.some(([g0, g1]) => g0 >= a && g1 <= b + 300000);
    for (const p of r.series) { d += (!d || broken(prev, p.t) ? "M" : "L") + x(p.t).toFixed(1) + " " + y(p[key]).toFixed(1); prev = p.t; }
    return `<path d="${d}" fill="none" stroke="${color}" stroke-width="1.6" stroke-linejoin="round"/>`;
  };
  const marks = r.restarts.filter(x0 => x0.boot >= r.from).map(b => `<line x1="${x(b.boot)}" x2="${x(b.boot)}" y1="${T}" y2="${H - B}" stroke="var(--bad)" stroke-width="2"/>`).join("");
  const empty = r.series.length ? "" : `<text class="axis" x="${W / 2}" y="${H / 2}" text-anchor="middle" style="font-size:14px">No history yet: it fills in every 5 minutes while TaskLens runs.</text>`;
  $("rChart").innerHTML = `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${bands}${g}${line("mem", "var(--mem)")}${line("cpu", "var(--cpu)")}${marks}${empty}
    <line id="rCursor" y1="${T}" y2="${H - B}" stroke="var(--muted)" stroke-width="1" visibility="hidden"/></svg><div class="tip" id="rTip"></div>`;
  const svg = $("rChart").querySelector("svg"), tip = $("rTip"), cur = $("rCursor");
  svg.onmousemove = e => {
    if (!r.series.length) return;
    const box = svg.getBoundingClientRect(), px = (e.clientX - box.left) / box.width * W;
    const t = r.from + (px - L) / (W - L - R) * (r.to - r.from);
    let best = r.series[0];
    for (const p of r.series) if (Math.abs(p.t - t) < Math.abs(best.t - t)) best = p;
    if (Math.abs(best.t - t) > 40 * 60000 * Math.max(1, r.days / 3)) { tip.style.display = "none"; cur.setAttribute("visibility", "hidden"); return; }
    cur.setAttribute("x1", x(best.t)); cur.setAttribute("x2", x(best.t)); cur.setAttribute("visibility", "visible");
    tip.innerHTML = `<b>${fmtWhen(best.t)}</b><br>Processor ${Math.round(best.cpu)}% (peak ${Math.round(best.cpuMax)}%)<br>Memory ${Math.round(best.mem)}% (peak ${Math.round(best.memMax)}%)` +
      (best.swap >= GB ? `<br>Moved to disk: ${bytes(best.swap)}` : "");
    tip.style.display = "block";
    const left = (x(best.t) / W) * box.width;
    tip.style.left = Math.min(box.width - tip.offsetWidth, Math.max(0, left - tip.offsetWidth / 2)) + "px";
    tip.style.top = "0px";
  };
  svg.onmouseleave = () => { tip.style.display = "none"; cur.setAttribute("visibility", "hidden"); };
}

$("reportView").addEventListener("click", async e => {
  const b = e.target.closest("button");
  if (!b) return;
  if (b.dataset.days) { rDays = +b.dataset.days; loadReport(); }
  else if (b.id === "rPrint") window.print();
  else if (b.id === "rEmail") {
    b.disabled = true; toast("Sending the report…");
    try { await api("/api/report/email?days=" + rDays, {}); toast("Report emailed to everyone on your list."); }
    catch (err) { toast(err.message + " (set up email in the Alerts tab)"); }
    b.disabled = false;
  }
});
$("reportView").addEventListener("change", async e => {
  if (!cfg) return;
  if (e.target.id === "sHist") cfg.history = e.target.checked;
  else if (e.target.id === "sWeekly") cfg.email.weekly = e.target.checked;
  else return;
  try { const v = await api("/api/settings", cfg); cfg = v.settings; cfgMeta = v; toast("Saved."); }
  catch (err) { toast(err.message); openReport(); }
});

refresh();
setInterval(refresh, 2000);
if (location.hash === "#disk") showTab("disk");
if (location.hash === "#alerts") showTab("alerts");
if (location.hash === "#report") showTab("report");
