// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
// Run with node --test tools/admin-layout.test.cjs. Checks the page's stylesheet for rules that keep the layout usable (found in a real-browser audit, issue #78);
// it does not replace looking at the page in a browser.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const html = fs.readFileSync(process.env.BOOTLACE_ADMIN_HTML || 'internal/admin/web/index.html', 'utf8').replace(/\r\n/g, '\n');
const css = html.match(/<style>([\s\S]*?)<\/style>/)[1];

test('the rule drop-down of a PC row keeps a readable width', () => {
  const m = css.match(/table\.rt select\.inline\[data-mac\]\{min-width:(\d+)em\}/);
  assert.ok(m && Number(m[1]) >= 14, 'the select would be squeezed to a few letters by the row of buttons');
});

test('the PC action buttons wrap instead of squeezing the other columns', () => {
  assert.ok(/<div class="row" style="gap:6px;min-width:14em"><button class="btn small" data-name=/.test(html));
});

test('long addresses and paths wrap on a phone instead of widening the page', () => {
  assert.ok(/td\.mono\{overflow-wrap:anywhere\}/.test(css));
  const phone = css.slice(css.indexOf('@media (max-width:640px)'));
  assert.ok(/table\.rt td\{overflow-wrap:anywhere\}/.test(phone));
  assert.ok(/table\.rt td\.k,table\.rt td\.nowrap\{white-space:normal\}/.test(phone));
});
