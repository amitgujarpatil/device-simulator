'use strict';
(function () {

// ── SVG icon definitions ────────────────────────────────────────────────────
const _STROKE = {
  run:      `<polygon points="5,2.5 15.5,9 5,15.5"/>`,
  logs:     `<line x1="3" y1="5" x2="15" y2="5"/><line x1="3" y1="9" x2="15" y2="9"/><line x1="3" y1="13" x2="11" y2="13"/>`,
  mqtt:     `<polyline points="4,7 13,7 10.5,4.5"/><polyline points="14,11 5,11 7.5,13.5"/>`,
  apiclient:`<polygon points="10.5,2 5.5,9.5 9.5,9.5 7.5,16 14,8.5 10,8.5"/>`,
  mongo:    `<ellipse cx="9" cy="5" rx="5.5" ry="2.5"/><path d="M3.5 5v8c0 1.38 2.46 2.5 5.5 2.5s5.5-1.12 5.5-2.5V5"/><line x1="9" y1="2.5" x2="9" y2="15.5"/>`,
  utilities:`<rect x="2.5" y="2.5" width="5" height="5" rx="1"/><rect x="10.5" y="2.5" width="5" height="5" rx="1"/><rect x="2.5" y="10.5" width="5" height="5" rx="1"/><rect x="10.5" y="10.5" width="5" height="5" rx="1"/>`,
  history:  `<circle cx="9" cy="9" r="7"/><polyline points="9,5 9,9 12.5,11"/>`,
  metrics:  `<polyline points="2,14 5.5,8 9,11.5 12.5,4.5 16,7"/>`,
  settings: `<circle cx="9" cy="9" r="3"/><path d="M9 1.5v2M9 14.5v2M1.5 9h2M14.5 9h2M3.6 3.6l1.4 1.4M13 13l1.4 1.4M14.4 3.6l-1.4 1.4M5 13l-1.4 1.4"/>`,
  theme:    `<path d="M9 1.5l7.5 7.5-7.5 7.5-7.5-7.5z"/><circle cx="9" cy="9" r="3"/>`,
};
const _FILL = {
  run:      `<path d="M5 2l11 7-11 7z"/>`,
  logs:     `<rect x="3" y="4" width="12" height="2" rx="1"/><rect x="3" y="8" width="12" height="2" rx="1"/><rect x="3" y="12" width="8" height="2" rx="1"/>`,
  mqtt:     `<path d="M4 6.5h9V5L16.5 7.5 13 10V8.5H4zm10 5H5V10L1.5 12.5 5 15v-1.5h9z"/>`,
  apiclient:`<path d="M10.5 2H5.5L3.5 10H9l-2.5 6 8.5-10H10z"/>`,
  mongo:    `<path d="M9 2C6 2 3.5 3.3 3.5 5v8c0 1.7 2.5 3 5.5 3s5.5-1.3 5.5-3V5C14.5 3.3 12 2 9 2zm0 1.5c2.5 0 4 .9 4 1.5s-1.5 1.5-4 1.5S5 5.6 5 5s1.5-1.5 4-1.5z"/>`,
  utilities:`<rect x="2" y="2" width="6" height="6" rx="1.5"/><rect x="10" y="2" width="6" height="6" rx="1.5"/><rect x="2" y="10" width="6" height="6" rx="1.5"/><rect x="10" y="10" width="6" height="6" rx="1.5"/>`,
  history:  `<path d="M9 2a7 7 0 1 0 0 14A7 7 0 0 0 9 2zm.5 4.5v4.2l3.3 2-1 1.6-4.3-2.6V6.5h2z"/>`,
  metrics:  `<rect x="2" y="8" width="3" height="6" rx=".5"/><rect x="7.5" y="3.5" width="3" height="10.5" rx=".5"/><rect x="13" y="6" width="3" height="8" rx=".5"/>`,
  settings: `<path d="M9 6.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5zm6.5-.7-.9-2-1.9.5a4.8 4.8 0 0 0-1.2-.7l-.3-2H7.8l-.3 2a4.8 4.8 0 0 0-1.2.7L4.4 3.8l-.9 2 1.5 1.2c-.1.4-.1.8-.1 1.3s0 .9.1 1.3L3.5 11l.9 2 1.9-.5a4.8 4.8 0 0 0 1.2.7l.3 2h2.4l.3-2a4.8 4.8 0 0 0 1.2-.7l1.9.5.9-2-1.5-1.2c.1-.4.1-.8.1-1.3s0-.9-.1-1.3z"/>`,
  theme:    `<path d="M9 1L17 9 9 17 1 9ZM9 5.5A3.5 3.5 0 0 0 5.5 9 3.5 3.5 0 0 0 9 12.5 3.5 3.5 0 0 0 12.5 9 3.5 3.5 0 0 0 9 5.5Z" fill-rule="evenodd"/>`,
};
const ICON_PACKS = {
  outline: { sw:'1.5', lc:'round',  lj:'round', filled:false },
  bold:    { sw:'2.5', lc:'round',  lj:'round', filled:false },
  sharp:   { sw:'1.5', lc:'square', lj:'miter', filled:false },
  filled:  { sw:'0',   lc:'round',  lj:'round', filled:true  },
};
function _icon(key, packKey) {
  const pk = ICON_PACKS[packKey] || ICON_PACKS.outline;
  const body = pk.filled ? (_FILL[key] || _STROKE[key]) : (_STROKE[key] || '');
  const attrs = pk.filled
    ? `fill="currentColor" stroke="none"`
    : `fill="none" stroke="currentColor" stroke-width="${pk.sw}" stroke-linecap="${pk.lc}" stroke-linejoin="${pk.lj}"`;
  return `<svg viewBox="0 0 18 18" width="15" height="15" ${attrs} style="display:block">${body}</svg>`;
}

const NAV_CSS = `
:root{--bg0:#09080e;--bg1:#100e1a;--bg2:#181526;--bg3:#221e33;--border:#2d2a40;--t0:#f0eaff;--t1:#b8acdc;--t2:#8878b8;--blue:#a78bfa;--green:#34d399;--yellow:#fbbf24;--red:#f87171;--purple:#818cf8;--orange:#fb923c;--font-ui:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;--font-mono:'SF Mono','Cascadia Code',Consolas,monospace}
*{box-sizing:border-box;margin:0;padding:0}
html{zoom:1.25}
body{font-family:var(--font-ui);background:var(--bg0);color:var(--t0);height:100vh;display:flex;flex-direction:row;overflow:hidden;font-size:12px}
.side-nav{width:44px;min-width:44px;background:var(--bg1);border-right:1px solid var(--border);display:flex;flex-direction:column;align-items:center;padding:0 0 8px;gap:2px;flex-shrink:0;z-index:20}
.sn-trafficpad{height:36px;width:100%;flex-shrink:0;--wails-draggable:drag}
.sn-logo{font-size:14px;font-weight:900;color:var(--blue);margin-bottom:10px;user-select:none;--wails-draggable:no-drag}
/* ── Window drag regions (Wails v2 uses --wails-draggable, not -webkit-app-region) ── */
.hdr,.hist-header,.page-hdr,.ac-hdr,.ctrl-bar,.conn-status{--wails-draggable:drag}
button,select,input,textarea,a,.tg,.ibtn,.icon-btn,.fpill,.sn-icon,.status-badge,.tg-wrap,.sn-region,label{--wails-draggable:no-drag}
.sn-icon{width:32px;height:32px;border-radius:6px;display:flex;align-items:center;justify-content:center;font-size:14px;color:var(--t2);text-decoration:none;transition:all .15s;position:relative;cursor:pointer}
.sn-icon:hover{background:var(--bg2);color:var(--t0)}
.sn-icon.on{background:rgba(167,139,250,.15);color:var(--blue)}
.sn-icon::after{content:attr(data-tip);position:absolute;left:40px;top:50%;transform:translateY(-50%);background:var(--bg3);color:var(--t0);font-size:10px;padding:3px 8px;border-radius:4px;white-space:nowrap;border:1px solid var(--border);pointer-events:none;opacity:0;transition:opacity .12s;z-index:99}
.sn-icon:hover::after{opacity:1}
.sn-spacer{flex:1}
.app-wrap{flex:1;display:flex;flex-direction:column;overflow:hidden;min-width:0}

/* ── shared form atoms ─────────────────────────────────────────── */
.inp{width:100%;padding:5px 8px;background:var(--bg2);border:none;border-radius:5px;color:var(--t0);font-size:11px;outline:none;transition:background .22s,box-shadow .22s}
.inp:focus{background:var(--bg3);box-shadow:0 4px 18px rgba(167,139,250,.16),0 1px 6px rgba(0,0,0,.28)}
select.inp{cursor:pointer;border:1px solid var(--border);padding:6px 26px 6px 10px;font-size:11px;min-height:32px;-webkit-appearance:none;appearance:none;background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%238878b8' stroke-width='1.5' fill='none' stroke-linecap='round'/%3E%3C/svg%3E");background-repeat:no-repeat;background-position:right 9px center;transition:border-color .15s,background-color .15s}
select.inp:hover{border-color:var(--t2);background-color:var(--bg3)}
select.inp:focus{border-color:var(--blue);box-shadow:0 0 0 2px rgba(167,139,250,.15);background-color:var(--bg3)}
.lbl{font-size:9px;color:var(--t1);font-weight:600;margin-bottom:3px;display:block;letter-spacing:.04em}
.fg{margin-bottom:9px}
.fg:last-child{margin-bottom:0}
.inp-row{display:flex;gap:5px}.inp-row .inp{flex:1}
.ibtn{padding:4px 9px;background:var(--bg3);border:1px solid var(--border);border-radius:4px;color:var(--t1);font-size:10px;cursor:pointer;white-space:nowrap;font-weight:600;outline:none;transition:all .14s}
.ibtn:hover{color:var(--t0);border-color:var(--t2);background:var(--bg2)}
.btn{padding:5px 13px;border-radius:var(--btn-radius,5px);border:none;cursor:pointer;font-size:11px;font-weight:700;display:inline-flex;align-items:center;gap:4px;transition:all .15s;outline:none}
.btn-go{background:var(--blue);color:#09080e}.btn-go:hover{filter:brightness(1.15);box-shadow:0 2px 14px rgba(167,139,250,.45)}.btn-go:disabled{background:var(--t2);color:var(--bg2);cursor:not-allowed}
.btn-stop{background:var(--red);color:#fff}.btn-stop:hover{background:#ff8080;box-shadow:0 2px 10px rgba(248,113,113,.45)}.btn-stop:disabled{background:var(--bg2);color:var(--t2);cursor:not-allowed}
.btn-ghost{background:var(--bg2);color:var(--t1);border:1px solid var(--border)}.btn-ghost:hover{background:var(--bg3);color:var(--t0);box-shadow:0 1px 6px rgba(0,0,0,.28)}
.tg{width:32px;height:17px;background:var(--bg2);border:1px solid var(--border);border-radius:9px;cursor:pointer;position:relative;transition:background .2s;flex-shrink:0}
.tg.on{background:var(--blue);border-color:var(--blue)}
.tg::after{content:'';position:absolute;top:2px;left:2px;width:11px;height:11px;background:#fff;border-radius:50%;transition:transform .2s}
.tg.on::after{transform:translateX(15px)}
.tg-row{display:flex;align-items:center;justify-content:space-between}
.tg-lbl{font-size:11px;color:var(--t1)}
.badge{display:inline-block;padding:1px 5px;border-radius:3px;font-size:9px;font-weight:700}
.badge-eu{background:rgba(129,140,248,.15);color:#818cf8}
.badge-us{background:rgba(52,211,153,.15);color:#34d399}
.badge-ap{background:rgba(251,146,60,.15);color:#fb923c}
.badge-in{background:rgba(244,114,182,.15);color:#f472b6}
.badge-done{background:rgba(52,211,153,.12);color:#34d399}
.badge-stopped{background:rgba(251,191,36,.12);color:#fbbf24}
.badge-error{background:rgba(248,113,113,.12);color:#f87171}
.badge-running{background:rgba(167,139,250,.14);color:#a78bfa}
.badge-idle{background:var(--bg2);color:var(--t2)}
.badge-simulate{background:rgba(167,139,250,.12);color:#a78bfa}
.badge-pipeline{background:rgba(251,146,60,.12);color:#fb923c}
.badge-fetch{background:rgba(129,140,248,.12);color:#818cf8}
.badge-validate{background:rgba(52,211,153,.12);color:#34d399}
.dot{width:6px;height:6px;border-radius:50%;flex-shrink:0}
.dot-done{background:#34d399}
.dot-stopped{background:#fbbf24}
.dot-error{background:#f87171}
.dot-running{background:#a78bfa}
.dot-idle{background:var(--t2)}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.5;transform:scale(1.55)}}
.dot-running{animation:pulse 1.4s ease-in-out infinite}
.kv{display:flex;justify-content:space-between;font-size:10px;padding:2px 0}
.kv .k{color:var(--t2)}.kv .v{font-family:monospace;color:var(--t0)}
.kv .v.ok{color:var(--green)}.kv .v.hi{color:var(--blue)}
.sb{background:var(--bg1);border-right:1px solid var(--border);overflow-y:auto;flex-shrink:0}
.sb::-webkit-scrollbar{width:3px}.sb::-webkit-scrollbar-thumb{background:var(--border);border-radius:2px}
.sn-region{width:34px;height:38px;border-radius:6px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:2px;cursor:pointer;position:relative;transition:background .15s;margin-bottom:2px}
.sn-region:hover{background:var(--bg2)}
.sn-region-dot{width:9px;height:9px;border-radius:50%;transition:background .2s}
.sn-region-lbl{font-size:7px;font-weight:700;color:var(--t2);letter-spacing:.03em}
.sn-rmenu{position:absolute;left:42px;bottom:0;background:var(--bg1);border:1px solid var(--border);border-radius:7px;padding:5px;min-width:150px;z-index:200;display:none;box-shadow:4px 6px 18px rgba(0,0,0,.6)}
.sn-rmenu.open{display:block}
.sn-rmenu-title{font-size:8px;font-weight:700;letter-spacing:.08em;color:var(--t2);text-transform:uppercase;padding:4px 8px 6px;border-bottom:1px solid var(--border);margin-bottom:4px}
.sn-ropt{display:flex;align-items:center;gap:8px;padding:6px 8px;border-radius:5px;cursor:pointer;transition:background .12s}
.sn-ropt:hover{background:var(--bg2)}
.sn-ropt.on{background:rgba(167,139,250,.1)}
.sn-ropt-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.sn-ropt-name{font-size:10px;font-weight:700;color:var(--t0)}
.sn-ropt.on .sn-ropt-name{color:var(--blue)}
.sn-ropt-sub{font-size:8px;color:var(--t2);margin-top:1px}
/* ── Button groups (SaaS-standard connected actions) ── */
.btn-group{display:inline-flex;align-items:stretch;border-radius:var(--btn-radius,5px);overflow:hidden;flex-shrink:0;box-shadow:0 1px 3px rgba(0,0,0,.2)}
.btn-group .btn{border-radius:0;flex-shrink:0;border-right:1px solid rgba(0,0,0,.12);box-shadow:none}
.btn-group .btn:last-child{border-right:none}
.btn-group .btn:first-child{border-radius:var(--btn-radius,5px) 0 0 var(--btn-radius,5px)}
.btn-group .btn:last-child{border-radius:0 var(--btn-radius,5px) var(--btn-radius,5px) 0}
.btn-group .btn:only-child{border-radius:var(--btn-radius,5px);border-right:none}
/* ── Segmented control ── */
.seg-ctrl{display:inline-flex;background:var(--bg2);border:1px solid var(--border);border-radius:var(--btn-radius,5px);padding:2px;gap:1px;flex-shrink:0}
.seg-btn{padding:3px 11px;border-radius:calc(max(2px,var(--btn-radius,5px) - 2px));font-size:10px;font-weight:700;cursor:pointer;border:none;background:transparent;color:var(--t2);transition:all .15s;white-space:nowrap;outline:none;letter-spacing:.03em;text-transform:uppercase;font-family:var(--font-ui);--wails-draggable:no-drag}
.seg-btn:hover:not([disabled]):not(.on){color:var(--t0);background:var(--bg3)}
.seg-btn.on{background:var(--bg1);color:var(--t0);box-shadow:0 1px 3px rgba(0,0,0,.3),inset 0 0 0 1px var(--border)}
.seg-btn[disabled]{opacity:.35;cursor:not-allowed;pointer-events:none}
/* ── Toolbar helpers ── */
.toolbar{display:flex;align-items:center;gap:4px}
.toolbar-sep{width:1px;height:20px;background:var(--border);flex-shrink:0;margin:0 3px}
/* ── Keyboard hint badges ── */
.kbd{display:inline-block;font-size:7px;font-family:var(--font-ui);background:rgba(255,255,255,.04);border:1px solid var(--border);border-bottom-width:2px;border-radius:3px;padding:1px 4px;color:var(--t2);line-height:1.4;letter-spacing:.02em;pointer-events:none;margin-left:4px;vertical-align:middle}
/* ── Density overrides ── */
body.density-compact .btn{padding:3px 10px!important;font-size:10px!important}
body.density-compact .btn.btn-sm{padding:2px 7px!important;font-size:9px!important}
body.density-compact .inp{padding:3px 7px!important;font-size:10px!important}
body.density-compact .hdr{height:38px!important}
body.density-compact .fg{margin-bottom:6px!important}
body.density-spacious .btn{padding:8px 18px!important;font-size:12px!important}
body.density-spacious .inp{padding:7px 12px!important;font-size:12px!important}
body.density-spacious .hdr{height:52px!important}
body.density-spacious .fg{margin-bottom:14px!important}
/* ── Reduce-motion ── */
body.reduce-motion *{transition-duration:.01ms!important;animation-duration:.01ms!important}
`;

window.SimNav = {
  icon: _icon,

  inject(activePage) {
    if (typeof SimState !== 'undefined') SimState.applyTheme();
    const packKey = (typeof SimState !== 'undefined' && SimState.getTheme().iconPack) || 'outline';
    const style = document.createElement('style');
    style.textContent = NAV_CSS;
    document.head.appendChild(style);

    const pages = [
      { href:'sim-run.html',      icon:_icon('run',      packKey), tip:'Run',         key:'run'       },
      { href:'sim-logs.html',     icon:_icon('logs',     packKey), tip:'Data Logs',   key:'logs'      },
      { href:'sim-mqtt.html',     icon:_icon('mqtt',     packKey), tip:'MQTT Client', key:'mqtt'      },
      { href:'api-client.html',   icon:_icon('apiclient',packKey), tip:'API Client',  key:'apiclient' },
      { href:'sim-mongo.html',    icon:_icon('mongo',    packKey), tip:'MongoDB',     key:'mongo'     },
      { href:'utilities.html',    icon:_icon('utilities',packKey), tip:'Utilities',   key:'utilities' },
      { href:'sim-history.html',  icon:_icon('history',  packKey), tip:'History',     key:'history'   },
      { href:'sim-metrics.html',  icon:_icon('metrics',  packKey), tip:'Metrics',     key:'metrics'   },
      { href:'sim-settings.html', icon:_icon('settings', packKey), tip:'Settings',    key:'settings'  },
      { href:'sim-theme.html',    icon:_icon('theme',    packKey), tip:'Theme',       key:'theme'     },
    ];

    const nav = document.createElement('nav');
    nav.className = 'side-nav';
    nav.innerHTML = '<div class="sn-trafficpad"></div><div class="sn-logo">◉</div>' +
      pages.map(p => `<a href="${p.href}" class="sn-icon${activePage===p.key?' on':''}" data-tip="${p.tip}">${p.icon}</a>`).join('') +
      '<div class="sn-spacer"></div>' +
      '<div class="sn-region" id="snRegion">' +
        '<div class="sn-region-dot" id="snRegionDot"></div>' +
        '<div class="sn-region-lbl" id="snRegionLbl">—</div>' +
        '<div class="sn-rmenu" id="snRmenu"><div class="sn-rmenu-title">Region</div></div>' +
      '</div>';
    document.body.insertBefore(nav, document.body.firstChild);

    document.getElementById('snRegion').addEventListener('click', e => {
      e.stopPropagation();
      document.getElementById('snRmenu').classList.toggle('open');
    });
    document.addEventListener('click', () => {
      const m = document.getElementById('snRmenu');
      if (m) m.classList.remove('open');
    });

    SimNav._refreshRegion();
  },

  _refreshRegion() {
    if (typeof SimState === 'undefined') return;
    const key = SimState.getActiveRegionKey();
    const r = key ? SimState.getRegion(key) : null;
    const dot = document.getElementById('snRegionDot');
    const lbl = document.getElementById('snRegionLbl');
    if (dot) dot.style.background = r ? r.color : 'var(--t2)';
    if (lbl) lbl.textContent = key ? SimNav.regionShort(key) : '—';
    const menu = document.getElementById('snRmenu');
    if (!menu) return;
    const title = menu.firstChild;
    menu.innerHTML = '';
    if (title) menu.appendChild(title);
    SimState.regionKeys().forEach(k => {
      const reg = SimState.getRegion(k) || {};
      const opt = document.createElement('div');
      opt.className = 'sn-ropt' + (k === key ? ' on' : '');
      opt.innerHTML =
        `<div class="sn-ropt-dot" style="background:${reg.color||'var(--t2)'}"></div>` +
        `<div><div class="sn-ropt-name">${reg.label||k}</div><div class="sn-ropt-sub">${reg.sub||''}</div></div>`;
      opt.addEventListener('click', e => { e.stopPropagation(); SimNav.selectRegion(k); });
      menu.appendChild(opt);
    });
  },

  selectRegion(key) {
    SimState.setActiveRegionKey(key);
    SimNav._refreshRegion();
    const m = document.getElementById('snRmenu');
    if (m) m.classList.remove('open');
    if (typeof window.onRegionChange === 'function') window.onRegionChange(key);
  },

  regionBadgeClass(key) {
    const map = { 'eu-north':'eu', 'us-east':'us', 'ap-south':'ap', 'in-central':'in' };
    return 'badge badge-' + (map[key] || 'idle');
  },

  regionLabel(key) {
    const r = window.SimState ? SimState.getRegion(key) : null;
    if (r) return r.label;
    const map = { 'eu-north':'EU North','us-east':'US East','ap-south':'AP South','in-central':'IN Central' };
    return map[key] || key;
  },

  regionShort(key) {
    const map = { 'eu-north':'EU','us-east':'US','ap-south':'AP','in-central':'IN' };
    return map[key] || key.substring(0,2).toUpperCase();
  },
};

})();
