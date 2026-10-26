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