'use strict';
(function () {

const NAV_CSS = `
:root{--bg0:#09080e;--bg1:#100e1a;--bg2:#181526;--bg3:#221e33;--border:#2d2a40;--t0:#f0eaff;--t1:#b8acdc;--t2:#8878b8;--blue:#a78bfa;--green:#34d399;--yellow:#fbbf24;--red:#f87171;--purple:#818cf8;--orange:#fb923c;--font-ui:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;--font-mono:'SF Mono','Cascadia Code',Consolas,monospace}
*{box-sizing:border-box;margin:0;padding:0}
html{zoom:1.25}
body{font-family:var(--font-ui);background:var(--bg0);color:var(--t0);height:100vh;display:flex;flex-direction:row;overflow:hidden;font-size:12px}
.side-nav{width:44px;min-width:44px;background:var(--bg1);border-right:1px solid var(--border);display:flex;flex-direction:column;align-items:center;padding:6px 0 8px;gap:2px;flex-shrink:0;z-index:20}
.sn-logo{font-size:14px;font-weight:900;color:var(--blue);margin-bottom:10px;user-select:none;padding-top:2px}
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
.btn{padding:5px 13px;border-radius:5px;border:none;cursor:pointer;font-size:11px;font-weight:700;display:inline-flex;align-items:center;gap:4px;transition:all .15s;outline:none}
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
`;

window.SimNav = {
  inject(activePage) {
    if (typeof SimState !== 'undefined') SimState.applyTheme();
    const style = document.createElement('style');
    style.textContent = NAV_CSS;
    document.head.appendChild(style);

    const pages = [
      { href:'sim-run.html',      icon:'▶', tip:'Run',      key:'run'      },
      { href:'sim-logs.html',     icon:'≡', tip:'Data Logs', key:'logs'     },
      { href:'sim-mqtt.html',     icon:'⇄', tip:'MQTT Client', key:'mqtt'  },
      { href:'sim-history.html',  icon:'⧖', tip:'History',  key:'history'  },
      { href:'sim-settings.html', icon:'⚙', tip:'Settings', key:'settings' },
      { href:'sim-theme.html',    icon:'◈', tip:'Theme',    key:'theme'    },
    ];

    const nav = document.createElement('nav');
    nav.className = 'side-nav';
    nav.innerHTML = '<div class="sn-logo">◉</div>' +
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
