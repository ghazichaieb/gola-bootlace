// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
// Run with node --test tools/admin-views.test.cjs. No browser dependencies.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(process.env.BOOTLACE_ADMIN_HTML || 'internal/admin/web/index.html', 'utf8').replace(/\r\n/g,'\n').match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
const boot = source.lastIndexOf('// ---- start\n');
assert.ok(boot > 0);
function fixture() {
  const pending = [], intervals = [], notices = [];
  let page, departed = null, staleWrites = 0, foreignReads = 0;
  function element() { return {innerHTML:'',textContent:'',value:'',checked:false,dataset:{},classList:{add(){},remove(){},toggle(){}},querySelector(){return element();},querySelectorAll(){return [];},insertAdjacentHTML(){},addEventListener(){},contains(){return false;},focus(){}}; }
  page = element();
  let html = ""; Object.defineProperty(page,"innerHTML",{get(){return html;},set(value){if(departed)staleWrites++;html=value;}});
  const doc = {querySelector:s=>{if(departed && s!=='#page')foreignReads++;return s==='#page'?page:element();},querySelectorAll:()=>[],documentElement:{dataset:{}},body:element(),activeElement:null};
  const context = vm.createContext({document:doc,location:{hash:''},localStorage:{getItem(){return null;}},matchMedia:()=>({matches:false}),setInterval:fn=>{intervals.push(fn);return intervals.length;},clearInterval(){},setTimeout(){},clearTimeout(){},console,window:{innerHeight:900},URLSearchParams,fetch:(...args)=>new Promise((resolve,reject)=>pending.push({resolve,reject,args}))});
  vm.runInContext(source.slice(0,boot),context);
  context.notices=notices;
  vm.runInContext('toast = message => notices.push(message)',context);
  return {context,pending,intervals,notices,get page(){return page;},get foreignReads(){return foreignReads;},get staleWrites(){return staleWrites;},leave(){departed=page;page=element();},run:s=>vm.runInContext(s,context)};
}
for (const view of ['Start','Dash','Server','Library','ISOs','Answers','PCs','Menu','Log']) {
  test(`${view}: late requests cannot draw into a replacement page or restart polling`, async () => {
    const f=fixture();
    const work=f.run(`guard(page${view})()`);
    assert.ok(f.pending.length, 'view starts loading');
    f.leave();
    f.page.innerHTML='replacement';
    for(const request of f.pending)request.resolve({ok:true,json:async()=>({})});
    await work;
    assert.equal(f.page.innerHTML,'replacement');
    assert.deepEqual(f.notices,[]);
    assert.equal(f.foreignReads,0);
    assert.equal(f.staleWrites,0);
    assert.equal(f.intervals.length,0);
  });
}
test('same-name revisit, stale failures, active failures and stale writes',async()=>{
 const f=fixture();
 f.run('globalThis.scope=viewScope(document.querySelector("#page"))');
 const old=f.run('guard(async()=>{await scope.request("/read");toast("old result")})()');
 f.leave();
 f.pending[0].reject(new Error('late server failure'));
 await old;
 assert.deepEqual(f.notices,[]);
 await f.run('guard(()=>scope.request("/mutation","POST",{}))()');
 assert.equal(f.pending.length,1,'stale event handler must not send a mutation');
 const active=f.run('guard(async()=>{await viewScope(document.querySelector("#page")).request("/read")})()');
 f.pending[1].reject(new Error('current failure'));
 await active;
 assert.deepEqual(f.notices,['current failure']);
});
test('queued polling callback does no work after navigation',async()=>{
 const f=fixture();
 f.run('viewScope(document.querySelector("#page")).repeat(10,()=>api("/poll"))');
 assert.equal(f.intervals.length,1);
 f.leave(); await f.intervals[0]();
 assert.equal(f.pending.length,0);
});
test('active requests return data; sent mutations finish without stale UI feedback',async()=>{
 const f=fixture();
 const active=f.run('viewScope(document.querySelector("#page")).request("/read")');
 f.pending[0].resolve({ok:true,json:async()=>({value:42})});
 assert.equal((await active).value,42);
 const mutation=f.run('guard(async()=>{await viewScope(document.querySelector("#page")).request("/save","POST",{});toast("saved")})()');
 assert.equal(f.pending[1].args[1].method,'POST');
 f.leave(); f.pending[1].resolve({ok:true,json:async()=>({ok:true})});
 await mutation;
 assert.deepEqual(f.notices,[]);
});
test('the Account page offers the first-start guide again without changing a setting',()=>{
 const f=fixture();
 f.run('pageAccount()');
 assert.match(f.page.innerHTML,/id="guide"/);
 assert.match(f.page.innerHTML,/Show the first-start guide again/);
 f.run('globalThis.drawn=0; render=()=>{drawn++}; state.setupDone=true; openGuide()');
 assert.equal(f.run('state.view'),'start');
 assert.equal(f.run('location.hash'),'start');
 assert.equal(f.run('drawn'),1);
 assert.equal(f.pending.length,0,'opening the guide sends nothing');
});
test('Tab stays inside a dialog: it wraps from the last control to the first and back (#70)', () => {
  const f = fixture();
  const control = name => ({ name, focused: 0, focus() { this.focused++; } });
  const [a, b, c] = ['a', 'b', 'c'].map(control);
  const root = { querySelectorAll: () => [a, b, c] };
  const press = (active, shiftKey) => { const e = { key: 'Tab', shiftKey, prevented: false, preventDefault() { this.prevented = true; } }; f.context.trapTab(e, root, active); return e.prevented; };
  assert.equal(press(c, false), true); assert.equal(a.focused, 1);
  assert.equal(press(a, true), true); assert.equal(c.focused, 1);
  assert.equal(press(null, false), true); assert.equal(a.focused, 2, 'focus outside the dialog comes back in');
  assert.equal(press(b, false), false, 'a Tab inside the dialog is left to the browser');
  assert.equal(press(b, true), false);
  const other = { key: 'a', preventDefault() { assert.fail('only Tab is handled'); } }; f.context.trapTab(other, root, c);
  const none = { key: 'Tab', prevented: false, preventDefault() { this.prevented = true; } }; f.context.trapTab(none, { querySelectorAll: () => [] }, null); assert.equal(none.prevented, true);
});
test('the boot-entries dialog neither offers a hidden entry as the default nor lets the default be hidden (#70)', () => {
  const f = fixture();
  const row = (id, checked) => ({ dataset: { entry: id }, box: { checked, disabled: false }, querySelector(s) { assert.equal(s, '[data-hidden]'); return this.box; } });
  const rows = [row('a', false), row('b', true)];
  const options = [{ value: '', disabled: false }, { value: 'a', disabled: false }, { value: 'b', disabled: false }];
  const select = { value: 'a', options };
  const root = { querySelector: () => select, querySelectorAll: () => rows };
  f.context.syncEntryDefault(root);
  assert.deepEqual(options.map(o => o.disabled), [false, false, true], 'the hidden entry is not offered');
  assert.deepEqual(rows.map(r => r.box.disabled), [true, false], 'the default entry cannot be hidden');
  select.value = ''; rows[0].box.checked = true;
  f.context.syncEntryDefault(root);
  assert.deepEqual(options.map(o => o.disabled), [false, true, true]);
  assert.deepEqual(rows.map(r => r.box.disabled), [false, false]);
});

