'use strict';
(function () {

const LS_KEY = 'intangles_sim_v1';

const REGION_SEED = {
  'eu-north':  { label:'EU North',   sub:'Ireland',      color:'#818cf8', apiBase:'https://apis.intangles-aws-eu-north-1.eu.intangles.com',   apiToken:'RlOLZw2on9fP2edIOuGeQbZBGuxxBnUBEXkwLpiec1H9cCO6wsDo4mmOvMwgXUhD', userToken:'', sessionToken:'1B2dG2mhIV0IjdlOGLCfEhKCsXLb6V', mqttProtocol:'mqtts', mqttBroker:'mqttsecure.intangles-aws-eu-north-1.eu.intangles.com',  mqttPort:8884, mqttBrokerPlain:'mqtt.intangles-aws-eu-north-1.eu.intangles.com', mqttPortPlain:1883, tlsCerts:'embedded', uploadUrl:'https://device-history-server.intangles-aws-eu-north-1.eu.intangles.com/upload',  rmqHost:'rabbitmqcluster-prod.default.svc.cluster.local', rmqPort:5672, rmqVhost:'prod_new', rmqUser:'appuser', rmqPassword:'Ni1sYg0VfUmVibf', rmqQueue:'message.parser3', rmqCompression:'gzip', encryptEnabled:true, aesVersion:'2', rsaKey:'embedded' },
  'us-east':   { label:'US East',    sub:'Virginia',     color:'#34d399', apiBase:'https://apis.intangles-aws-us-east-1.us.intangles.com',    apiToken:'', userToken:'', sessionToken:'', mqttProtocol:'mqtts', mqttBroker:'mqttsecure.intangles-aws-us-east-1.us.intangles.com',   mqttPort:8884, mqttBrokerPlain:'', mqttPortPlain:1883, tlsCerts:'embedded', uploadUrl:'https://device-history-server.us.intangles.com/upload',   rmqHost:'rabbitmq.us.intangles.com', rmqPort:5672, rmqVhost:'', rmqUser:'', rmqPassword:'', rmqQueue:'message.parser3', rmqCompression:'gzip', encryptEnabled:true, aesVersion:'2', rsaKey:'embedded' },
  'ap-south':  { label:'AP South',   sub:'Mumbai',       color:'#fb923c', apiBase:'https://apis.intangles-aws-ap-south-1.ap.intangles.com',   apiToken:'', userToken:'', sessionToken:'', mqttProtocol:'mqtts', mqttBroker:'mqttsecure.intangles-aws-ap-south-1.ap.intangles.com',  mqttPort:8884, mqttBrokerPlain:'', mqttPortPlain:1883, tlsCerts:'embedded', uploadUrl:'https://device-history-server.ap.intangles.com/upload',  rmqHost:'rabbitmq.ap.intangles.com',  rmqPort:5672, rmqVhost:'', rmqUser:'', rmqPassword:'', rmqQueue:'message.parser3', rmqCompression:'gzip', encryptEnabled:true, aesVersion:'2', rsaKey:'embedded' },
  'in-central':{ label:'IN Central', sub:'Pune',         color:'#f472b6', apiBase:'https://apis.intangles-aws-in-central-1.in.intangles.com', apiToken:'', userToken:'', sessionToken:'', mqttProtocol:'mqtts', mqttBroker:'mqttsecure.intangles-aws-in-central-1.in.intangles.com',mqttPort:8884, mqttBrokerPlain:'', mqttPortPlain:1883, tlsCerts:'embedded', uploadUrl:'https://device-history-server.in.intangles.com/upload', rmqHost:'rabbitmq.in.intangles.com',  rmqPort:5672, rmqVhost:'', rmqUser:'', rmqPassword:'', rmqQueue:'message.parser3', rmqCompression:'gzip', encryptEnabled:true, aesVersion:'2', rsaKey:'embedded' },
};

const GLOBAL_SEED = {
  batchSize:30, apiPageSize:1000, apiRequestDelay:300, outputDir:'',
  batchUploadDelayMs:120000, gpsL1IntervalMs:10000, obdAccumIntervalMs:120000, normalModeIntervalMs:60000,
};

const TEST_SEED = [
  { id:1, name:'EU-Test-1',   regionKey:'eu-north',  srcImei:'869305070942563', tgtImei:'869305073861125', from:'2026-09-14T07:30', until:'2026-09-15T07:30', histEnd:'2026-09-14T19:30', mode:'simulate', createdAt:'2026-09-14T06:00:00Z' },
  { id:2, name:'EU-Test-2',   regionKey:'eu-north',  srcImei:'869305070942563', tgtImei:'869305073861126', from:'2026-09-13T07:30', until:'2026-09-14T07:30', histEnd:'2026-09-13T19:30', mode:'simulate', createdAt:'2026-09-13T06:00:00Z' },
  { id:3, name:'Pipeline-AP', regionKey:'ap-south',  srcImei:'869305070942563', tgtImei:'869305073861127', from:'2026-09-12T00:00', until:'2026-09-13T00:00', histEnd:'2026-09-12T12:00', mode:'pipeline', createdAt:'2026-09-12T06:00:00Z' },
];

const RUN_SEED = [
  { id:'r-1725264034', testId:1, testName:'EU-Test-1',   regionKey:'eu-north',  srcImei:'869305070942563', tgtImei:'869305073861125', from:'2026-09-14T07:30', until:'2026-09-15T07:30', histEnd:'2026-09-14T19:30', mode:'simulate', startedAt:'2026-09-14T08:00:00Z', completedAt:'2026-09-14T08:00:34Z', status:'done',    durationSec:34.4, metrics:{ totalPackets:1243, historicPackets:892, liveGps:198,  liveObd:153, batchesUploaded:30, errors:0 } },
  { id:'r-1725177600', testId:3, testName:'Pipeline-AP', regionKey:'ap-south',  srcImei:'869305070942563', tgtImei:'869305073861127', from:'2026-09-12T00:00', until:'2026-09-13T00:00', histEnd:'2026-09-12T12:00', mode:'pipeline', startedAt:'2026-09-12T15:30:00Z', completedAt:'2026-09-12T15:30:28Z', status:'done',    durationSec:28.1, metrics:{ totalPackets:987,  historicPackets:712, liveGps:156,  liveObd:119, batchesUploaded:24, errors:0 } },
  { id:'r-1725091200', testId:1, testName:'EU-Test-1',   regionKey:'eu-north',  srcImei:'869305070942563', tgtImei:'869305073861125', from:'2026-09-13T07:30', until:'2026-09-14T07:30', histEnd:'2026-09-13T19:30', mode:'simulate', startedAt:'2026-09-13T09:15:00Z', completedAt:'2026-09-13T09:15:13Z', status:'stopped', durationSec:12.7, metrics:{ totalPackets:847,  historicPackets:610, liveGps:0,    liveObd:0,   batchesUploaded:11, errors:0 } },
];

function mkDefault() {
  return {
    version: 1,
    activeTestId: 1,
    activeRegionKey: null,
    regions: JSON.parse(JSON.stringify(REGION_SEED)),
    tests:   JSON.parse(JSON.stringify(TEST_SEED)),
    runs:    JSON.parse(JSON.stringify(RUN_SEED)),
    global:  JSON.parse(JSON.stringify(GLOBAL_SEED)),
    theme: { name:'dark', vars:{}, fontSize:12, fontFamily:'', monoFamily:'', fontUrl:'' },
    customThemes: [],
    mqttProfiles: [],
  };
}

let _s = (() => {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) {
      const p = JSON.parse(raw);
      if (p && p.version === 1) {
        for (const k of Object.keys(REGION_SEED)) {
          if (!p.regions[k]) p.regions[k] = JSON.parse(JSON.stringify(REGION_SEED[k]));
          else p.regions[k] = { ...JSON.parse(JSON.stringify(REGION_SEED[k])), ...p.regions[k] };
        }
        if (!p.global) p.global = JSON.parse(JSON.stringify(GLOBAL_SEED));
        // migrate: old relative path is unusable inside .app bundle
        if (p.global.outputDir === './sim_output') p.global.outputDir = '';
        if (!p.hasOwnProperty('activeRegionKey')) p.activeRegionKey = null;
        if (!p.hasOwnProperty('setupComplete')) p.setupComplete = false;
        if (!p.theme) p.theme = { name:'dark', vars:{}, fontSize:12, fontFamily:'', monoFamily:'', fontUrl:'' };
        if (!p.theme.fontFamily) p.theme.fontFamily = '';
        if (!p.customThemes) p.customThemes = [];
        if (!p.mqttProfiles) p.mqttProfiles = [];
        return p;
      }
    }
  } catch (e) {}
  return mkDefault();
})();

