// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
// Run with node --test tools/admin-secureboot.test.cjs. No browser dependencies: the Secure Boot block of the Server page and its banner are drawn by the page's own functions
// into a stand-in document, and the radio buttons' handlers are called the way a click would.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(process.env.BOOTLACE_ADMIN_HTML || 'internal/admin/web/index.html', 'utf8').replace(/\r\n/g,'\n').match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
const boot = source.lastIndexOf('// ---- start\n');
assert.ok(boot > 0);

function rig() {
  const requests = [], notices = [], asked = [];
  let answer = true, refreshed = 0;
  const element = () => ({innerHTML:'',textContent:'',value:'',checked:false,dataset:{},classList:{add(){},remove(){},toggle(){}},querySelector(){return null;},querySelectorAll(){return [];},insertAdjacentHTML(){},addEventListener(){},contains(){return false;},focus(){}});
  const card = element();
  const radios = {sbmode: [], sbscope: []};
  let turnOn = null;
  card.querySelector = sel => sel === '[data-sb-on]' && card.innerHTML.includes('data-sb-on') ? (turnOn = {}) : null;
  card.querySelectorAll = sel => {
    const name = (sel.match(/name="(\w+)"/) || [])[1];
    if (radios[name].html === card.innerHTML) return radios[name]; // (the same objects until the block is drawn again, so the handlers the page set are kept)
    const found = [...card.innerHTML.matchAll(new RegExp('<input type="radio" name="' + name + '" value="(\\w+)"( checked)?( disabled)?>', 'g'))].map(m => ({value: m[1], checked: !!m[2], disabled: !!m[3]}));
    found.html = card.innerHTML;
    radios[name] = found;
    return found;
  };
  const doc = {querySelector: s => s === '#sbcard' ? card : element(), querySelectorAll: () => [], documentElement: {dataset: {}}, body: element(), activeElement: null};
  const context = vm.createContext({document: doc, location: {hash: ''}, localStorage: {getItem() {return null;}}, matchMedia: () => ({matches: false}), setInterval() {}, clearInterval() {}, setTimeout() {}, clearTimeout() {}, console, window: {innerHeight: 900}, URLSearchParams,
    fetch: (path, opt) => new Promise(resolve => requests.push({path, opt, body: opt.body ? JSON.parse(opt.body) : undefined, resolve: (data, status = 200) => resolve({ok: status < 300, status, json: async () => data})}))});
  vm.runInContext(source.slice(0, boot), context);
  Object.assign(context, {notices, asked});
  vm.runInContext('toast = (message, bad) => notices.push((bad ? "!" : "") + message); ask = async (title, body, yes) => { asked.push({title, body, yes}); return globalThis.__answer; }', context);
  const set = v => { answer = v; context.__answer = v; };
  set(true);
  return {
    card, radios, requests, notices, asked, set,
    get turnOn() {return turnOn;},
    get refreshed() {return refreshed;},
    draw: sb => { context.__sb = sb; context.__refresh = () => { refreshed++; }; vm.runInContext('drawSecureBoot(__sb, __refresh)', context); },
    banner: (sb, link) => { context.__sb = sb; return vm.runInContext('sbBanner(__sb, ' + !!link + ')', context); },
    html: () => card.innerHTML,
  };
}

const files = [{name: 'ipxe-shimx64.efi', role: 'the boot file the PC is told to start (the shim, signed by Microsoft)', required: true, present: true, size: 1048424, sha256: '5eecca2780bd49c900565e124516a1bd666ec5e012825f34991b6ba1ef2fa6cf', release: 'ipxe-16.1', source: 'https://github.com/ipxe/shim/releases/download/ipxe-16.1/ipxe-shimx64.efi', signedWith: ['2011', '2023']},
  {name: 'ipxe.efi', role: 'what the shim starts next (iPXE, signed by the iPXE project)', required: true, present: true, size: 397160, sha256: '6558e37887516b246d6a97122e8d18bedfe4197b7ba7f67bf1bf102a16678d33'}];
const builtin = {ok: true, release: 'v2.0.0', released: '2026-03-06', added: '2026-10-27', source: 'https://github.com/ipxe/ipxe/releases/download/v2.0.0/ipxeboot.tar.gz', tarballSha256: '01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1', files};
const base = {on: true, mode: 'builtin', scope: 'all', modeSource: 'default', scopeSource: 'default', modeLocked: false, scopeLocked: false, unset: false, shim: 'ipxe-shimx64.efi', marked: 0, files, ready: true, problem: '', message: 'The Secure Boot route is on.', builtin, own: {dir: '/data/secure-boot', dirFromOption: false, shim: 'shimx64.efi', files: [], ready: false, problem: 'shimx64.efi is missing from /data/secure-boot'}};

