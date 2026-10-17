"use strict";

const TAB_IDS = ["dashboard", "queries", "domains", "hosts", "clients", "lists", "gravity", "drift", "settings"];
let currentLanguage = localStorage.getItem("ph_combo_language") || (navigator.language || "en").split("-")[0];
if (!UI_LANGUAGES.includes(currentLanguage)) currentLanguage = "en";
const state = {servers: desktopConfig.servers.map(s => ({...s, connected:false, checking:!!s.password})), activeTab:"dashboard", page:null, ready:false};
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const escapeHtml = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;", "'":"&#39;"})[c]);
const localeCode = () => ({en:"en-GB", da:"da-DK", el:"el-GR", sv:"sv-SE"})[currentLanguage];
const fmtNum = value => value == null ? "–" : Number(value).toLocaleString(localeCode());
const fmtDate = value => Number(value) ? new Date(Number(value)*1000).toLocaleString(localeCode()) : t("never");
function t(key, values = {}) {
    if (!MESSAGES[key]) throw new Error("Missing UI message: " + key);
    return MESSAGES[key][UI_LANGUAGES.indexOf(currentLanguage)].replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? ""));
}
function preference(key, fallback) {
    try { return JSON.parse(localStorage.getItem("ph_combo_native_" + key)) ?? fallback; } catch { return fallback; }
}
function savePreference(key, value) { localStorage.setItem("ph_combo_native_" + key, JSON.stringify(value)); }
function textHTML(key, values) { return escapeHtml(t(key, values)); }
function setHTML(element, html) { if (element && element.innerHTML !== html) element.innerHTML = html; }
function humanError(error) {
    const message = String(error?.message || error);
    if (message.includes("cannot reach Pi-hole")) return t("networkError");
    if (message.includes("session expired") || error?.status === 401) return t("sessionExpired");
    if (/login rejected|Login failed/i.test(message)) return t("loginError");
    if (/timed out|cancelled/.test(message)) return t("timeout");
    if (message.includes("Gravity stream interrupted")) return t("interrupted");
    return t("serverError", {message});
}
function toast(message, kind = "") {
    const element = document.createElement("div");
    element.className = "toast " + kind;
    element.textContent = message;
    $("#toastArea").append(element);
    setTimeout(() => element.remove(), 6000);
}
function showError(error) { toast(humanError(error), "err"); }
function serverById(id) { return state.servers.find(s => s.id === id); }
async function api(server, path, {method="GET", body} = {}) {
    const r = await nativeRequest(server.id, path, method, body === undefined ? "" : JSON.stringify(body), false);
    let data;
    try { data = r.body ? JSON.parse(r.body) : null; } catch { data = r.body; }
    if (r.status < 200 || r.status >= 300) {
        if (r.status === 401) { server.connected = false; renderPills(); }
        const error = new Error(data?.error?.message || "HTTP " + r.status);
        error.status = r.status;
        throw error;
    }
    if (data?.processed?.errors?.length) throw new Error(data.processed.errors.map(e=>e.error || e.message || e.item).join("; "));
    return data;
}
async function login(server, password) {
    server.checking = true; renderPills();
    try {
        await nativeLogin(server.id, password);
        server.password = password; server.connected = true; server.error = "";
        const configured = desktopConfig.servers.find(s => s.id === server.id);
        if (configured) configured.password = password;
    } catch (error) { server.connected = false; server.error = humanError(error); throw error; }
    finally { server.checking = false; renderPills(); }
}
let hostHealth = new Map(), trayInfo = {available:false,reason:""};
function healthText(h) {
    if (!h?.sampledAt) return "";
    const value = (v,suffix="") => v == null ? "—" : Number(v).toLocaleString(localeCode(),{maximumFractionDigits:1})+suffix;
    return `${t(h.reachable ? "reachable" : "unreachable")} · ${t("load")}: ${value(h.load)} · ${t("cpu")}: ${value(h.cpu," %")} · ${t("memory")}: ${value(h.memory," %")} · ${t("temperature")}: ${value(h.temperature," °"+(h.unit || "C"))} · ${new Date(h.sampledAt*1000).toLocaleTimeString(localeCode())}`;
}
function renderPills() {
    updateViewChrome(state.page?.container);
    const pills = $("#serverPills");
    $(".topbar-row").insertBefore(pills,$(".topbar-actions"));
    setHTML(pills,state.servers.map(s => {
        const h=hostHealth.get(s.id),online=h?.sampledAt ? h.reachable : s.connected;
        const status=s.checking ? "connecting" : !online ? (h?.sampledAt ? "unreachable" : "disconnected") : !s.connected ? "disconnected" : h?.blocking === "disabled" ? "disabled" : "connected";
        return `<span class="pill ${online ? "on" : "off"}" title="${escapeHtml(healthText(h))}"><span class="dot"></span><b>${escapeHtml(s.label)}</b><span class="role-badge">${textHTML(s.role || "secondary")}</span><span>${textHTML(status)}</span></span>`;
    }).join(""));
}

