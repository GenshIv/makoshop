// Pure branding resolution logic (no Vue/store dependencies).
// Kept separate from useBranding.js so it can be unit-tested in plain Node.

// Cache compiled regexes per pattern string. Invalid patterns compile to
// null and never match (they are reported in the admin UI on save).
const patternCache = new Map();

function getRegex(pattern) {
  if (patternCache.has(pattern)) return patternCache.get(pattern);
  let re = null;
  try {
    re = new RegExp(pattern);
  } catch {
    re = null;
  }
  patternCache.set(pattern, re);
  return re;
}

// An element applies to a path when any of its patterns matches.
// An empty pattern list means "every page".
export function elementAppliesToPath(element, path) {
  const patterns = element?.page_patterns;
  if (!patterns || patterns.length === 0) return true;
  return patterns.some((p) => {
    const re = getRegex(p);
    return re !== null && re.test(path);
  });
}

// Resolve the element for a slot:
//  1. Category override (deepest category in the chain first).
//  2. Exact regex match — highest-priority enabled set wins.
//  3. Default element (no patterns) — highest priority wins.
//  4. null (slot not rendered).
export function resolveSlot(sets, categoryOverrides, categoryChain, slot, path) {
  // 1. Category override — deepest category in the chain first.
  if (categoryOverrides && categoryChain && categoryChain.length > 0) {
    for (let i = categoryChain.length - 1; i >= 0; i--) {
      const catId = categoryChain[i];
      const ovr = categoryOverrides.find(
        (o) => o.category_id === catId && o.slot === slot
      );
      if (ovr) return ovr;
    }
  }

  const enabled = (sets || []).filter((s) => s.enabled);
  if (enabled.length === 0) return null;

  // 2. Exact regex match — highest priority first.
  const byPriority = [...enabled].sort((a, b) => (b.priority || 0) - (a.priority || 0));
  for (const s of byPriority) {
    const el = (s.elements || []).find(
      (e) => e.slot === slot && e.page_patterns?.length > 0
    );
    if (el && elementAppliesToPath(el, path)) return el;
  }

  // 3. Default element (no patterns) — highest priority first.
  for (const s of byPriority) {
    const el = (s.elements || []).find(
      (e) => e.slot === slot && (!e.page_patterns || e.page_patterns.length === 0)
    );
    if (el) return el;
  }

  return null;
}