test('built in: the way and the scope are chosen with radio buttons, the files, release and checksums are shown', () => {
  const r = rig(); r.draw(base);
  const h = r.html();
  assert.match(h, /<input type="radio" name="sbmode" value="builtin" checked>/);
  assert.match(h, /<input type="radio" name="sbmode" value="own">/);
  assert.match(h, /<input type="radio" name="sbmode" value="off">/);
  assert.match(h, /<input type="radio" name="sbscope" value="all" checked>/);
  assert.match(h, /<legend>Way<\/legend>/);
  for (const text of ['ipxe-shimx64.efi', 'ipxe.efi', 'v2.0.0', '2026-03-06', '01a526d4cc791fc30362259c609d6c506cc64a7bdff51b9a5eb788354e17eee1', '5eecca2780bd49c900565e124516a1bd666ec5e012825f34991b6ba1ef2fa6cf'.slice(0, 16), 'ipxe-16.1', "Microsoft's 2011 and 2023 CA", 'built in', 'The default of a new installation']) assert.ok(h.includes(text), text);
  for (const way of ['Built in.', 'Own key.', 'Off.']) assert.ok(h.includes(way), way);
  assert.ok(!h.includes(' disabled'), 'nothing is locked');
  assert.ok(!h.includes('data-sb-on'), 'a chosen installation gets no banner');
  assert.match(h, /Secure Boot route <span class="badge ok">on<\/span>/);
});

test('what an option decides is shown as such and cannot be changed', () => {
  const r = rig(); r.draw({...base, mode: 'own', modeSource: 'option', modeLocked: true, scope: 'marked', scopeSource: 'option', scopeLocked: true, dir: '/sb', files: [{name: 'shimx64.efi', role: 'x', required: true, present: true}]});
  const h = r.html();
  assert.match(h, /<input type="radio" name="sbmode" value="own" checked disabled>/);
  assert.match(h, /<input type="radio" name="sbscope" value="marked" checked disabled>/);
  assert.ok(h.includes('Set by the command line or the environment (-secure-boot, -secure-boot-dir or BOOTLACE_SECURE_BOOT)'));
  assert.ok(h.includes('-secure-boot-scope, -secure-boot-all or BOOTLACE_SECURE_BOOT_SCOPE'));
});

test('own key: the folder and the missing files are shown, off shows no file table', () => {
  const r = rig(); r.draw({...base, mode: 'own', dir: '/data/secure-boot', shim: 'shimx64.efi', ready: false, problem: 'grubx64.efi is missing from /data/secure-boot: the shim asks for it next', message: 'grubx64.efi is missing', files: [{name: 'shimx64.efi', role: 'the shim', required: true, present: true}, {name: 'grubx64.efi', role: 'iPXE', required: true, present: false}]});
  assert.ok(r.html().includes('/data/secure-boot'));
  assert.match(r.html(), /<span class="badge red">missing<\/span>/);
  assert.match(r.html(), /on, incomplete/);
  const off = rig(); off.draw({...base, on: false, mode: 'off', scope: 'marked', files: [], ready: false, message: 'The Secure Boot route is off.'});
  assert.match(off.html(), /<input type="radio" name="sbmode" value="off" checked>/);
  assert.ok(!off.html().includes('ipxe-shimx64.efi</td>'));
  assert.ok(off.html().includes('The own-key way is not set up yet, which does not matter while you use another way'));
  assert.ok(off.html().includes('the folder (/data/secure-boot) is not ready'));
});