function _save() {
  try { localStorage.setItem(LS_KEY, JSON.stringify(_s)); } catch (e) { console.error('[SimState] save failed', e); }
}

window.SimState = {
  // ── raw ──────────────────────────────────────────
  raw: () => _s,
  persist: _save,

  // ── active region ─────────────────────────────────
  getActiveRegionKey() {
    if (_s.activeRegionKey) return _s.activeRegionKey;
    const t = this.getActiveTest();
    return (t && t.regionKey) ? t.regionKey : Object.keys(_s.regions)[0];
  },
  setActiveRegionKey(key) { _s.activeRegionKey = key; _save(); },

  // ── regions ──────────────────────────────────────
  getRegions: () => _s.regions,
  getRegion: k => _s.regions[k] || null,
  regionKeys: () => Object.keys(_s.regions),
  setRegion(k, patch) { if (!_s.regions[k]) _s.regions[k] = {}; Object.assign(_s.regions[k], patch); _save(); },
  regionColor: k => (_s.regions[k] || {}).color || '#8b7db8',
  regionLabel: k => (_s.regions[k] || {}).label || k,

  // ── tests ─────────────────────────────────────────
  getTests: () => _s.tests,
  getTest: id => _s.tests.find(t => t.id === id) || null,
  getActiveTest() { return _s.tests.find(t => t.id === _s.activeTestId) || _s.tests[0] || null; },
  setActiveTest(id) { _s.activeTestId = id; _save(); },
  saveTest(t) {
    const i = _s.tests.findIndex(x => x.id === t.id);
    if (i >= 0) _s.tests[i] = { ..._s.tests[i], ...t }; else _s.tests.push(t);
    _save();
  },
  deleteTest(id) { _s.tests = _s.tests.filter(t => t.id !== id); if (_s.activeTestId === id) _s.activeTestId = (_s.tests[0] || {}).id || null; _save(); },
  nextTestId: () => Math.max(0, ..._s.tests.map(t => t.id)) + 1,

  // ── runs (history) ────────────────────────────────
  getRuns: () => _s.runs,
  addRun(r) { _s.runs.unshift(r); if (_s.runs.length > 500) _s.runs = _s.runs.slice(0, 500); _save(); },
  updateRun(id, patch) { const r = _s.runs.find(x => x.id === id); if (r) { Object.assign(r, patch); _save(); } },
  deleteRun(id) { _s.runs = _s.runs.filter(r => r.id !== id); _save(); },
  clearRuns() { _s.runs = []; _save(); },

  // ── global settings ───────────────────────────────
  getGlobal: () => _s.global,
  setGlobal(patch) { Object.assign(_s.global, patch); _save(); },

  // ── utils ─────────────────────────────────────────
  newRunId: () => 'r-' + Date.now(),
  reset() { _s = mkDefault(); _save(); },

  // ── theme ─────────────────────────────────────────
  getTheme: () => _s.theme || { name:'dark', vars:{}, fontSize:12, fontFamily:'', monoFamily:'', fontUrl:'' },
  setTheme(patch) {
    if (!_s.theme) _s.theme = { name:'dark', vars:{}, fontSize:12, fontFamily:'', monoFamily:'', fontUrl:'' };
    Object.assign(_s.theme, patch);
    _save();
  },
  applyTheme() {
    const t = _s.theme || {};
    const root = document.documentElement;
    const ALL_VARS = ['--bg0','--bg1','--bg2','--bg3','--border','--t0','--t1','--t2','--blue','--green','--yellow','--red','--purple','--orange'];
    ALL_VARS.forEach(k => root.style.removeProperty(k));
    Object.entries(t.vars || {}).forEach(([k,v]) => root.style.setProperty(k, v));
    // font families
    if (t.fontFamily) root.style.setProperty('--font-ui', t.fontFamily);
    else root.style.removeProperty('--font-ui');
    if (t.monoFamily) root.style.setProperty('--font-mono', t.monoFamily);
    else root.style.removeProperty('--font-mono');
    // load web font link
    const prevLink = document.querySelector('link[data-simfont]');
    if (t.fontUrl) {
      if (!prevLink || prevLink.getAttribute('href') !== t.fontUrl) {
        if (prevLink) prevLink.remove();
        const lnk = document.createElement('link');
        lnk.rel = 'stylesheet'; lnk.setAttribute('href', t.fontUrl);
        lnk.setAttribute('data-simfont', '');
        document.head.appendChild(lnk);
      }
    } else if (prevLink) { prevLink.remove(); }
    // font size
    let el = document.getElementById('_simFontStyle');
    if (!el) { el = document.createElement('style'); el.id = '_simFontStyle'; document.head.appendChild(el); }
    el.textContent = t.fontSize && t.fontSize !== 12 ? `body{font-size:${t.fontSize}px!important}` : '';
    // button style
    const btnRadii = { pill:'50px', sharp:'2px', soft:'10px', default:'5px' };
    root.style.setProperty('--btn-radius', btnRadii[t.buttonStyle || 'default'] || '5px');
    // density
    document.body.classList.remove('density-compact','density-spacious');
    if (t.density === 'compact') document.body.classList.add('density-compact');
    else if (t.density === 'spacious') document.body.classList.add('density-spacious');
    // reduce motion
    document.body.classList.toggle('reduce-motion', !!t.reduceMotion);
  },
  // ── setup ─────────────────────────────────────────
  isSetupDone: () => !!_s.setupComplete,
  markSetupDone(outputDir) {
    _s.setupComplete = true;
    if (outputDir !== undefined) Object.assign(_s.global, { outputDir: outputDir || '' });
    _save();
  },

  // ── custom themes ─────────────────────────────────
  getCustomThemes: () => _s.customThemes || [],
  saveCustomTheme(ct) {
    if (!_s.customThemes) _s.customThemes = [];
    const i = _s.customThemes.findIndex(x => x.key === ct.key);
    if (i >= 0) _s.customThemes[i] = ct; else _s.customThemes.push(ct);
    _save();
  },
  deleteCustomTheme(key) { _s.customThemes = (_s.customThemes || []).filter(t => t.key !== key); _save(); },

  // ── MQTT saved profiles ───────────────────────────────
  getMqttProfiles: () => _s.mqttProfiles || [],
  saveMqttProfile(p) {
    if (!_s.mqttProfiles) _s.mqttProfiles = [];
    const i = _s.mqttProfiles.findIndex(x => x.id === p.id);
    if (i >= 0) _s.mqttProfiles[i] = p; else _s.mqttProfiles.push(p);
    _save();
  },
  deleteMqttProfile(id) { _s.mqttProfiles = (_s.mqttProfiles || []).filter(p => p.id !== id); _save(); },
};

window.REGION_SEED = REGION_SEED;

})();