test('the address and the page agree: navigate() writes it, Back/Forward and typed addresses are followed (#80)', () => {
  const f = fixture();
  f.run('globalThis.drawn=[]; render=()=>{drawn.push(state.view)}; state.setupDone=true; state.view="dash"');
  f.run('navigate("pcs")');
  assert.equal(f.run('location.hash'), 'pcs');
  assert.equal(f.run('state.view'), 'pcs');
  f.run('location.hash="#server"; onAddressChange()');
  assert.deepEqual(JSON.parse(f.run('JSON.stringify(drawn)')), ['pcs', 'server'], 'Back or a typed address shows that page');
  f.run('onAddressChange()');
  assert.equal(f.run('drawn.length'), 2, 'the address of the page already shown draws nothing again');
  f.run('location.hash="#nonsense"; onAddressChange()');
  assert.equal(f.run('state.view'), 'server', 'an address that names no page is ignored');
  assert.equal(f.run('drawn.length'), 2);
});
test('an address is followed while the first-start guide is shown, and #start still opens it (#80)', () => {
  const f = fixture();
  f.run('globalThis.drawn=0; render=()=>{drawn++}; state.setupDone=false; state.view="start"; location.hash="#pcs"; onAddressChange()');
  assert.equal(f.run('state.view'), 'pcs');
  f.run('location.hash="#start"; onAddressChange()');
  assert.equal(f.run('state.view'), 'start');
  assert.equal(f.run('drawn'), 2);
});
test('the page buttons and in-page links go through navigate() (#80)', () => {
  assert.equal(source.match(/location\.hash =/g).length, 1, 'only navigate() writes the address');
  assert.match(source, /addEventListener\("hashchange", onAddressChange\)/);
});
test('the rule drop-down groups the entries by what they need (#81)', () => {
  const f = fixture();
  f.run('state.isos=[{id:"a",title:"Ubuntu",family:"Ubuntu (casper)"},{id:"w",title:"Windows 11",family:"Windows setup"},{id:"d",title:"Debian",family:"Debian"}]');
  const html = f.run('actionOptions("native:w")');
  const groups = [...html.matchAll(/<optgroup label="([^"]*)">([\s\S]*?)<\/optgroup>/g)];
  assert.equal(groups.length, 2);
  assert.match(groups[0][1], /iPXE/); assert.equal((groups[0][2].match(/<option/g) || []).length, 3);
  assert.match(groups[1][1], /Secure Boot/); assert.equal((groups[1][2].match(/<option/g) || []).length, 2);
  assert.match(groups[1][2], /value="native:w" selected/);
  assert.match(html.split('<optgroup')[0], /Show the menu[\s\S]*Boot from its own disk/, 'the menu and own-disk choices stay ungrouped, first');
  f.run('state.isos=[]');
  assert.doesNotMatch(f.run('actionOptions("menu")'), /optgroup/, 'no empty group');
});
test('a PC row keeps Name, History and Remove rule visible and puts the rest under More; the menu count is in History (#81)', () => {
  const row = source.slice(source.indexOf('<td><div class="row" style="gap:6px;min-width:14em">'));
  const more = row.indexOf('<details class="more">');
  assert.ok(more > 0);
  for (const keep of ['data-name=', 'data-history=', 'data-del=']) assert.ok(row.indexOf(keep) < more, keep + ' stays visible');
  for (const hide of ['data-kopt=', 'data-sb=', 'data-sf=', 'data-wake=', 'data-forget=']) assert.ok(row.indexOf(hide) > more, hide + ' is under More');
  assert.doesNotMatch(source, /\$\{p\.count\}× menu/);
  assert.match(source, /It has asked for the menu \$\{p\.count\} time/);
});
test('PCs page: status badge per PC from the data the page already has', () => {
  const f = fixture(), now = Date.parse('2026-11-01T12:00:00Z');
  const badge = p => JSON.parse(f.run(`JSON.stringify(pcStatus(${JSON.stringify(p)}, ${now}))`));
  assert.equal(badge({ shimRefused: true, count: 3, lastSeen: '2026-11-01T11:59:00Z' }).text, 'Refused Secure Boot');
  assert.equal(badge({ shimRefused: true }).cls, 'red');
  assert.equal(badge({ lastSeen: '0001-01-01T00:00:00Z' }).text, 'Not seen yet');
  assert.equal(badge({}).text, 'Not seen yet');
  assert.deepEqual([badge({ count: 0, lastSeen: '2026-11-01T11:59:00Z' }).text, badge({ count: 0, lastSeen: '2026-11-01T11:59:00Z' }).cls], ['Waiting', 'warn']);
  assert.deepEqual([badge({ count: 2, lastSeen: '2026-11-01T11:55:00Z' }).text, badge({ count: 2, lastSeen: '2026-11-01T11:55:00Z' }).cls], ['Seen recently', 'ok']);
  assert.equal(badge({ count: 2, lastSeen: '2026-11-01T11:30:00Z' }).text, 'Not seen lately');
});
test('PCs page: warning on a plain Windows rule for a PC known to use Secure Boot', () => {
  const f = fixture();
  const isos = [{ id: 'w', family: 'Windows setup' }, { id: 'u', family: 'Ubuntu (casper)' }];
  const warn = p => f.run(`pcRuleWarning(${JSON.stringify(p)}, ${JSON.stringify(isos)})`);
  assert.match(warn({ shimRefused: true, rule: { action: 'iso:w' } }), /Secure Boot/);
  assert.match(warn({ options: { secureBoot: true }, rule: { action: 'iso:w' } }), /once \(works with Secure Boot\)/);
  assert.equal(warn({ rule: { action: 'iso:w' } }), '', 'Secure Boot not known: no warning');
  assert.equal(warn({ shimRefused: true, rule: { action: 'native:w' } }), '', 'the Secure Boot start is the right rule');
  assert.equal(warn({ shimRefused: true, rule: { action: 'iso:u' } }), '', 'not a Windows ISO');
  assert.equal(warn({ shimRefused: true }), '', 'no rule');
});
test('PCs page: a rule can name one entry of an ISO\'s own boot menu', () => {
  const f = fixture(), way = 'ab'.repeat(16);
  f.run(`state.isos=[{id:'0123456789',title:'Ubuntu',family:'Ubuntu-style (casper)',ways:[{id:'${way}',title:'Safe <graphics>'}]}]`);
  const options = f.run(`actionOptions('iso:0123456789@${way}')`);
  assert.match(options, new RegExp(`<option value="iso:0123456789@${way}" selected>Ubuntu: Safe &lt;graphics&gt;</option>`));
  assert.equal(f.run(`actionLabel('iso:0123456789@${way}')`), 'Ubuntu: Safe <graphics>');
  assert.equal(f.run(`actionLabel('iso:0123456789')`), 'Ubuntu');
  assert.equal(f.run(`actionLabel('iso:0123456789@${'cd'.repeat(16)}')`), 'Ubuntu (missing entry)');
  f.run('state.isos=[{id:"0123456789",title:"Plain"}]');
  assert.doesNotMatch(f.run('actionOptions("menu")'), /boot menu/, 'an ISO without entries adds no group');
});

