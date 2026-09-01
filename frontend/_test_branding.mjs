// Standalone test for resolveSlot (pure logic, no Vue runtime needed).
// Run: node _test_branding.mjs  (from frontend/)
import { resolveSlot } from './src/composables/brandingResolve.js';

let pass = 0, fail = 0;
function check(name, cond) {
  if (cond) { pass++; console.log('ok   -', name); }
  else { fail++; console.log('FAIL -', name); }
}

const slot = 'home_banner';
const el = (patterns, extra = {}) => ({ slot, image_url: '/x.png', page_patterns: patterns, ...extra });
const set = (priority, enabled, elements) => ({ id: priority, priority, enabled, elements });

// 1. No data -> null
check('empty -> null', resolveSlot([], [], [], slot, '/') === null);

// 2. Enabled set, default element (no patterns) -> shown everywhere
check('default shown on /',
  resolveSlot([set(1, true, [el([])])], [], [], slot, '/')?.image_url === '/x.png');
check('default shown on /shop/a/b',
  resolveSlot([set(1, true, [el([])])], [], [], slot, '/shop/a/b')?.image_url === '/x.png');

// 3. Disabled set -> ignored
check('disabled ignored',
  resolveSlot([set(1, false, [el([])])], [], [], slot, '/') === null);

// 4. Exact pattern match
const exact = set(1, true, [el(['^/shop/telefony'])]);
check('pattern match',
  resolveSlot([exact], [], [], slot, '/shop/telefony/samsung')?.image_url === '/x.png');
check('pattern no match',
  resolveSlot([exact], [], [], slot, '/') === null);

// 5. Multiple patterns = OR
const orSet = set(1, true, [el(['^/$', '^/cart$'])]);
check('OR pattern 1', resolveSlot([orSet], [], [], slot, '/')?.image_url === '/x.png');
check('OR pattern 2', resolveSlot([orSet], [], [], slot, '/cart')?.image_url === '/x.png');
check('OR no match', resolveSlot([orSet], [], [], slot, '/products/1') === null);

// 6. Priority: two exact matches -> higher wins
const low = set(1, true, [el(['^/'], { image_url: '/low.png' })]);
const high = set(10, true, [el(['^/'], { image_url: '/high.png' })]);
check('higher priority wins',
  resolveSlot([low, high], [], [], slot, '/')?.image_url === '/high.png');

// 7. Specificity beats priority: low-priority exact match beats high-priority default
const lowExact = set(1, true, [el(['^/shop/'], { image_url: '/lowExact.png' })]);
const highDefault = set(10, true, [el([], { image_url: '/highDefault.png' })]);
check('exact beats default (specificity)',
  resolveSlot([highDefault, lowExact], [], [], slot, '/shop/x')?.image_url === '/lowExact.png');
check('default used when no exact',
  resolveSlot([highDefault, lowExact], [], [], slot, '/')?.image_url === '/highDefault.png');

// 8. Category override wins over everything (deepest first)
const override = (catId, url) => ({ category_id: catId, slot, image_url: url });
check('override wins over set',
  resolveSlot([set(1, true, [el([])])], [override(5, '/ovr.png')], [5], slot, '/shop/a')?.image_url === '/ovr.png');
check('override deepest first',
  resolveSlot([], [override(1, '/root.png'), override(5, '/deep.png')], [1, 5], slot, '/shop/a')?.image_url === '/deep.png');
check('override ancestor applies',
  resolveSlot([], [override(1, '/root.png')], [1, 5], slot, '/shop/a')?.image_url === '/root.png');
check('override not in chain -> set used',
  resolveSlot([set(1, true, [el([]), ])], [override(99, '/ovr.png')], [5], slot, '/shop/a')?.image_url === '/x.png');
check('no chain -> no override',
  resolveSlot([], [override(5, '/ovr.png')], [], slot, '/') === null);

// 9. Invalid pattern -> never matches (but element with empty list still default)
const invalid = set(1, true, [el(['(unclosed'])]);
check('invalid pattern no match',
  resolveSlot([invalid], [], [], slot, '/') === null);

// 10. Element for a different slot does not leak
const otherSlot = { slot: 'footer_fullwidth', image_url: '/f.png', page_patterns: [] };
check('other slot ignored',
  resolveSlot([set(1, true, [otherSlot])], [], [], slot, '/') === null);

console.log(`\n=== RESULT: ${pass} passed, ${fail} failed ===`);
process.exit(fail === 0 ? 0 : 1);