test('an installation that has not chosen is offered the switch, here and on the dashboard', async () => {
  const r = rig(); const sb = {...base, on: false, mode: 'off', scope: 'marked', unset: true, files: [], ready: false};
  r.draw(sb);
  assert.ok(r.html().includes('Secure Boot support is built in: turn it on'));
  assert.ok(r.html().includes('Not chosen yet'));
  assert.ok(r.turnOn, 'the button exists');
  const dash = r.banner(sb, true);
  assert.ok(dash.includes('Secure Boot support is built in: turn it on') && dash.includes('data-sb-on') && dash.includes('Choose how'));
  assert.equal(r.banner(base, true), '');
  assert.ok(r.banner({...base, problem: 'The Secure Boot files built into this program do not match their manifest'}, true).includes('banner bad'));
  r.turnOn.onclick();
  assert.equal(r.requests.length, 1);
  assert.equal(r.requests[0].path, '/api/secureboot');
  assert.equal(r.requests[0].opt.method, 'PUT');
  assert.deepEqual(r.requests[0].body, {mode: 'builtin'});
  assert.equal(r.requests[0].opt.headers['X-Bootlace'], '1');
  r.requests[0].resolve({...base});
  await new Promise(res => setImmediate(res));
  assert.deepEqual(r.notices, ['Saved']);
  assert.equal(r.refreshed, 1);
});

test('choosing the own key asks first and says that every PC needs the key enrolled', async () => {
  const r = rig(); r.draw({...base, scope: 'all'});
  const handlers = r.card.querySelectorAll('input[name="sbmode"]'); // (with the handlers the page attached)
  assert.equal(handlers.length, 3);
  r.set(false);
  const ownRadio = handlers.find(x => x.value === 'own'); ownRadio.checked = true;
  await ownRadio.onchange();
  assert.equal(r.asked.length, 1);
  assert.equal(r.asked[0].title, 'Switch to your own key?');
  assert.ok(r.asked[0].body.includes('has to be enrolled in every PC first') && r.asked[0].body.includes('Every 64-bit UEFI PC'));
  assert.equal(r.requests.length, 0, 'nothing is sent when the person says no');
  assert.equal(r.refreshed, 1, 'the page puts the old choice back');
  r.set(true);
  const done = ownRadio.onchange();
  await new Promise(res => setImmediate(res));
  assert.deepEqual(r.requests.map(x => x.body), [{mode: 'own'}]);
  r.requests[0].resolve({...base, mode: 'own'});
  await done;
  assert.deepEqual(r.notices, ['Saved']);
});

test('choosing every PC asks first; going back to the marked ones and the other ways need no question; a refusal is shown', async () => {
  const r = rig(); r.draw({...base, scope: 'marked', mode: 'builtin'});
  const scope = r.card.querySelectorAll('input[name="sbscope"]');
  const all = scope.find(x => x.value === 'all'); all.checked = true;
  r.set(false); await all.onchange();
  assert.equal(r.asked[0].title, 'Send every 64-bit UEFI PC through the shim?');
  assert.ok(r.asked[0].body.includes('BIOS PCs and ARM64 PCs are not affected'));
  assert.equal(r.requests.length, 0);
  r.set(true);
  const sent = all.onchange(); await new Promise(res => setImmediate(res));
  assert.deepEqual(r.requests[0].body, {scope: 'all'});
  r.requests[0].resolve({...base}); await sent;

  const r2 = rig(); r2.draw({...base, scope: 'all'});
  const marked = r2.card.querySelectorAll('input[name="sbscope"]').find(x => x.value === 'marked'); marked.checked = true;
  const p = marked.onchange(); await new Promise(res => setImmediate(res));
  assert.equal(r2.asked.length, 0);
  assert.deepEqual(r2.requests[0].body, {scope: 'marked'});
  r2.requests[0].resolve({...base}); await p;

  const r3 = rig(); r3.draw({...base, mode: 'own', scope: 'all'});
  const builtinRadio = r3.card.querySelectorAll('input[name="sbmode"]').find(x => x.value === 'builtin'); builtinRadio.checked = true;
  const q = builtinRadio.onchange(); await new Promise(res => setImmediate(res));
  assert.equal(r3.asked.length, 0);
  r3.requests[0].resolve({error: 'the mode is set by the command line'}, 409);
  await q;
  assert.deepEqual(r3.notices, ['!the mode is set by the command line']);
  assert.equal(r3.refreshed, 1, 'the radio goes back to what is true');
});

test('text from the server is escaped', () => {
  const r = rig(); r.draw({...base, mode: 'own', dir: '<img src=x onerror=alert(1)>', message: '<b>m</b>', problem: '<i>p</i>', files: [{name: '<script>x</script>', role: '<u>r</u>', required: true, present: false}], own: {...base.own, dir: '<img src=x>'}});
  const h = r.html();
  assert.ok(!h.includes('<img src=x') && !h.includes('<script>x') && !h.includes('<b>m</b>') && !h.includes('<u>r</u>'));
  assert.ok(h.includes('&lt;img src=x onerror=alert(1)&gt;'));
});
