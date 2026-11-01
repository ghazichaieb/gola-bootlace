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