function renderShell() {
    document.documentElement.lang = currentLanguage;
    document.documentElement.dataset.theme = localStorage.getItem("ph_combo_theme") || "dark";
    $("header h1").innerHTML = `Pi-Manager <span class="dim">· ${textHTML("dashboard")}</span>`;
    const language = $("#languageSelect");
    language.innerHTML = '<option value="en">English</option><option value="da">Dansk</option><option value="el">Ελληνικά</option><option value="sv">Svenska</option>';
    language.value = currentLanguage;
    language.title = language.ariaLabel = t("language");
    language.onchange = () => setLanguage(language.value);
    $("#themeToggle").title = $("#themeToggle").ariaLabel = t("theme");
    $("#themeToggle").onclick = () => {
        const theme = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
        document.documentElement.dataset.theme = theme;
        localStorage.setItem("ph_combo_theme", theme);
        state.page?.refresh?.();
    };
    setHTML($("#tabs"), TAB_IDS.map(id => `<button data-tab="${id}" class="${id === state.activeTab ? "active" : ""}">${textHTML(id)}</button>`).join(""));
    $("#tabs").onclick = event => { const button = event.target.closest("[data-tab]"); if (button) switchTab(button.dataset.tab).catch(showError); };
    let trayButton=$("#minimizeTray");
    if (!trayButton) {trayButton=document.createElement("button");trayButton.id="minimizeTray";trayButton.className="btn xs";$(".topbar-actions").prepend(trayButton);}
    trayButton.textContent=t("minimizeTray");trayButton.disabled=!trayInfo.available;
    trayButton.title=trayInfo.available ? t("minimizeTray") : t("trayUnavailable",{reason:trayInfo.reason});
    trayButton.onclick=()=>nativeMinimizeToTray().catch(showError);
    renderPills();
}
async function setLanguage(language, render = true) {
    currentLanguage = UI_LANGUAGES.includes(language) ? language : "en";
    localStorage.setItem("ph_combo_language", currentLanguage);
    renderShell();
    if (render) await switchTab(state.activeTab);
}
async function switchTab(tab) {
    if (!TAB_IDS.includes(tab)) tab = "dashboard";
    const old = state.page;
    if (old) { old.active = false; clearTimeout(old.timer); old.dispose?.(); }
    state.activeTab = tab;
    const page = state.page = {active:true, timer:null, busy:false};
    try { history.replaceState(null, "", "#"+tab); } catch { /* about:blank may reject history updates */ }
    renderShell();
    // Give each view its own root so delegated handlers cannot survive a tab
    // change and act on another view's rows or stale record maps.
    const host = $("#tabContent");
    const container = document.createElement("section");
    host.replaceChildren(container);page.container=container;
    try {
        if (tab === "dashboard") await renderDashboard(container, page);
        else if (tab === "queries") await renderQueries(container, page);
        else if (["domains", "hosts", "clients", "lists"].includes(tab)) await renderRecords(container, page, tab);
        else if (tab === "drift") await renderDrift(container, page);
        else if (tab === "settings") await renderSettings(container, page);
        else await renderGravity(container, page);
        if(page.active)renderViewChrome(container,tab);
    } catch (error) { if (page.active) { container.textContent = humanError(error); showError(error); } }
}
const VIEW_ICONS={
    queries:'<path d="M4 5h16M4 12h10M4 19h6"/><circle cx="18" cy="17" r="3"/><path d="m20 19 2 3"/>',
    domains:'<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a18 18 0 0 1 0 18 18 18 0 0 1 0-18"/>',
    hosts:'<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M11 6.5h6M11 17.5h6"/>',
    clients:'<rect x="3" y="4" width="14" height="11" rx="2"/><path d="M10 15v5M6 20h8"/><rect x="17" y="10" width="5" height="11" rx="1"/>',
    lists:'<rect x="5" y="3" width="14" height="18" rx="2"/><path d="M9 8h6M9 12h6M9 16h4"/>',
    drift:'<path d="M8 3v13a4 4 0 0 0 4 4h8M16 3v5a4 4 0 0 1-4 4H8M5 6l3-3 3 3M13 6l3-3 3 3M17 17l3 3-3 3"/>',
    gravity:'<path d="M20 7a9 9 0 0 0-15-1L2 9M2 3v6h6M4 17a9 9 0 0 0 15 1l3-3M22 21v-6h-6"/>',
    settings:'<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3"/><circle cx="16" cy="17" r="3"/>'
};
function viewIcon(tab){return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${VIEW_ICONS[tab] || VIEW_ICONS.domains}</svg>`;}
function updateViewChrome(container) {
    if(!container)return;
    const connections=$("[data-view-connections]",container),main=$("[data-view-main]",container),count=$("[data-view-count]",container);
    if(connections)connections.textContent=state.servers.filter(server=>server.connected).length+" / "+state.servers.filter(server=>server.url).length;
    if(main)main.textContent=state.servers.find(server=>server.role === "main")?.label || "—";
    if(count)count.textContent=fmtNum(container._visibleCount || 0);
    const empty=$("[data-view-empty]",container),table=$(".datatable",container);
    if(empty && table)empty.hidden=!!$("tbody tr",table);
}
function renderViewChrome(container,tab) {
    if(tab === "dashboard")return;
    container.classList.add("workspace-view","view-"+tab);
    const heading=document.createElement("div");heading.className="view-heading";
    heading.innerHTML=`<div class="view-title-group"><span class="view-icon">${viewIcon(tab)}</span><div><span class="overview-eyebrow">${textHTML("workspace")}</span><h2>${textHTML(tab)}</h2><p>${textHTML(tab+"Intro")}</p></div></div><div class="view-facts"><div><span>${textHTML("viewConnections")}</span><strong data-view-connections></strong></div><div><span>${textHTML("main")}</span><strong data-view-main></strong></div>${container._visibleCount!==undefined ? `<div><span>${textHTML("viewRecords")}</span><strong data-view-count></strong></div>` : ""}</div>`;
    container.prepend(heading);
    const table=$(".datatable",container);
    if(table){const empty=document.createElement("p");empty.className="table-empty";empty.dataset.viewEmpty="";empty.textContent=t("noMatches");table.closest(".table-wrap").after(empty);}
    updateViewChrome(container);
}

function refreshControls(key) {
    const options = [[500,"realtime"], [1000,"oneSecond"], [2000,"seconds"], [5000,"seconds"], [10000,"seconds"], [15000,"seconds"], [30000,"seconds"], [60000,"minute"], [0,"paused"]];
    const selected = preference(key+"_interval", 2000);
    return `<label>${textHTML("refreshRate")} <select class="refresh-interval" aria-label="${textHTML("refreshRate")}">${options.map(([value,label]) => `<option value="${value}" ${selected === value ? "selected" : ""}>${textHTML(label,{n:value/1000})}</option>`).join("")}</select></label><button class="btn" data-refresh>${textHTML("refresh")}</button><span class="status-line" data-refresh-status></span>`;
}
async function startPolling(page, container, key, refresh) {
    let interval = preference(key+"_interval", 2000);
    const status = $("[data-refresh-status]", container);
    function schedule() {
        clearTimeout(page.timer);
        if (page.active && interval > 0) page.timer = setTimeout(() => document.hidden ? schedule() : page.refresh(), interval);
    }
    page.refresh = () => {
        if (!page.active) return Promise.resolve();
        if (page.busy) return page.pending;
        clearTimeout(page.timer);
        page.busy = true;
        page.pending = (async () => {
            try {
                const errors = await refresh();
                if (page.active) status.textContent = errors?.length ? errors.join(" · ") : t("lastUpdated", {time:new Date().toLocaleTimeString(localeCode())});
            } catch (error) { if (page.active) status.textContent = humanError(error); }
            finally { page.busy = false; schedule(); }
        })();
        return page.pending;
    };
    $(".refresh-interval", container).onchange = event => {
        interval = Number(event.target.value); savePreference(key+"_interval", interval); schedule();
    };
    $("[data-refresh]", container).onclick = () => page.refresh();
    await page.refresh();
}
document.addEventListener("visibilitychange", () => { if (!document.hidden && state.page?.refresh) state.page.refresh(); });
function loginHTML(server) {
    return `<form class="login-form" data-loginform="${server.id}"><input type="password" autocomplete="current-password" aria-label="${textHTML("password")}" placeholder="${textHTML("password")}" data-loginpw="${server.id}"><button class="btn primary xs">${textHTML("login")}</button></form>${server.error ? `<p class="sub">${escapeHtml(server.error)}</p>` : ""}`;
}
function bindLogins(container, page) {
    container.addEventListener("submit", async event => {
        const form = event.target.closest("[data-loginform]");
        if (!form) return;
        event.preventDefault();
        const button = $("button",form); button.disabled = true;
        try { await login(serverById(form.dataset.loginform), $("input",form).value); if (page.active) await page.refresh?.(); }
        catch (error) { showError(error); }
        finally { button.disabled = false; }
    });
}
function field(label, html, extra="") { return `<label class="field ${extra}"><span>${textHTML(label)}</span>${html}</label>`; }
function targetPicker(ids = state.servers.map(s => s.id)) {
    return `<fieldset class="target-picker"><legend>${textHTML("targets")}</legend>${state.servers.map(s => `<label><input type="checkbox" data-target="${s.id}" ${ids.includes(s.id) ? "checked" : ""}>${escapeHtml(s.label)}</label>`).join("")}</fieldset>`;
}
function selectedTargets(container) { return $$("[data-target]:checked",container).map(input => serverById(input.dataset.target)); }
function pagerHTML() { return `<div class="pager"><span data-count class="count-note"></span><button class="btn xs" data-previous>${textHTML("previous")}</button><span data-page></span><button class="btn xs" data-next>${textHTML("next")}</button></div>`; }
function updatePager(container, model, count) {
    container._visibleCount=count;updateViewChrome(container);
    const pages = Math.max(1, Math.ceil(count/model.pageSize));
    model.page = Math.max(0, Math.min(model.page,pages-1));
    $("[data-count]",container).textContent = t("count", {n:fmtNum(count)});
    $("[data-page]",container).textContent = t("page", {page:model.page+1,total:pages});
    $("[data-previous]",container).disabled = model.page === 0;
    $("[data-next]",container).disabled = model.page >= pages-1;
}
function reconcileRows(tbody, entries) {
    const old = new Map($$("tr[data-key]",tbody).map(row => [row.dataset.key,row]));
    const keep = new Set();
    let cursor = tbody.firstElementChild;
    for (const {key,html} of entries) {
        let row = old.get(key);
        if (!row) { row = document.createElement("tr"); row.dataset.key = key; }
        if (row._html !== html) { row.innerHTML = html; row._html = html; }
        keep.add(key);
        if (row !== cursor) tbody.insertBefore(row, cursor);
        cursor = row.nextElementSibling;
    }
    for (const [key,row] of old) if (!keep.has(key)) row.remove();
}

// Align histories by the API's ten-minute buckets instead of array position.
function combineDashboard(items) {
    const result={summary:{queries:{total:0,blocked:0},clients:{active:0}},history:{history:[]},clientHistory:{history:[],clients:{}},queryTypes:{types:{}},upstreams:{upstreams:[]}};
    const history=new Map(),clientHistory=new Map(),upstreams=new Map();
    for(const key of ["topDomains","topBlocked","topClients"]) result[key]={[key === "topClients" ? "clients" : "domains"]:[]};
    const tops={topDomains:new Map(),topBlocked:new Map(),topClients:new Map()};
    for(const item of items) {
        for(const key of ["total","blocked"]) result.summary.queries[key]+=Number(item.summary?.queries?.[key])||0;
        result.summary.clients.active+=Number(item.summary?.clients?.active)||0;
        for(const row of item.history?.history || []) {
            const timestamp=Math.floor(Number(row.timestamp)/600)*600;
            if(!Number.isFinite(timestamp)) continue;
            if(!history.has(timestamp))history.set(timestamp,{timestamp,total:0,blocked:0,cached:0,forwarded:0});
            const dest=history.get(timestamp);for(const key of ["total","blocked","cached","forwarded"])dest[key]+=Number(row[key])||0;
        }
        Object.assign(result.clientHistory.clients,item.clientHistory?.clients || {});
        for(const row of item.clientHistory?.history || []) {
            const timestamp=Math.floor(Number(row.timestamp)/600)*600;
            if(!Number.isFinite(timestamp))continue;
            if(!clientHistory.has(timestamp))clientHistory.set(timestamp,{timestamp,data:{}});
            const dest=clientHistory.get(timestamp).data;
            for(const [ip,count] of Object.entries(row.data || {}))dest[ip]=(dest[ip]||0)+(Number(count)||0);
        }
        for(const [type,count] of Object.entries(item.queryTypes?.types || {}))result.queryTypes.types[type]=(result.queryTypes.types[type]||0)+(Number(count)||0);
        for(const row of item.upstreams?.upstreams || []) {
            const key=JSON.stringify([row.ip || row.name,row.port]);
            if(!upstreams.has(key))upstreams.set(key,{...row,count:0});
            upstreams.get(key).count+=Number(row.count)||0;
        }
        for(const key of Object.keys(tops))for(const row of item[key]?.[key === "topClients" ? "clients" : "domains"] || []) {
            const identity=key === "topClients" ? row.ip : row.domain;
            if(!tops[key].has(identity))tops[key].set(identity,{...row,count:0});
            tops[key].get(identity).count+=Number(row.count)||0;
        }
    }
    result.history.history=[...history.values()].sort((a,b)=>a.timestamp-b.timestamp);
    result.clientHistory.history=[...clientHistory.values()].sort((a,b)=>a.timestamp-b.timestamp);
    result.upstreams.upstreams=[...upstreams.values()].sort((a,b)=>b.count-a.count);
    for(const key of Object.keys(tops))result[key][key === "topClients" ? "clients" : "domains"]=[...tops[key].values()].sort((a,b)=>b.count-a.count);
    return result;
}
const METRIC_ICONS={
    queriesToday:'<path d="m3 13 4-5 4 8 4-12 3 9h3"/><path d="M3 20h18"/>',
    blocked:'<path d="m12 3 8 3v5c0 5-8 10-8 10S4 16 4 11V6z"/><path d="m9 12 2 2 4-5"/>',
    activeClients:'<circle cx="9" cy="8" r="3"/><path d="M3 21v-3a6 6 0 0 1 12 0v3M16 5a3 3 0 0 1 0 6M21 21v-3a6 6 0 0 0-3-5"/>',
    blockedPercent:'<circle cx="12" cy="12" r="9"/><path d="m8 16 8-8M8 8h.01M16 16h.01"/>',
    blocklistDomains:'<path d="m12 3 10 5-10 5L2 8zM2 12l10 5 10-5M2 16l10 5 10-5"/>'
};
function metricIcon(key){return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${METRIC_ICONS[key]}</svg>`;}
function metricSparkline(history) {
    const values=history.map(row=>Number(row.total)||0);
    if(values.length<2)return `<div class="metric-sparkline metric-sparkline-empty" aria-hidden="true"></div>`;
    const max=Math.max(1,...values),points=values.map((value,index)=>`${(index*300/(values.length-1)).toFixed(1)},${(58-value*50/max).toFixed(1)}`);
    return `<svg class="metric-sparkline" viewBox="0 0 300 64" preserveAspectRatio="none" aria-hidden="true"><path d="M0,64 L${points.join(" L")} L300,64 Z" fill="currentColor" opacity=".13"/><polyline points="${points.join(" ")}" fill="none" stroke="currentColor" stroke-width="2.5" vector-effect="non-scaling-stroke"/></svg>`;
}
function blocklistOverview(data,servers=state.servers) {
    const main=servers.find(server=>server.role === "main");
    const countFor=server=>{const value=server?.connected ? data.get(server.id)?.summary?.gravity?.domains_being_blocked : null;return typeof value === "number" && Number.isFinite(value) && value>=0 ? value : null;};
    const count=countFor(main),drift=[],unknown=[];
    for(const server of servers.filter(server=>server.role === "secondary" && server.url)) {
        const secondaryCount=countFor(server);
        if(secondaryCount===null)unknown.push(server.label);
        else if(count!==null && secondaryCount!==count)drift.push({label:server.label,count:secondaryCount});
    }
    return {main,count,drift,unknown};
}
function dashboardMetrics(combined,servers,data) {
    const q=combined.summary.queries,total=q.total,blocked=q.blocked,percent=total ? Math.min(100,Math.max(0,blocked*100/total)) : 0;
    const available=servers.filter(server=>server.connected && data.has(server.id));
    const blocklist=blocklistOverview(data);
    const mainLabel=blocklist.main?.label || t("main");
    const blocklistBadge=blocklist.count===null ? "unavailable" : blocklist.drift.length ? "blocklistCountDrift" : blocklist.unknown.length ? "comparisonIncomplete" : "main";
    const warnings=[...(blocklist.count===null ? [t("mainBlocklistUnavailable",{main:mainLabel})] : []),...(blocklist.drift.length ? [t("blocklistCountWarning",{main:mainLabel,count:fmtNum(blocklist.count),servers:blocklist.drift.map(server=>server.label+" ("+fmtNum(server.count)+")").join(", ")})] : []),...(blocklist.unknown.length ? [t("blocklistCompareUnknown",{servers:blocklist.unknown.join(", ")})] : [])];
    const color=server=>["#5b8def","#e3b341","#c297ff","#ff8a3d","#56d4dd","#ff8a5b","#c297ff","#df86b9","#80c98f","#e2bc65"][(Number(server.id)-1)%10];
    const shares=(key,value,totalValue)=>`<div class="metric-share" role="img" aria-label="${escapeHtml(available.map(server=>server.label+": "+fmtNum(value(data.get(server.id)))).join("; "))}">${available.map((server,index)=>`<span title="${escapeHtml(server.label+": "+fmtNum(value(data.get(server.id))))}" style="flex:${totalValue ? Math.max(0,value(data.get(server.id))) : 0};background:${color(server)}"></span>`).join("")}</div><span class="metric-footnote">${textHTML(key)}</span>`;
    const tile=(key,value,visual,extra="")=>`<article class="metric-tile metric-${key} ${extra}"><div class="metric-title"><span>${textHTML(key)}</span><span class="metric-icon">${metricIcon(key)}</span></div><div class="big-num metric-value">${escapeHtml(value)}</div>${visual}</article>`;
    return `<div class="overview-heading"><div><span class="overview-eyebrow">Pi-hole</span><h2>${textHTML("combined")}</h2></div><span class="coverage-chip"><span class="coverage-dot"></span>${textHTML("hostCoverage",{n:available.length,total:servers.length})}</span></div><div class="metric-grid">
        ${tile("queriesToday",fmtNum(total),metricSparkline(combined.history.history)+`<div class="metric-footnote">${textHTML("fromSelectedHosts")}</div>`,"metric-hero")}
        ${tile("blocked",fmtNum(blocked),`<div class="metric-meter" role="img" aria-label="${textHTML("blockedPercent")}: ${escapeHtml(fmtNum(percent.toFixed(1)))} %"><span style="width:${percent}%"></span></div><span class="metric-footnote">${textHTML("permitted")} <b>${fmtNum(Math.max(0,total-blocked))}</b></span>`)}
        ${tile("activeClients",fmtNum(combined.summary.clients.active),shares("fromSelectedHosts",item=>Number(item.summary?.clients?.active)||0,combined.summary.clients.active))}
        <article class="metric-tile metric-blockedPercent"><div class="metric-title"><span>${textHTML("blockedPercent")}</span><span class="metric-icon">${metricIcon("blockedPercent")}</span></div><div class="metric-ring" style="--ring-value:${percent}%" role="img" aria-label="${textHTML("blockedPercent")}: ${escapeHtml(fmtNum(percent.toFixed(1)))} %"><div><span class="metric-ring-value">${fmtNum(percent.toFixed(1))}</span><span class="metric-ring-unit">%</span></div></div></article>
        ${tile("blocklistDomains",blocklist.count===null ? "—" : fmtNum(blocklist.count),`<div class="main-source"><span class="main-source-mark">${metricIcon("blocklistDomains")}</span><div><b>${escapeHtml(mainLabel)}</b><span>${textHTML("mainBlocklistSource")}</span></div></div><span class="metric-footnote ${blocklist.drift.length || blocklist.unknown.length || blocklist.count===null ? "warning-text" : ""}">${textHTML(blocklistBadge)}</span>`,"metric-main-blocklist") }
        </div>${warnings.length ? `<div class="notice warning blocklist-warning" role="status"><span class="notice-icon">${metricIcon("blocked")}</span><div>${warnings.map(warning=>`<p>${escapeHtml(warning)}</p>`).join("")}</div><button class="btn xs" data-open-drift>${textHTML("drift")}</button></div>` : ""}<div class="host-contribution"><span class="metric-footnote">${textHTML("hostContribution")}</span><div class="host-contribution-items">${available.map((server,index)=>{const count=Number(data.get(server.id)?.summary?.queries?.total)||0;return `<span class="host-contribution-item"><i style="background:${color(server)}"></i>${escapeHtml(server.label)}<b>${fmtNum((total ? count*100/total : 0).toFixed(1))}%</b></span>`}).join("") || `<span class="count-note">${textHTML("empty")}</span>`}</div></div>`;
}