const dirEntry = (name, path, count) => ({ name, path, dir: true, count, managed: false, size: 0, mod: '2026-01-01T00:00:00Z', inUse: false });
const isoEntry = (name, path) => ({ name, path, dir: false, count: 0, managed: true, size: 5 * 2 ** 20, mod: '2026-01-01T00:00:00Z', inUse: false, sha256: 'a'.repeat(64) });
const call = (f, expr, data) => { f.context.__d = data; return f.run(expr); };

test('a library folder is part of the address: navigate() writes it, Back, Forward and typed addresses are followed (library folders)', () => {
  const f = fixture();
  f.run('globalThis.drawn=[]; render=()=>{drawn.push(state.view+":"+state.libPath)}; state.setupDone=true; state.view="dash"');
  f.run('navigate("library", "linux/deb")');
  assert.equal(f.run('location.hash'), 'library/linux/deb');
  assert.equal(f.run('state.libPath'), 'linux/deb');
  f.run('navigate("library", "R&D #1/new?")');
  assert.equal(f.run('location.hash'), 'library/R%26D%20%231/new%3F', 'a name keeps to one part of the address');
  f.run('location.hash="#library/R%26D%20%231/new%3F"; onAddressChange()');
  assert.equal(f.run('drawn.length'), 2, 'the address of the folder already shown draws nothing again');
  f.run('location.hash="#library/a%20b/c"; onAddressChange()');
  assert.equal(f.run('state.libPath'), 'a b/c', 'Back or a typed address shows that folder');
  f.run('location.hash="#library"; onAddressChange()');
  assert.equal(f.run('state.libPath'), '', 'the address of the library alone is its top');
  f.run('location.hash="#library/%E0%A4%A"; onAddressChange()');
  assert.equal(f.run('state.libPath'), '', 'an address that cannot be read is the top, not an error');
  f.run('navigate("pcs"); navigate("library")');
  assert.equal(f.run('location.hash'), 'library', 'the Library button opens the top');
  assert.equal(f.run('state.libPath'), '');
  f.run('location.hash="#library"');
  assert.equal(f.run('pageInAddress()'), 'library');
  f.run('location.hash="#library/x"');
  assert.equal(f.run('pageInAddress()'), 'library', 'a page followed by a path is still that page');
  assert.equal(f.run('folderInAddress()'), 'x');
  f.run('location.hash="#pcs/x"');
  assert.equal(f.run('folderInAddress()'), '', 'only the Library page has a folder');
});
test('the breadcrumb is Library / folder / subfolder: every part but the last is a link, names are escaped', () => {
  const f = fixture();
  const top = call(f, 'crumbsHTML(__d)', '');
  assert.match(top, /<span aria-current="page">Library<\/span>/);
  assert.doesNotMatch(top, /<a /, 'the top has nothing to go up to');
  const deep = call(f, 'crumbsHTML(__d)', 'linux/R&D <x>/new #2');
  assert.match(deep, /<a href="#library" data-open="">Library<\/a>/);
  assert.match(deep, /<a href="#library\/linux" data-open="linux">linux<\/a>/);
  assert.match(deep, /<a href="#library\/linux\/R%26D%20%3Cx%3E" data-open="linux\/R&amp;D &lt;x&gt;">R&amp;D &lt;x&gt;<\/a>/);
  assert.match(deep, /<span aria-current="page">new #2<\/span>/, 'the folder shown is text, not a link');
  assert.doesNotMatch(deep, /<x>/, 'nothing of a name reaches the page unescaped');
  assert.match(deep, /<nav aria-label="[^"]+"><ol class="crumbs">/);
});
test('the library list shows the folder\'s folders first, with a count and an icon, then its ISOs only', () => {
  const f = fixture();
  const d = { path: 'linux', parent: '', hasParent: true, entries: [dirEntry('deb', 'linux/deb', 2), dirEntry('<b>new</b>', 'linux/<b>new</b>', 0), dirEntry('one', 'linux/one', 1), isoEntry('ubuntu.iso', 'linux/ubuntu.iso')] };
  const html = call(f, 'libraryTableHTML(__d, null)', d);
  assert.match(html, /<svg class="ficon"[^>]*aria-hidden="true"/);
  assert.match(html, /data-open="linux\/deb">deb<\/a> <span class="badge">2 ISOs or images<\/span>/);
  assert.match(html, /<span class="badge">1 ISO or image<\/span>/);
  assert.match(html, /<span class="badge warn">empty<\/span>/);
  assert.ok(html.indexOf('deb</a>') < html.indexOf('ubuntu.iso'), 'folders come first');
  assert.doesNotMatch(html, /<b>new/, 'a folder name is escaped');
  assert.match(html, /data-open="linux\/&lt;b&gt;new&lt;\/b&gt;"/);
  assert.equal((html.match(/data-del=/g) || []).length, 2, 'a folder with ISOs in it is not offered for deletion');
  assert.match(html, /data-mv="linux\/ubuntu\.iso"/);
  assert.match(html, /data-ren="linux\/deb"/);
  assert.doesNotMatch(html, /href=[^>]*ubuntu/, 'an ISO is not a link');
});
test('an empty folder says so, the top of an empty library says how to fill it', () => {
  const f = fixture();
  assert.match(call(f, 'libraryTableHTML(__d, null)', { path: 'linux', entries: [] }), /This folder is empty/);
  assert.match(call(f, 'libraryTableHTML(__d, null)', { path: '', entries: [] }), /The library is empty/);
});
test('Up goes to the parent folder and is absent at the top', () => {
  const f = fixture();
  assert.equal(call(f, 'upHTML(__d)', { hasParent: false }), '');
  assert.match(call(f, 'upHTML(__d)', { hasParent: true, parent: 'a/b' }), /<a class="btn small" href="#library\/a\/b" data-open="a\/b">/);
  assert.match(call(f, 'upHTML(__d)', { hasParent: true, parent: '' }), /href="#library" data-open=""/);
});
test('a search hit shows the folder it is in, and a folder hit opens', () => {
  const f = fixture();
  const ui = { query: 'ubu <', results: [isoEntry('ubuntu.iso', 'linux/ubuntu.iso'), isoEntry('top.iso', 'top.iso'), dirEntry('ubuntu-old', 'linux/ubuntu-old', 1)], more: true };
  const html = call(f, 'searchHTML(__d)', ui);
  assert.match(html, /<a href="#library\/linux" data-open="linux">\/linux<\/a>/);
  assert.match(html, /<a href="#library" data-open="">\/<\/a>/, 'a file at the top is in the top');
  assert.match(html, /data-open="linux\/ubuntu-old">ubuntu-old<\/a>/);
  assert.match(html, /Only the first 3 are shown/);
  assert.match(call(f, 'searchHTML(__d)', { query: ' x<y ', results: [], more: false }), /Nothing in the library has x&lt;y in its name/);
  assert.match(call(f, 'searchHTML(__d)', { query: 'x', results: null }), /Searching/);
});
test('the folders an item can be moved to leave out where it is, and for a folder itself and what is inside it', () => {
  const f = fixture();
  const folders = ['a', 'a/b', 'a/b/c', 'ab', 'z'];
  const targets = (path, dir) => JSON.parse(f.run(`JSON.stringify(moveTargets(${JSON.stringify(folders)}, ${JSON.stringify(path)}, ${dir}))`));
  assert.deepEqual(targets('x.iso', false), folders, 'a file at the top can go to every folder');
  assert.deepEqual(targets('a/x.iso', false), ['', 'a/b', 'a/b/c', 'ab', 'z'], 'not where it already is');
  assert.deepEqual(targets('a/b', true), ['', 'ab', 'z'], 'a folder cannot go into itself or below; where it is now (a) is left out');
  assert.deepEqual(targets('ab', true), ['a', 'a/b', 'a/b/c', 'z'], 'a folder whose name starts like another is not "inside" it');
});
test('an upload goes to the folder that was shown when it was chosen, not where the page is when its turn comes', async () => {
  const f = fixture();
  f.run('globalThis.sent=[]; uploadFile = async (file, folder) => { sent.push(folder); }; state.libPath="linux"; queueUpload({name:"a.iso"}); state.libPath="other"; queueUpload({name:"b.iso"}); state.libPath="elsewhere"');
  await f.run('transfers.queue');
  assert.deepEqual(JSON.parse(f.run('JSON.stringify(sent)')), ['linux', 'other']);
});
test('the ISOs page links the folder of each ISO to the Library page', () => {
  const f = fixture();
  assert.equal(f.run('isoPathHTML("a.iso")'), 'a.iso');
  assert.equal(f.run('isoPathHTML("linux/deb/<a>.iso")'), '<a href="#library/linux/deb" data-open="linux/deb">linux/deb/</a>&lt;a&gt;.iso');
});
