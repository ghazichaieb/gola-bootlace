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
  assert.ok(/table\.rt td\.k,table\.rt td\.nowrap\{white-space:normal;overflow-wrap:anywhere\}/.test(phone));
});

/** The colour tokens of a block of the stylesheet that starts at `marker`, as {name: "#rrggbb"}. */
function tokens(marker) {
  const block = css.slice(css.indexOf(marker)).match(/\{([^}]*)\}/)[1];
  return Object.fromEntries([...block.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})/gi)].map(m => [m[1], m[2]]));
}
/** WCAG relative luminance of "#rrggbb". */
function luminance(hex) {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
/** WCAG contrast ratio of two "#rrggbb" colours. */
function contrast(a, b) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

for (const [theme, marker] of [['light', ':root{'], ['dark', ':root[data-theme="dark"]{']]) {
  test(`${theme}: red text reads on the page's panels (AA, 4.5:1) and form controls have a 3:1 outline`, () => {
    const t = Object.assign(tokens(':root{'), tokens(marker));
    for (const bg of ['panel', 'panel2', 'bg', 'accent-soft']) assert.ok(contrast(t['accent-fg'], t[bg]) >= 4.5, `accent-fg on ${bg}: ${contrast(t['accent-fg'], t[bg]).toFixed(2)}`);
    for (const bg of ['panel', 'bg']) assert.ok(contrast(t.field, t[bg]) >= 3, `field outline on ${bg}: ${contrast(t.field, t[bg]).toFixed(2)}`);
  });
}

test('an address in the PCs table is never broken in the middle', () => {
  assert.ok(/<td class="hide-s mono small nowrap" data-l="Address">/.test(html));
  assert.ok(/table\.rt td\.nowrap\{white-space:nowrap;overflow-wrap:normal\}/.test(css));
});