async function renderDashboard(container,page) {
    const data=new Map(),charts=new Map();
    const palette=["#5b8def","#e05252","#3fb950","#d29922","#c297ff","#56d4dd","#ff8a5b","#8b98a5"];
    let selected=preference("dashboard_hosts",null);
    if(selected!==null)selected=selected.filter(id=>state.servers.some(s=>s.id === id));
    const selectedServers=()=>state.servers.filter(s=>selected===null || selected.includes(s.id));
    const foldPreference=key=>"dashboard_"+(key === "details" ? "details_expanded_v2" : key);
    const sectionOpen=key=>preference(foldPreference(key),key === "details");
    container.innerHTML=`<div class="toolbar">${refreshControls("dashboard")}</div>
        <fieldset class="target-picker dashboard-hosts"><legend>${textHTML("hostFilter")}</legend><label><input type="checkbox" data-all-hosts ${selected === null ? "checked" : ""}>${textHTML("allHosts")}</label>${state.servers.map(s=>`<label><input type="checkbox" data-dashboard-host="${s.id}" ${selected===null || selected.includes(s.id) ? "checked" : ""}>${escapeHtml(s.label)}</label>`).join("")}</fieldset>
        <div class="combined-summary" id="summaryCombined"></div><p class="count-note">${textHTML("sumNote")}</p>
        <div class="server-grid" id="dashGrid">${state.servers.map(s=>`<div class="card" id="summary${s.id}"></div>`).join("")}</div>
        <details class="fold" data-fold="details" ${sectionOpen("details") ? "open" : ""}><summary>${textHTML("details")}</summary><div class="chart-grid">${["history","clientHistory","queryTypes","upstreams"].map(key=>`<div class="card chart-panel"><h2>${textHTML("combined")} · ${textHTML(key)}</h2><p class="count-note" data-chart-coverage="${key}"></p><div class="chart-box"><canvas id="chart-${key}-combined" aria-label="${textHTML(key)}"></canvas></div></div>`).join("")}</div></details>
        <details class="fold" data-fold="top" ${sectionOpen("top") ? "open" : ""}><summary>${textHTML("topPanels")}</summary><div class="chart-grid">${["topDomains","topBlocked","topClients"].map(key=>`<div class="card"><h2>${textHTML("combined")} · ${textHTML(key)}</h2><p class="count-note" data-chart-coverage="${key}"></p><div id="${key}combined"></div></div>`).join("")}</div></details>`;
    page.dispose=()=>{for(const chart of charts.values())chart.destroy()};
    const stat=(key,value)=>`<div class="stat-row"><span class="k">${textHTML(key)}</span><span class="v">${escapeHtml(value)}</span></div>`;
    function updateChart(key,type,labels,datasets) {
        if(!page.active)return;
        const id=`chart-${key}-combined`,color=getComputedStyle(document.documentElement).getPropertyValue("--text").trim();
        const options={responsive:true,maintainAspectRatio:false,animation:false,plugins:{legend:{labels:{color}}}};
        if(type === "bar")options.scales={x:{stacked:true,ticks:{color,maxTicksLimit:8}},y:{stacked:true,beginAtZero:true,ticks:{color}}};
        if(charts.has(id)){const chart=charts.get(id);chart.data.labels=labels;chart.data.datasets=datasets;chart.options=options;chart.update("none")}
        else charts.set(id,new Chart(document.getElementById(id),{type,data:{labels,datasets},options}));
    }
    function draw() {
        const servers=selectedServers(),items=servers.filter(s=>s.connected).map(s=>data.get(s.id)).filter(Boolean),combined=combineDashboard(items),q=combined.summary.queries;
        setHTML($("#summaryCombined",container),dashboardMetrics(combined,servers,data));
        for(const server of state.servers) {
            const card=document.getElementById("summary"+server.id);card.hidden=!servers.includes(server);
            const item=server.connected ? data.get(server.id) : null,q=item?.summary?.queries;
            let html=`<h2><span class="tag tag-${server.id}">${escapeHtml(server.label)}</span> <span class="role-badge">${textHTML(server.role || "secondary")}</span></h2>`;
            if(!server.connected)html+=`<p class="sub">${textHTML(server.checking ? "connecting" : "disconnected")}</p>${loginHTML(server)}`;
            else if(!q)html+=`<p class="sub">${textHTML("unavailable")}</p>`;
            else {
                const enabled=item.blocking?.blocking === "enabled";
                html+=`<div class="big-num">${fmtNum(q.total)}</div><p class="sub">${textHTML("queriesToday")}</p>${stat("blocked",fmtNum(q.blocked))}${stat("lastGravity",fmtDate(item.summary.gravity?.last_update))}<div class="record-actions"><button class="btn xs ${enabled ? "danger" : "success"}" data-blocking="${server.id}" data-enable="${!enabled}">${textHTML(enabled ? "disableBlocking" : "enableBlocking")}</button></div>`;
            }
            setHTML(card,html);
        }
        for(const key of ["history","clientHistory","queryTypes","upstreams","topDomains","topBlocked","topClients"]){
            const count=items.filter(item=>item[key]!=null).length;
            $("[data-chart-coverage="+key+"]",container).textContent=t("hostCoverage",{n:count,total:servers.length});
        }
        for(const key of ["topDomains","topBlocked","topClients"]){
            const values=combined[key][key === "topClients" ? "clients" : "domains"],max=Math.max(1,...values.map(v=>v.count));
            setHTML(document.getElementById(key+"combined"),values.length ? values.slice(0,10).map(v=>`<div class="bar-row"><div class="bar-label"><span class="name" title="${escapeHtml(v.domain || v.name || v.ip)}">${escapeHtml(v.domain || v.name || v.ip)}</span><span>${fmtNum(v.count)}</span></div><div class="bar-track"><div class="bar-fill neutral" style="width:${100*v.count/max}%"></div></div></div>`).join("") : `<p class="sub">${textHTML("empty")}</p>`);
        }
        if(!$("[data-fold=details]",container).open)return;
        const labels=rows=>rows.map(v=>new Date(v.timestamp*1000).toLocaleTimeString(localeCode(),{hour:"2-digit",minute:"2-digit"})),history=combined.history.history;
        updateChart("history","bar",labels(history),[
            {label:t("blocked"),data:history.map(v=>v.blocked),backgroundColor:palette[1]},
            {label:t("cache"),data:history.map(v=>v.cached),backgroundColor:palette[0]},
            {label:t("forwarded"),data:history.map(v=>v.forwarded),backgroundColor:palette[2]},
            {label:t("other"),data:history.map(v=>Math.max(0,v.total-v.blocked-v.cached-v.forwarded)),backgroundColor:palette[7]}
        ]);
        const clientHistory=combined.clientHistory.history,clients=Object.entries(combined.clientHistory.clients).sort(([a],[b])=>clientHistory.reduce((sum,v)=>sum+(v.data[b]||0)-(v.data[a]||0),0));
        updateChart("clientHistory","bar",labels(clientHistory),clients.slice(0,8).map(([ip,client],i)=>({label:client.name||ip,data:clientHistory.map(v=>v.data[ip]||0),backgroundColor:palette[i%palette.length]})));
        const types=Object.entries(combined.queryTypes.types),upstreams=combined.upstreams.upstreams;
        updateChart("queryTypes","doughnut",types.map(([key])=>key),[{data:types.map(([,count])=>count),backgroundColor:palette}]);
        updateChart("upstreams","doughnut",upstreams.map(v=>(v.name||v.ip||t("other"))+(v.port>0 ? ":"+v.port : "")),[{data:upstreams.map(v=>v.count),backgroundColor:palette}]);
    }
    container.addEventListener("change",event=>{
        if(event.target.matches("[data-all-hosts]")){
            selected=event.target.checked ? null : [];
            for(const box of $$("[data-dashboard-host]",container))box.checked=selected===null;
        }else if(event.target.matches("[data-dashboard-host]")){
            selected=$$("[data-dashboard-host]:checked",container).map(box=>box.dataset.dashboardHost);
            if(selected.length===state.servers.length)selected=null;
            $("[data-all-hosts]",container).checked=selected===null;
        }else return;
        savePreference("dashboard_hosts",selected);draw();page.refresh?.();
    });
    for(const fold of $$("[data-fold]",container))fold.addEventListener("toggle",()=>{
        savePreference(foldPreference(fold.dataset.fold),fold.open);
        if(fold.open){for(const chart of charts.values())chart.resize();page.refresh?.()}
    });
    bindLogins(container,page);
    container.addEventListener("click",async event=>{
        if(event.target.closest("[data-open-drift]")){switchTab("drift").catch(showError);return;}
        const button=event.target.closest("[data-blocking]");if(!button)return;button.disabled=true;
        try {await api(serverById(button.dataset.blocking),"/dns/blocking",{method:"POST",body:{blocking:button.dataset.enable === "true"}});await page.refresh()}
        catch(error){showError(error)}finally{button.disabled=false}
    });
    draw();
    await startPolling(page,container,"dashboard",async()=>{
        const errors=[],details=$("[data-fold=details]",container).open,top=$("[data-fold=top]",container).open;
        await Promise.all(state.servers.filter(s=>s.connected).map(async server=>{
            try {
                const [summary,blocking]=await Promise.all([api(server,"/stats/summary"),api(server,"/dns/blocking")]);
                const item={...data.get(server.id),summary,blocking};
                const included=selectedServers().includes(server);
                const paths={...(details && included ? {history:"/history",clientHistory:"/history/clients",queryTypes:"/stats/query_types",upstreams:"/stats/upstreams"} : {}),...(top && included ? {topDomains:"/stats/top_domains?count=1000",topBlocked:"/stats/top_domains?blocked=true&count=1000",topClients:"/stats/top_clients?count=1000"} : {})};
                await Promise.all(Object.entries(paths).map(async([key,path])=>{try{item[key]=await api(server,path)}catch{item[key]=null}}));
                if(page.active)data.set(server.id,item);
            }catch(error){errors.push(server.label+": "+humanError(error));if(page.active)data.delete(server.id)}
        }));
        if(page.active)draw();return errors;
    });
}

function statusLabel(status) {
    const value = String(status || "").toUpperCase();
    if (value === "FORWARDED") return t("forwarded");
    if (value.startsWith("CACHE")) return t("cache");
    if (/GRAVITY|DENY|BLOCK|REGEX|SPECIAL_DOMAIN/.test(value)) return t("blocked");
    return value || "–";
}
async function renderQueries(container, page) {
    const model = {...{range:6,filter:"",server:"all",client:"all",type:"all",status:"all",pageSize:50,page:0,sort:"time",direction:-1},...preference("queries",{})};
    const rows = new Map(), cursors = new Map(), clientNames = new Map(), latest = new Map();
    let resetRequested = true, limited = false;
    const option = (value,label,selected) => `<option value="${escapeHtml(value)}" ${String(value) === String(selected) ? "selected" : ""}>${escapeHtml(label)}</option>`;
    container.innerHTML = `<div class="toolbar">
        <label>${textHTML("range")}<select data-range>${[[1,"hour"],[6,"hours"],[24,"hours"],[72,"days"],[168,"days"]].map(([n,key])=>option(n,t(key,{n:n>24?n/24:n}),model.range)).join("")}</select></label>
        <label>${textHTML("filter")}<input type="text" data-filter value="${escapeHtml(model.filter)}" placeholder="${textHTML("filterHint")}"></label>
        <label>${textHTML("server")}<select data-server-filter>${option("all",t("all"),model.server)}${state.servers.map(s=>option(s.id,s.label,model.server)).join("")}</select></label>
        <label>${textHTML("client")}<select data-client-filter></select></label>
        <label>${textHTML("type")}<select data-type-filter></select></label>
        <label>${textHTML("status")}<select data-status-filter></select></label>
        <label>${textHTML("perPage")}<select data-page-size>${[25,50,100,250,1000].map(n=>option(n,n,model.pageSize)).join("")}</select></label>
        <span class="spacer"></span>${refreshControls("queries")}
        </div><div class="card"><div class="table-wrap queries-table"><table class="datatable"><thead><tr>${["time","server","client","domain","type","status"].map(key=>`<th data-sort="${key}" aria-sort="none">${textHTML(key)}</th>`).join("")}</tr></thead><tbody></tbody></table></div>
        ${pagerHTML()}<button class="btn xs" data-load-more>${textHTML("loadMore")}</button><p class="sub" data-query-note></p></div>`;
    const tbody = $("tbody",container);
    function remember() { savePreference("queries",model); }
    function updateOptions(selector, values, selected, label = value => value) {
        const select = $(selector,container);
        const all = [...new Set([...values,...(selected !== "all" ? [selected] : [])])].sort((a,b)=>String(a).localeCompare(String(b),localeCode(),{numeric:true}));
        setHTML(select,option("all",t("all"),selected)+all.map(value=>option(value,label(value),selected)).join(""));
    }
    function draw() {
        if (!page.active) return;
        const allRows = [...rows.values()];
        updateOptions("[data-client-filter]",allRows.map(q=>q.client?.ip||"").filter(Boolean),model.client,ip=>clientNames.get(ip)||allRows.find(q=>q.client?.ip===ip)?.client?.name||ip);
        updateOptions("[data-type-filter]",allRows.map(q=>q.type).filter(Boolean),model.type);
        updateOptions("[data-status-filter]",allRows.map(q=>q.status).filter(Boolean),model.status,statusLabel);
        const filter = model.filter.toLocaleLowerCase(localeCode());
        const cutoff = Date.now()/1000 - model.range*3600;
        const filtered = allRows.filter(q => q.time >= cutoff && (model.server === "all" || q.server.id === model.server) && (model.client === "all" || q.client?.ip === model.client) && (model.type === "all" || q.type === model.type) && (model.status === "all" || q.status === model.status) && (!filter || `${q.domain} ${q.client?.ip} ${q.client?.name} ${clientNames.get(q.client?.ip)||""}`.toLocaleLowerCase(localeCode()).includes(filter)));
        const value = q => model.sort === "server" ? q.server.label : model.sort === "client" ? clientNames.get(q.client?.ip)||q.client?.name||q.client?.ip||"" : q[model.sort];
        filtered.sort((a,b) => model.direction * (model.sort === "time" ? a.time-b.time : String(value(a)||"").localeCompare(String(value(b)||""),localeCode(),{numeric:true})));
        updatePager(container,model,filtered.length);
        const visible = filtered.slice(model.page*model.pageSize,(model.page+1)*model.pageSize);
        reconcileRows(tbody,visible.map(q=>({key:q._key,html:`<td>${escapeHtml(new Date(q.time*1000).toLocaleString(localeCode()))}</td><td><span class="server-chip s${q.server.id}">${escapeHtml(q.server.label)}</span></td><td title="${escapeHtml(q.client?.ip)}">${escapeHtml(clientNames.get(q.client?.ip)||q.client?.name||q.client?.ip||"–")}</td><td class="mono">${escapeHtml(q.domain)}</td><td>${escapeHtml(q.type)}</td><td title="${escapeHtml(q.status)}">${escapeHtml(statusLabel(q.status))}</td>`})));
        $("[data-load-more]",container).disabled = ![...cursors.values()].some(c=>c?.cursor != null && c.cursor !== 0 && c.cursor !== "");
        for (const th of $$("[data-sort]",container)) th.setAttribute("aria-sort",th.dataset.sort === model.sort ? model.direction === 1 ? "ascending" : "descending" : "none");
        $("[data-query-note]",container).textContent = limited ? t("queryLimit") : state.servers.some(s=>s.connected) ? filtered.length ? "" : t("noMatches") : t("loginRequired");
    }
    function merge(server, result) {
        for (const q of result?.queries || []) {
            const key = server.id+":"+(q.id ?? [q.time,q.domain,q.client?.ip,q.type].join("|"));
            rows.set(key,{...q,server,_key:key});
            latest.set(server.id,Math.max(latest.get(server.id)||0,Number(q.time)||0));
        }
        for (const [key,q] of rows) if (q.time < Date.now()/1000 - model.range*3600) rows.delete(key);
        if (rows.size > 50000) {
            const sorted = [...rows.values()].sort((a,b)=>b.time-a.time);
            for (const q of sorted.slice(50000)) rows.delete(q._key);
            limited=true;
        }
    }
    for (const [selector,key] of [["[data-filter]","filter"],["[data-server-filter]","server"],["[data-client-filter]","client"],["[data-type-filter]","type"],["[data-status-filter]","status"],["[data-page-size]","pageSize"]]) {
        $(selector,container).addEventListener(key === "filter" ? "input" : "change",event=>{
            model[key] = key === "pageSize" ? Number(event.target.value) : event.target.value;
            model.page = 0; remember(); draw();
        });
    }
    $("[data-range]",container).onchange = async event => {
        model.range = Number(event.target.value); model.page=0; resetRequested=true; remember();
        await page.refresh(); if (resetRequested) await page.refresh();
    };
    for (const [selector,delta] of [["[data-previous]",-1],["[data-next]",1]]) $(selector,container).onclick=()=>{model.page+=delta;remember();draw();};
    $("thead",container).onclick = event => {
        const th = event.target.closest("[data-sort]"); if (!th) return;
        if (model.sort === th.dataset.sort) model.direction *= -1; else {model.sort=th.dataset.sort;model.direction=1;}
        model.page=0;remember();draw();
    };
    $("[data-load-more]",container).onclick = async event => {
        const button = event.target; button.disabled=true;
        try {
            await Promise.all(state.servers.filter(s=>s.connected).map(async server=>{
                const cursor = cursors.get(server.id); if (!cursor?.cursor) return;
                const result = await api(server,`/queries?length=1000&from=${cursor.from}&until=${cursor.until}&cursor=${encodeURIComponent(cursor.cursor)}`);
                if (page.active) {merge(server,result);cursors.set(server.id,{...cursor,cursor:result.cursor});}
            }));
            draw();
        } catch (error) {showError(error);button.disabled=false;}
    };
    draw();
    await startPolling(page,container,"queries",async()=>{
        const reset = resetRequested; resetRequested=false;
        const range = model.range, until=Math.floor(Date.now()/1000), from=until-range*3600;
        if (reset) {rows.clear();cursors.clear();latest.clear();limited=false;}
        const errors = [];
        await Promise.all(state.servers.filter(s=>s.connected).map(async server=>{
            const previousLatest = latest.get(server.id);
            try {
                const since = reset || !latest.has(server.id) ? from : Math.max(from,Math.floor(latest.get(server.id))-1);
                const result = await api(server,`/queries?length=1000&from=${since}&until=${until}`);
                if (!page.active || model.range !== range) return;
                merge(server,result);
                if (reset || !cursors.has(server.id)) cursors.set(server.id,{from,until,cursor:result?.cursor});
                // Drain the complete new time slice before advancing to the
                // next poll, even if it contains more than one API page.
                if (!reset && previousLatest !== undefined) {
                    let cursor=result?.cursor;
                    const seen=new Set();
                    while (cursor && !seen.has(String(cursor))) {
                        seen.add(String(cursor));
                        const next=await api(server,`/queries?length=1000&from=${since}&until=${until}&cursor=${encodeURIComponent(cursor)}`);
                        if (!page.active || model.range !== range) return;
                        merge(server,next);
                        cursor=next?.queries?.length ? next.cursor : null;
                    }
                }
                if (reset) {
                    try { const clients=await api(server,"/clients");for (const client of clients?.clients||[]) if (client.comment) clientNames.set(client.client,client.comment); } catch { /* Query display works without client metadata. */ }
                }
            } catch (error) {
                // Retry an incomplete slice rather than skipping its older rows.
                if (previousLatest !== undefined) latest.set(server.id,previousLatest);
                errors.push(server.label+": "+humanError(error));
            }
        }));
        draw();return errors;
    });
}

const RECORD_PATHS = {domains:"/domains",hosts:"/config/dns/hosts",clients:"/clients",lists:"/lists"};
function recordKey(kind, item) {
    if (kind === "domains") return JSON.stringify([item.domain,item.type,item.kind]);
    if (kind === "hosts") return item.name.toLowerCase();
    if (kind === "clients") return String(item.client).toUpperCase();
    return JSON.stringify([item.address,item.type]);
}
function recordName(kind, item) { return kind === "domains" ? item.domain : kind === "hosts" ? item.name : kind === "clients" ? item.client : item.address; }
function recordItems(kind, data) {
    if (kind !== "hosts") return data?.[kind] || [];
    return (data?.config?.dns?.hosts || []).flatMap(raw => {
        const [ip,...names] = raw.trim().split(/\s+/);
        return names.length ? [{ip,name:names.join(" "),raw}] : [];
    });
}
function recordDeletePath(kind, item) {
    if (kind === "domains") return `/domains/${item.type}/${item.kind}/${encodeURIComponent(item.domain)}`;
    if (kind === "hosts") return "/config/dns/hosts/"+encodeURIComponent(item.raw);
    if (kind === "clients") return "/clients/"+encodeURIComponent(item.client);
    return "/lists/"+encodeURIComponent(item.address)+"?type="+encodeURIComponent(item.type);
}
function recordCreateRequest(kind, item) {
    const body = {comment:item.comment || "",groups:item.groups || [0]};
    if (kind === "domains") return {path:`/domains/${item.type}/${item.kind}`,method:"POST",body:{...body,domain:[item.domain],type:item.type,kind:item.kind,enabled:item.enabled !== false}};
    if (kind === "hosts") return {path:"/config/dns/hosts/"+encodeURIComponent(item.raw),method:"PUT"};
    if (kind === "clients") return {path:"/clients",method:"POST",body:{...body,client:[item.client]}};
    return {path:"/lists?type="+encodeURIComponent(item.type),method:"POST",body:{...body,address:[item.address],enabled:item.enabled !== false}};
}
function recordToggleRequest(kind, item, enabled) {
    const body = {enabled,comment:item.comment || "",groups:item.groups || [0],type:item.type};
    if (kind === "domains") return {path:`/domains/${item.type}/${encodeURIComponent(item.domain)}`,method:"PUT",body:{...body,kind:item.kind}};
    return {path:"/lists/"+encodeURIComponent(item.address)+"?type="+encodeURIComponent(item.type),method:"PUT",body};
}
function validIPAddress(value) {
    if (/^\d{1,3}(\.\d{1,3}){3}$/.test(value)) return value.split(".").every(part=>Number(part)<=255);
    if (!value.includes(":")) return false;
    try {new URL("http://["+value+"]/");return true;} catch {return false;}
}
async function sendRecordRequest(server, request) { return api(server,request.path,request); }
async function replaceDNS(server, existing, next) {
    await api(server,recordDeletePath("hosts",existing),{method:"DELETE"});
    try {await sendRecordRequest(server,recordCreateRequest("hosts",next));}
    catch (error) {
        try {await sendRecordRequest(server,recordCreateRequest("hosts",existing));}
        catch {throw new Error(t("restoreFailed"));}
        throw new Error(t("restored"));
    }
}
async function renderRecords(container, page, kind) {
    const model = {...{filter:"",pageSize:50,page:0,sort:"name",direction:1,targets:state.servers.map(s=>s.id)},...preference("records_"+kind,{})};
    const selected = new Set(), byServer = new Map(), loaded = new Set();
    let merged = new Map(), visible = [], working = false;
    const toggleable = kind === "domains" || kind === "lists";
    const options = (values, selectedValue) => values.map(([value,key])=>`<option value="${value}" ${value === selectedValue ? "selected" : ""}>${textHTML(key)}</option>`).join("");
    container.innerHTML = `<div class="card"><h2>${textHTML("addMany")} · ${textHTML(kind)}</h2>
        <form class="record-form" id="recordForm">
        ${field(kind === "hosts" ? "dnsValues" : "values",`<textarea data-values required aria-label="${textHTML("values")}" placeholder="${kind === "hosts" ? "192.168.1.50 nas.home" : kind === "lists" ? "https://example.com/hosts" : kind === "clients" ? "192.168.1.50" : "ads.example.com"}"></textarea>`,"values-field")}
        ${toggleable ? field("type",`<select data-record-type>${options(kind === "domains" ? [["deny","deny"],["allow","allow"]] : [["block","deny"],["allow","allow"]],kind === "domains" ? "deny" : "block")}</select>`) : ""}
        ${kind === "domains" ? field("kind",`<select data-record-kind>${options([["exact","exact"],["regex","regex"]],"exact")}</select>`) : ""}
        ${kind !== "hosts" ? field("comment",'<input type="text" data-comment>') : ""}
        <button class="btn primary" type="submit">${textHTML("add")}</button></form>
        <div style="margin-top:12px">${targetPicker(model.targets)}</div>
        <div class="resource-sync"><p class="sub">${textHTML("syncResourceHelp")}</p><div class="toolbar"><button class="btn" data-sync-secondaries>${textHTML("syncSecondaries")}</button><span class="status-line" role="status" data-record-sync-status></span></div><p class="sub sync-errors" role="alert" data-record-sync-errors></p></div></div>
        <div class="card" style="margin-top:14px"><div class="toolbar"><label>${textHTML("filter")}<input type="text" data-record-filter value="${escapeHtml(model.filter)}" placeholder="${textHTML("filterHint")}"></label>
        <label>${textHTML("perPage")}<select data-record-size>${[25,50,100,250,1000].map(n=>`<option value="${n}" ${n === model.pageSize ? "selected" : ""}>${n}</option>`).join("")}</select></label><button class="btn" data-refresh-records>${textHTML("refresh")}</button><span class="status-line" data-record-status></span></div>
        <div class="bulk-toolbar"><span data-selected>${textHTML("selected",{n:0})}</span><button class="btn xs" data-bulk="clear">${textHTML("clearSelection")}</button>
        ${toggleable ? `<button class="btn xs" data-bulk="enable">${textHTML("enableSelected")}</button><button class="btn xs" data-bulk="disable">${textHTML("disableSelected")}</button>` : ""}
        <button class="btn xs" data-bulk="copy">${textHTML("copySelected")}</button><button class="btn xs danger" data-bulk="delete">${textHTML("deleteSelected")}</button></div>
        <div class="table-wrap records"><table class="datatable"><thead><tr><th><input type="checkbox" data-select-all aria-label="${textHTML("selectAll")}"></th>
        <th data-sort="name">${textHTML(kind === "hosts" || kind === "domains" ? "domain" : kind === "clients" ? "identifier" : "url")}</th>
        ${toggleable ? `<th data-sort="type">${textHTML("type")}</th>` : ""}${kind === "domains" ? `<th data-sort="kind">${textHTML("kind")}</th>` : ""}
        ${kind !== "hosts" ? `<th data-sort="comment">${textHTML("comment")}</th>` : ""}
        ${state.servers.map(s=>`<th data-sort="server${s.id}">${escapeHtml(s.label)}</th>`).join("")}<th>${textHTML("actions")}</th></tr></thead><tbody></tbody></table></div>${pagerHTML()}</div>
        ${kind === "clients" ? `<details class="fold" open><summary>${textHTML("suggestions")}</summary><div data-suggestions></div></details>` : ""}`;
    const tbody = $("tbody",container);
    const remember = () => savePreference("records_"+kind,model);
    function rebuild() {
        merged = new Map();
        for (const [id,items] of byServer) for (const item of items) {
            const key = recordKey(kind,item);
            if (!merged.has(key)) merged.set(key,{key,item,byServer:new Map()});
            merged.get(key).byServer.set(id,item);
        }
        for (const key of selected) if (!merged.has(key)) selected.delete(key);
    }
    function rowHTML(row) {
        const item = row.item;
        const checkbox = `<td><input type="checkbox" data-select ${selected.has(row.key) ? "checked" : ""} aria-label="${escapeHtml(t("selectAll")+": "+recordName(kind,item))}"></td>`;
        const common = `<td class="mono">${escapeHtml(recordName(kind,item))}</td>${toggleable ? `<td>${textHTML(item.type === "allow" ? "allow" : "deny")}</td>` : ""}${kind === "domains" ? `<td>${textHTML(item.kind === "regex" ? "regex" : "exact")}</td>` : ""}${kind !== "hosts" ? `<td>${escapeHtml(item.comment)}</td>` : ""}`;
        const cells = state.servers.map(server => {
            const value = row.byServer.get(server.id);
            if (!loaded.has(server.id)) return `<td><span class="sub">${textHTML(server.connected ? "unavailable" : "disconnected")}</span></td>`;
            if (!value) return `<td data-record-server="${server.id}"><span class="badge off">${textHTML("missing")}</span><div class="record-actions"><button class="btn xs" data-action="copy">${textHTML("add")}</button></div></td>`;
            if (kind === "hosts") return `<td data-record-server="${server.id}"><span class="mono">${escapeHtml(value.ip)}</span><div class="record-actions"><button class="btn xs" data-action="edit">${textHTML("edit")}</button><button class="btn xs" data-action="sync" title="${textHTML("copyFrom")}">${textHTML("copy")}</button><button class="btn xs danger" data-action="delete">${textHTML("delete")}</button></div></td>`;
            const enabled = value.enabled !== false;
            return `<td data-record-server="${server.id}"><span class="badge ${enabled ? "on" : "off"}">${textHTML(enabled ? "enabled" : "disabled")}</span><div class="record-actions">${toggleable ? `<button class="btn xs" data-action="${enabled ? "disable" : "enable"}">${textHTML(enabled ? "disable" : "enable")}</button>` : ""}<button class="btn xs danger" data-action="delete">${textHTML("delete")}</button></div></td>`;
        }).join("");
        return checkbox+common+cells+`<td class="row-actions"><button class="btn xs danger" data-action="delete-row">${textHTML("delete")}</button></td>`;
    }
    function draw() {
        if (!page.active) return;
        const filter = model.filter.toLocaleLowerCase(localeCode());
        const rows = [...merged.values()].filter(row=>`${recordName(kind,row.item)} ${row.item.comment||""} ${[...row.byServer.values()].map(v=>v.ip||"").join(" ")}`.toLocaleLowerCase(localeCode()).includes(filter));
        const value = row => model.sort === "name" ? recordName(kind,row.item) : model.sort.startsWith("server") ? kind === "hosts" ? row.byServer.get(model.sort.slice(6))?.ip||"" : row.byServer.has(model.sort.slice(6)) ? "1" : "0" : row.item[model.sort]||"";
        rows.sort((a,b)=>model.direction*String(value(a)).localeCompare(String(value(b)),localeCode(),{numeric:true}));
        updatePager(container,model,rows.length);
        visible = rows.slice(model.page*model.pageSize,(model.page+1)*model.pageSize);
        reconcileRows(tbody,visible.map(row=>({key:row.key,html:rowHTML(row)})));
        for (const row of $$("tr[data-key]",tbody)) {
            const record = merged.get(row.dataset.key);
            row.classList.toggle("selected-row",selected.has(row.dataset.key));
            row.classList.toggle("row-mismatch",kind === "hosts" && new Set([...record.byServer.values()].map(v=>v.ip)).size > 1);
        }
        const all = $("[data-select-all]",container);
        all.checked = visible.length>0 && visible.every(row=>selected.has(row.key));
        all.indeterminate = visible.some(row=>selected.has(row.key)) && !all.checked;
        $("[data-selected]",container).textContent = t("selected",{n:selected.size});
        for (const button of $$("[data-bulk]",container)) button.disabled = working || selected.size === 0;
        $("[data-sync-secondaries]",container).disabled=working || !state.servers.some(s=>s.role === "main" && s.url) || !selectedTargets(container).some(s=>s.role === "secondary" && s.url);
        for (const th of $$("[data-sort]",container)) th.setAttribute("aria-sort",th.dataset.sort === model.sort ? model.direction === 1 ? "ascending" : "descending" : "none");
    }
    async function loadServers(servers) {
        const errors = [];
        await Promise.all(servers.map(async server=>{
            if (!server.connected) {loaded.delete(server.id);return;}
            try {
                const data = await api(server,RECORD_PATHS[kind]);
                if (page.active) {byServer.set(server.id,recordItems(kind,data));loaded.add(server.id);}
            } catch (error) {loaded.delete(server.id);errors.push(server.label+": "+humanError(error));}
        }));
        if (page.active) {rebuild();draw();$("[data-record-status]",container).textContent = errors.join(" · ") || (state.servers.some(s=>s.connected) ? "" : t("loginRequired"));}
    }
    async function runChanges(operation) {
        if (working) return;
        working=true;
        for (const button of $$("button",container)) button.disabled=true;
        let completed=0,failed=0,conflicts=0;
        const touched = new Set();
        const execute = async (server,fn) => {
            touched.add(server.id);
            try {if (!server.connected) throw new Error(t("loginRequired"));await fn();completed++;}
            catch (error) {failed++;toast(server.label+": "+humanError(error),"err");}
        };
        try {await operation(execute,()=>conflicts++);await loadServers(state.servers.filter(s=>touched.has(s.id)));}
        finally {working=false;if (page.active) {for (const button of $$("button",container)) button.disabled=false;draw();}}
        if (completed) toast(t("changed",{n:completed}),"ok");
        if (failed) toast(t("failedChanges",{n:failed}),"err");
        if (conflicts) toast(t("conflictSkipped"));
    }
    async function perform(action, keys, ids, sourceId) {
        const targets = ids ? state.servers.filter(s=>ids.includes(s.id)) : selectedTargets(container);
        if (!targets.length) return toast(t("selectTargets"),"err");
        if (!keys.length) return toast(t("selectRows"),"err");
        if (action === "delete" && !window.confirm(t("deleteConfirm",{n:keys.length}))) return;
        await runChanges(async (execute, conflict) => {
            for (const key of keys) {
                const row = merged.get(key);if (!row) continue;
                const source = sourceId ? row.byServer.get(sourceId) : row.item;
                for (const server of targets) {
                    const existing = row.byServer.get(server.id);
                    if (action === "delete" && existing) await execute(server,()=>api(server,recordDeletePath(kind,existing),{method:"DELETE"}));
                    else if ((action === "enable" || action === "disable") && existing) await execute(server,()=>sendRecordRequest(server,recordToggleRequest(kind,existing,action === "enable")));
                    else if ((action === "copy" || action === "sync") && source) {
                        if (!existing) await execute(server,()=>sendRecordRequest(server,recordCreateRequest(kind,source)));
                        else if (kind === "hosts" && existing.ip !== source.ip) {
                            if (action === "sync" && window.confirm(t("dnsReplaceConfirm",{server:server.label}))) await execute(server,()=>replaceDNS(server,existing,source));
                            else conflict();
                        }
                    }
                }
            }
        });
    }
    $("[data-sync-secondaries]",container).onclick=async()=>{
        if(working)return;
        const main=state.servers.find(s=>s.role === "main"),targets=selectedTargets(container).filter(s=>s.role === "secondary" && s.url);
        if(!main || !targets.length)return;
        const status=$("[data-record-sync-status]",container),errors=$("[data-record-sync-errors]",container);
        working=true;status.textContent=t("loading");errors.textContent="";
        for(const control of $$("button,[data-target]",container))control.disabled=true;
        try {
            const plan=await nativePreviewSync(targets.map(s=>s.id),[kind],false);
            if(!page.active)return;
            const changes=plan.rows.filter(row=>row.status === "missing" || row.status === "changed");
            const problems=plan.rows.filter(row=>row.status === "conflict" || row.status === "unavailable").map(row=>`${serverById(row.serverId)?.label || row.serverId}: ${row.problem || t("drift_"+row.status)}`);
            errors.textContent=problems.join("\n");status.textContent=t("syncSummary",plan);
            if(!changes.length)return;
            if(!window.confirm(t("syncResourceConfirm",{n:changes.length,resource:t(kind),main:main.label,servers:targets.map(s=>s.label).join(", "),conflicts:plan.conflicts})+"\n"+t("upsertMode"))){status.textContent=t("syncCancelled");return;}
            if(!page.active)return;
            status.textContent=t("loading");
            const result=await nativeApplySync(plan.token,changes.map(row=>row.id));
            if(!page.active)return;
            status.textContent=t("syncDone",result);
            errors.textContent=[...problems,...result.errors].join("\n");
            toast(t("syncDone",result),result.failed || problems.length ? "err" : "ok");
            await loadServers([main,...targets]);
            if(kind === "clients")await loadSuggestions();
        }catch(error){if(page.active){status.textContent=humanError(error);showError(error);}}
        finally {
            working=false;
            if(page.active){for(const control of $$("button,[data-target]",container))control.disabled=false;draw();}
        }
    };
    $("#recordForm",container).onsubmit = async event => {
        event.preventDefault();
        const values = [...new Set($("[data-values]",container).value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean))];
        const targets = selectedTargets(container);
        if (!targets.length) return toast(t("selectTargets"),"err");
        if (!values.length) return;
        const items = values.map(value=>{
            if (kind === "hosts") {const [ip,...names]=value.split(/\s+/);return {ip,name:names.join(" "),raw:ip+" "+names.join(" ")};}
            const common = {comment:$("[data-comment]",container).value.trim(),groups:[0],enabled:true};
            if (kind === "domains") return {...common,domain:value,type:$("[data-record-type]",container).value,kind:$("[data-record-kind]",container).value};
            if (kind === "lists") return {...common,address:value,type:$("[data-record-type]",container).value};
            return {...common,client:value};
        });
        if (kind === "hosts" && items.some(item=>!item.name || !validIPAddress(item.ip))) return toast(t("dnsInvalid"),"err");
        await runChanges(async execute=>{
            for (const server of targets) for (const item of items) await execute(server,()=>sendRecordRequest(server,recordCreateRequest(kind,item)));
        });
        // Keep the form, filter, selection, sorting and scroll position intact.
    };
    $("[data-record-filter]",container).oninput=event=>{model.filter=event.target.value;model.page=0;remember();draw();};
    $("[data-record-size]",container).onchange=event=>{model.pageSize=Number(event.target.value);model.page=0;remember();draw();};
    for (const [selector,delta] of [["[data-previous]",-1],["[data-next]",1]]) $(selector,container).onclick=()=>{model.page+=delta;remember();draw();};
    for (const input of $$("[data-target]",container)) input.onchange=()=>{model.targets=selectedTargets(container).map(s=>s.id);remember();draw();};
    $("[data-select-all]",container).onchange=event=>{for (const row of visible) if (event.target.checked) selected.add(row.key);else selected.delete(row.key);draw();};
    tbody.onchange=event=>{if (event.target.matches("[data-select]")) {const key=event.target.closest("tr").dataset.key;if (event.target.checked) selected.add(key);else selected.delete(key);draw();}};
    container.addEventListener("click",async event=>{
        const bulk = event.target.closest("[data-bulk]");
        if (bulk) {if (bulk.dataset.bulk === "clear") {selected.clear();draw();} else await perform(bulk.dataset.bulk,[...selected]);return;}
        const action = event.target.closest("[data-action]");
        if (action) {
            const key=action.closest("tr").dataset.key;
            const id=action.closest("[data-record-server]")?.dataset.recordServer;
            const mode=action.dataset.action;
            if (mode === "edit") {
                const existing=merged.get(key).byServer.get(id),server=serverById(id);
                const ip=window.prompt(t("dnsEdit",{domain:existing.name}),existing.ip);
                if (ip && !validIPAddress(ip.trim())) {toast(t("dnsInvalid"),"err");return;}
                if (ip && ip.trim() !== existing.ip) await runChanges(execute=>execute(server,()=>replaceDNS(server,existing,{...existing,ip:ip.trim(),raw:ip.trim()+" "+existing.name})));
            } else await perform(mode === "delete-row" ? "delete" : mode,[key],mode === "sync" || mode === "delete-row" ? undefined : [id],mode === "sync" ? id : undefined);
            return;
        }
        const sort=event.target.closest("[data-sort]");
        if (sort) {if (model.sort === sort.dataset.sort) model.direction*=-1;else {model.sort=sort.dataset.sort;model.direction=1;}model.page=0;remember();draw();}
    });
    page.refresh = async () => {await loadServers(state.servers);if (kind === "clients") await loadSuggestions();};
    $("[data-refresh-records]",container).onclick=()=>page.refresh().catch(showError);
    async function loadSuggestions() {
        const area=$("[data-suggestions]",container);if (!area) return;
        const devices=new Map();
        await Promise.all(state.servers.filter(s=>s.connected).map(async server=>{
            try {const data=await api(server,"/clients/_suggestions");for (const item of data?.clients||[]) {
                const id=String(item.hwaddr||item.client||"").replace(/^IP-/i,"");
                if (!id || [...merged.values()].some(row=>String(row.item.client).toUpperCase() === id.toUpperCase())) continue;
                if (!devices.has(id)) devices.set(id,{...item,id,servers:[]});devices.get(id).servers.push(server.label);
            }} catch { /* Device suggestions are optional. */ }
        }));
        if (!page.active) return;
        area.innerHTML=devices.size ? `<div class="table-wrap records"><table class="datatable"><thead><tr><th>${textHTML("identifier")}</th><th>${textHTML("knownNames")}</th><th>${textHTML("vendor")}</th><th>${textHTML("server")}</th><th>${textHTML("name")}</th><th>${textHTML("actions")}</th></tr></thead><tbody>${[...devices.values()].map((item,index)=>`<tr data-device="${index}"><td class="mono">${escapeHtml(item.id)}</td><td>${escapeHtml(item.names)}</td><td>${escapeHtml(item.macVendor)}</td><td>${escapeHtml(item.servers.join(", "))}</td><td><input type="text" data-device-name aria-label="${textHTML("name")}"></td><td><button class="btn xs" data-add-device="${index}">${textHTML("add")}</button></td></tr>`).join("")}</tbody></table></div>` : `<p class="sub">${textHTML("empty")}</p>`;
        area.onclick=async event=>{
            const button=event.target.closest("[data-add-device]");if (!button) return;
            const item=[...devices.values()][Number(button.dataset.addDevice)],comment=$("[data-device-name]",button.closest("tr")).value.trim();
            const targets=selectedTargets(container);if (!targets.length) return toast(t("selectTargets"),"err");
            await runChanges(async execute=>{for (const server of targets) await execute(server,()=>sendRecordRequest(server,recordCreateRequest("clients",{client:item.id,comment,groups:[0]})));});
            if (page.active) button.closest("tr").remove();
        };
    }
    draw();await page.refresh();
}

async function renderDrift(container,page) {
    const main=state.servers.find(s=>s.role === "main"),secondaries=state.servers.filter(s=>s.role !== "main");
    const resources=["domains","hosts","clients","lists"];
    let plan=null,working=false,currentPage=0;
    const selected=new Set();
    const canSync=row=>row.status === "missing" || row.status === "changed" || (row.status === "extra" && plan?.mirror);
    container.innerHTML=`<div class="card"><h2>${textHTML("drift")} · ${textHTML("main")}: ${escapeHtml(main?.label || "—")}</h2><p class="sub">${textHTML("syncHelp")}</p>
        <fieldset class="target-picker" data-sync-targets><legend>${textHTML("secondary")}</legend>${secondaries.map(s=>`<label><input type="checkbox" value="${s.id}" checked>${escapeHtml(s.label)}</label>`).join("") || `<p>${textHTML("noSecondary")}</p>`}</fieldset>
        <fieldset class="target-picker" data-sync-resources><legend>${textHTML("resource")}</legend>${resources.map(kind=>`<label><input type="checkbox" value="${kind}" checked>${textHTML(kind)}</label>`).join("")}</fieldset>
        <div class="toolbar"><label><input type="checkbox" data-sync-mirror>${textHTML("mirror")}</label><button class="btn primary" data-preview-sync ${!secondaries.length ? "disabled" : ""}>${textHTML("previewSync")}</button><button class="btn" data-apply-sync disabled>${textHTML("applySelectedSync",{n:0})}</button></div><p role="status" data-sync-status></p><div role="alert" data-sync-errors></div></div>
        <div class="card drift-results"><p class="sub">${textHTML("syncSelectionHelp")}</p><div class="toolbar"><label>${textHTML("filter")}<input type="text" data-drift-filter></label><label>${textHTML("hostFilter")}<select data-drift-host><option value="all">${textHTML("allHosts")}</option>${secondaries.map(s=>`<option value="${s.id}">${escapeHtml(s.label)}</option>`).join("")}</select></label><label>${textHTML("resource")}<select data-drift-resource><option value="all">${textHTML("all")}</option>${resources.map(kind=>`<option value="${kind}">${textHTML(kind)}</option>`).join("")}</select></label><label><input type="checkbox" data-differences checked>${textHTML("differencesOnly")}</label></div>
        <div class="bulk-toolbar"><button class="btn xs" data-select-drift>${textHTML("selectFilteredChanges")}</button><button class="btn xs" data-clear-drift>${textHTML("clearSelection")}</button><span class="count-note" data-drift-selection role="status"></span></div>
        <div class="table-wrap"><table class="datatable"><thead><tr><th><input type="checkbox" data-drift-select-all aria-label="${textHTML("selectFilteredChanges")}"></th><th>${textHTML("server")}</th><th>${textHTML("resource")}</th><th>${textHTML("name")}</th><th>${textHTML("status")}</th><th>${textHTML("main")}</th><th>${textHTML("secondary")}</th></tr></thead><tbody></tbody></table></div>${pagerHTML()}</div>`;
    const status=$("[data-sync-status]",container),apply=$("[data-apply-sync]",container),preview=$("[data-preview-sync]",container);
    let filtered=[];
    function draw() {
        const filter=$("[data-drift-filter]",container).value.trim().toLowerCase(),host=$("[data-drift-host]",container).value,resource=$("[data-drift-resource]",container).value,differences=$("[data-differences]",container).checked;
        const rows=(plan?.rows || []).filter(row=>(host === "all" || row.serverId === host)&&(resource === "all" || row.resource === resource)&&(!differences || row.status !== "same")&&(!filter || [row.key,row.problem,JSON.stringify(row.main),JSON.stringify(row.secondary)].join(" ").toLowerCase().includes(filter)));
        filtered=rows;
        const pages=Math.max(1,Math.ceil(rows.length/50));currentPage=Math.min(currentPage,pages-1);
        const value=v=>v==null ? "—" : typeof v === "string" ? escapeHtml(v) : `<pre>${escapeHtml(JSON.stringify(v,null,2))}</pre>`;
        reconcileRows($("tbody",container),rows.slice(currentPage*50,(currentPage+1)*50).map(row=>({key:row.id,html:`<td><input type="checkbox" data-drift-select="${row.id}" aria-label="${escapeHtml(t("selectSyncChange",{name:row.key || row.resource,server:serverById(row.serverId)?.label || row.serverId}))}" ${!canSync(row) || working ? "disabled" : ""}></td><td>${escapeHtml(serverById(row.serverId)?.label || row.serverId)}</td><td>${textHTML(row.resource)}</td><td class="mono">${escapeHtml(row.key || "—")}</td><td><span class="badge drift-status drift-status-${row.status} ${row.status === "same" ? "ok" : ""}">${textHTML("drift_"+row.status)}</span>${row.problem ? `<p class="sub">${escapeHtml(row.problem)}</p>` : ""}</td><td>${value(row.main)}</td><td>${value(row.secondary)}</td>`})));
        updatePager(container,{page:currentPage,pageSize:50},rows.length);
        for(const box of $$("[data-drift-select]",container)){box.checked=selected.has(box.dataset.driftSelect);box.closest("tr").classList.toggle("selected-row",box.checked);}
        const eligible=rows.filter(canSync),count=eligible.filter(row=>selected.has(row.id)).length,all=$("[data-drift-select-all]",container);
        all.disabled=working || !eligible.length;all.checked=eligible.length>0 && count===eligible.length;all.indeterminate=count>0 && count<eligible.length;
        $("[data-select-drift]",container).disabled=working || !eligible.length;
        $("[data-clear-drift]",container).disabled=working || !selected.size;
        const filteredIDs=new Set(rows.map(row=>row.id)),hidden=[...selected].filter(id=>!filteredIDs.has(id)).length;
        $("[data-drift-selection]",container).textContent=t("selectedChanges",{n:selected.size})+(hidden ? " · "+t("hiddenSelection",{n:hidden}) : "");
        apply.textContent=t("applySelectedSync",{n:selected.size});
        apply.disabled=working || !plan || !selected.size;
    }
    function lockInputs(locked) {for(const input of $$("[data-sync-targets] input,[data-sync-resources] input,[data-sync-mirror]",container))input.disabled=locked;}
    async function check() {
        if(working)return;working=true;plan=null;selected.clear();draw();lockInputs(true);preview.disabled=true;apply.disabled=true;status.textContent=t("loading");$("[data-sync-errors]",container).textContent="";
        try {
            const targets=$$("[data-sync-targets] input:checked",container).map(box=>box.value),kinds=$$("[data-sync-resources] input:checked",container).map(box=>box.value);
            plan=await nativePreviewSync(targets,kinds,$("[data-sync-mirror]",container).checked);currentPage=0;
            if(page.active)status.textContent=t("syncSummary",plan);
        }catch(error){plan=null;selected.clear();if(page.active){status.textContent=humanError(error);showError(error)}}
        finally{working=false;lockInputs(false);if(page.active){preview.disabled=!secondaries.length;draw()}}
    }
    preview.onclick=check;
    apply.onclick=async()=>{
        if(working || !plan || !selected.size)return;
        if(!confirm(t("syncConfirm",{n:selected.size,main:main.label,mode:t(plan.mirror ? "selectedMirrorMode" : "upsertMode")})))return;
        working=true;lockInputs(true);draw();apply.disabled=true;preview.disabled=true;
        try {
            const result=await nativeApplySync(plan.token,[...selected]);plan=null;selected.clear();
            if(page.active){status.textContent=t("syncDone",result);$("[data-sync-errors]",container).textContent=result.errors.join("\n");toast(t("syncDone",result),result.failed ? "err" : "ok")}
        }catch(error){plan=null;selected.clear();if(page.active){status.textContent=humanError(error);showError(error)}}
        finally{working=false;lockInputs(false);if(page.active){preview.disabled=false;draw()}}
    };
    $("[data-select-drift]",container).onclick=()=>{if(working)return;for(const row of filtered.filter(canSync))selected.add(row.id);draw()};
    $("[data-clear-drift]",container).onclick=()=>{if(working)return;selected.clear();draw()};
    container.addEventListener("change",event=>{
        if(working)return;
        if(event.target.matches("[data-drift-select]")){if(event.target.checked)selected.add(event.target.dataset.driftSelect);else selected.delete(event.target.dataset.driftSelect);draw();return}
        if(event.target.matches("[data-drift-select-all]")){for(const row of filtered.filter(canSync)){if(event.target.checked)selected.add(row.id);else selected.delete(row.id)}draw();return}
        if(event.target.closest("[data-sync-targets],[data-sync-resources]") || event.target.matches("[data-sync-mirror]")){plan=null;selected.clear();status.textContent="";draw()}
        else if(event.target.matches("[data-drift-host],[data-drift-resource],[data-differences]")){currentPage=0;draw()}
    });
    $("[data-drift-filter]",container).oninput=()=>{currentPage=0;draw()};
    $("[data-previous]",container).onclick=()=>{currentPage=Math.max(0,currentPage-1);draw()};
    $("[data-next]",container).onclick=()=>{currentPage++;draw()};
    draw();
}

async function renderSettings(container, page) {
    let rows=desktopConfig.servers.map(s=>({...s}));
    container.innerHTML=`<div class="card"><h2>${textHTML("serverSettings")}</h2><p class="sub">${textHTML("urlHelp")}</p><p class="sub">${textHTML("passwordHelp")}</p>
        <p class="sub">${textHTML("roleHelp")}</p><form id="desktopSettings"><div id="desktopServers"></div><div class="toolbar"><button class="btn" type="button" id="desktopAdd">${textHTML("addServer")}</button><button class="btn primary" type="submit">${textHTML("save")}</button><span class="status-line" data-settings-status></span></div></form><p class="sub">${textHTML("logsHelp")}</p></div>`;
    const area=$("#desktopServers",container);
    function readRows() {
        for (const row of $$("[data-server]",area)) {
            const server=rows.find(s=>s.id === row.dataset.server);
            server.label=$("[data-name]",row).value.trim();
            server.url=$("[data-url]",row).value.trim();
            server.password=$("[data-password]",row).value;
        }
    }
    function draw() {
        area.innerHTML=rows.map(s=>`<div class="server-setting" data-server="${s.id}"><span class="server-chip s${s.id}">${s.id}</span>
            ${field("role",`<select data-role><option value="main" ${s.role === "main" ? "selected" : ""}>${textHTML("main")}</option><option value="secondary" ${s.role !== "main" ? "selected" : ""}>${textHTML("secondary")}</option></select>`)}
            ${field("name",`<input type="text" data-name value="${escapeHtml(s.label)}" required>`)}
            ${field("serverURL",`<input type="text" data-url value="${escapeHtml(s.url)}" placeholder="http://192.168.1.2/api">`,"url-field")}
            ${field("password",'<input type="password" data-password autocomplete="new-password">',"password-field")}
            <button class="btn danger" type="button" data-remove="${s.id}" ${rows.length === 1 ? "disabled" : ""}>${textHTML("remove")}</button></div>`).join("");
        for (const row of $$("[data-server]",area)) $("[data-password]",row).value=rows.find(s=>s.id === row.dataset.server).password || "";
        $("#desktopAdd",container).disabled=rows.length >= 10;
    }
    area.onchange=event=>{
        if (!event.target.matches("[data-role]")) return;
        readRows();const id=event.target.closest("[data-server]").dataset.server;
        if (event.target.value === "main") rows.forEach(s=>s.role=s.id === id ? "main" : "secondary");
        else {const row=rows.find(s=>s.id === id);if(row.role === "main") {draw();return;}}
        draw();
    };
    area.onclick=event=>{
        const button=event.target.closest("[data-remove]");if (!button) return;
        readRows();rows=rows.filter(s=>s.id !== button.dataset.remove);if (!rows.some(s=>s.role === "main")) rows[0].role="main";draw();
    };
    $("#desktopAdd",container).onclick=()=>{
        readRows();const id=Array.from({length:10},(_,i)=>String(i+1)).find(id=>!rows.some(s=>s.id === id));
        if (id) rows.push({id,label:"Pi-hole "+id,url:"",password:"",role:"secondary"});draw();
    };
    $("#desktopSettings",container).onsubmit=async event=>{
        event.preventDefault();readRows();
        const button=$("button[type=submit]",container);button.disabled=true;
        const status=$("[data-settings-status]",container);status.textContent=t("connecting");
        try {
            await nativeSaveServers(rows);
            const previous=new Map(state.servers.map(s=>[s.id,s]));
            desktopConfig.servers=rows.map(s=>({...s}));
            state.servers=rows.map(s=>{
                const old=previous.get(s.id),same=old?.url === s.url && old?.password === s.password;
                return {...s,connected:!!(same && old.connected),checking:false};
            });
            await Promise.all(state.servers.filter(s=>s.url && s.password && !s.connected).map(async s=>{try {await login(s,s.password);} catch (error) {toast(s.label+": "+humanError(error),"err");}}));
            if (page.active) {status.textContent=t("saved");toast(t("saved"),"ok");}
            renderPills();page.drawHealth?.();
        } catch (error) {if (page.active) status.textContent=t("saveError");showError(error);}
        finally {button.disabled=false;}
    };
    const health=document.createElement("div");health.className="card health-settings";container.append(health);
    health.innerHTML=`<h2>${textHTML("hostFilter")}</h2><p class="sub">${textHTML("backgroundHelp")}</p><p class="sub">${textHTML("logoutHelp")}</p>${!trayInfo.available ? `<p class="sub">${escapeHtml(t("trayUnavailable",{reason:trayInfo.reason}))}</p>` : ""}<div class="server-grid" data-health-grid></div>`;
    page.drawHealth=()=>{
        const grid=$("[data-health-grid]",health),keep=new Set();
        for(const server of state.servers) {
            keep.add(server.id);
            let card=$(`[data-health-server="${server.id}"]`,grid);
            if(!card){card=document.createElement("div");card.dataset.healthServer=server.id;card.innerHTML='<h3></h3><p class="health-metrics"></p><div data-health-auth></div>';grid.append(card);}
            $("h3",card).textContent=server.label+" · "+t(server.role || "secondary");
            $(".health-metrics",card).textContent=healthText(hostHealth.get(server.id)) || t("loading");
            setHTML($("[data-health-auth]",card),server.connected ? `<button class="btn xs" data-settings-logout="${server.id}">${textHTML("logout")}</button>` : loginHTML(server));
        }
        for(const card of $$("[data-health-server]",grid))if(!keep.has(card.dataset.healthServer))card.remove();
    };
    health.onclick=async event=>{
        const button=event.target.closest("[data-settings-logout]");if(!button)return;
        button.disabled=true;
        try {const server=serverById(button.dataset.settingsLogout);await api(server,"/auth",{method:"DELETE"});server.connected=false;hostHealth.delete(server.id);renderPills();page.drawHealth();}catch(error){showError(error);button.disabled=false;}
    };
    bindLogins(health,{get active(){return page.active},refresh:async()=>page.drawHealth()});
    page.drawHealth();
    draw();
}

const gravityState = new Map();
async function renderGravity(container, page) {
    container.innerHTML=`<div class="card gravity-actions"><div class="toolbar"><button class="btn primary" data-gravity-all>${textHTML("updateGravityAll")}</button><span class="status-line" role="status" data-gravity-all-status></span></div><p class="sub">${textHTML("gravityAllHelp")}</p></div><div class="server-grid">${state.servers.map(s=>`<div class="card"><h2>${escapeHtml(s.label)} · ${textHTML("gravity")}</h2><div class="stat-row"><span class="k">${textHTML("lastGravity")}</span><span id="gravLast${s.id}">–</span></div>
        ${s.connected ? "" : loginHTML(s)}<button class="btn primary block" id="gravBtn${s.id}" ${!s.connected || gravityState.get(s.id)?.running ? "disabled" : ""}>${textHTML(gravityState.get(s.id)?.running ? "running" : "updateGravity")}</button><pre class="log" id="gravLog${s.id}" style="margin-top:12px"></pre></div>`).join("")}</div>`;
    function drawLog(server) {
        if (!page.active) return;
        const value=gravityState.get(server.id);
        const log=document.getElementById("gravLog"+server.id);
        log.closest(".card").classList.toggle("is-running",!!value?.running);
        log.textContent=value?.text || t("ready");log.scrollTop=log.scrollHeight;
        const button=document.getElementById("gravBtn"+server.id);
        button.disabled=!server.connected || !!value?.running;button.textContent=t(value?.running ? "running" : "updateGravity");
        if (server.connected) $(`[data-loginform="${server.id}"]`,container)?.remove();
        drawAllButton();
    }
    function drawAllButton() {
        if(!page.active)return;
        const configured=state.servers.filter(s=>s.url),running=configured.filter(s=>gravityState.get(s.id)?.running).length,ready=configured.filter(s=>s.connected && !gravityState.get(s.id)?.running).length;
        $("[data-gravity-all]",container).disabled=!ready;
        $("[data-gravity-all-status]",container).textContent=t("gravityAllStatus",{ready,running,unavailable:configured.filter(s=>!s.connected).length});
    }
    const onProgress = event => {
        const server=serverById(event.detail.id);
        if (server) drawLog(server);
    };
    document.addEventListener("gravity-progress",onProgress);
    page.dispose=()=>document.removeEventListener("gravity-progress",onProgress);
    const notify=server=>document.dispatchEvent(new CustomEvent("gravity-progress",{detail:{id:server.id}}));
    page.refresh=async()=>{
        await Promise.all(state.servers.map(async server=>{
            drawLog(server);
            if (!server.connected) return;
            try {const data=await api(server,"/stats/summary");if (page.active) document.getElementById("gravLast"+server.id).textContent=fmtDate(data?.gravity?.last_update);}
            catch (error) {if (page.active) document.getElementById("gravLast"+server.id).textContent=humanError(error);}
        }));
    };
    bindLogins(container,page);
    async function startGravity(server) {
        if(!server.connected || gravityState.get(server.id)?.running)return;
        const value={running:true,text:""};gravityState.set(server.id,value);notify(server);
        let stream;
        try {
            const r=await nativeRequest(server.id,"/action/gravity?color=false","POST","",true);
            if (r.status < 200 || r.status >= 300) throw new Error("HTTP "+r.status);
            stream=r.stream;
            const decoder=new TextDecoder();
            for (;;) {
                const chunk=await nativeReadStream(stream);
                value.text+=decoder.decode(Uint8Array.from(atob(chunk.data),c=>c.charCodeAt(0)),{stream:!chunk.done});
                value.text=value.text.replace(/\x1b\[[0-9;]*[A-Za-z]/g,"").slice(-2*1024*1024);
                notify(server);
                if (chunk.done) break;
            }
            value.text+="\n— "+t("finished")+" —";
        } catch (error) {value.text+="\n"+humanError(error);}
        finally {if (stream) {try {await nativeCancelStream(stream);} catch { /* The application may already be shutting down. */ }}value.running=false;notify(server);if (page.active) await page.refresh();}
    }
    for (const server of state.servers) document.getElementById("gravBtn"+server.id).onclick=()=>startGravity(server);
    $("[data-gravity-all]",container).onclick=()=>Promise.all(state.servers.filter(s=>s.url).map(startGravity));
    await page.refresh();
}

async function bootstrap() {
    trayInfo=await nativeTrayStatus();
    const updateHealth=async()=>{
        try {hostHealth=new Map((await nativeMonitor()).map(h=>[h.id,h]));for(const s of state.servers){const h=hostHealth.get(s.id);if(h?.sampledAt && !s.checking)s.connected=h.authenticated;}renderPills();if (state.activeTab==="settings") state.page?.drawHealth?.();} catch { /* A closed native window ends polling. */ }
    };
    setInterval(()=>{if (!document.hidden) updateHealth();},2000);
    const saved=state.servers.filter(s=>s.url && s.password);
    const hash=decodeURIComponent(location.hash.slice(1));
    await switchTab(state.servers.every(s=>!s.url) ? "settings" : TAB_IDS.includes(hash) ? hash : "dashboard");
    await Promise.all(saved.map(async server=>{try {await login(server,server.password);} catch { /* Login cards show the localized failure and allow a corrected password. */ }}));
    state.ready=true;
    if (saved.length) {
        if (state.page?.refresh) await state.page.refresh();
        else renderPills();
    }
}
window.addEventListener("hashchange",()=>{
    const tab=decodeURIComponent(location.hash.slice(1));
    if (TAB_IDS.includes(tab) && tab !== state.activeTab) switchTab(tab).catch(showError);
});
bootstrap().catch(showError);
