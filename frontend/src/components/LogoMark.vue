<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';

/**
 * Brand monogram (MK) — the primary logo mark of the site.
 *
 * Two interlocking sharp strokes:
 *  - .mark-base    → currentColor (parent sets it via a text-* class)
 *  - .mark-accent  → var(--accent), adapts to light/dark automatically
 *
 * The second stroke overlaps the first (x 648–778), so the accent stroke
 * reads as the converging line on top of the base — the "aggregation" idea.
 *
 * Entrance: on first render the mark draws itself in with the same
 * hand-drawn strokes as the hover effect (.logo-draw). The class is dropped
 * shortly after the longest instance finishes, so the hover animation can
 * replay freely afterwards without the entrance re-triggering.
 *
 * Sizing: the parent sets the height (e.g. h-8); the width follows the
 * intrinsic 632:352 ratio (see the .logo-mark-svg rule in style.css).
 * The mark is decorative — the wordmark text carries the brand name.
 */

// Present from the very first render (no flash of the final state),
// dropped after the animation completes: 1s duration + 0.3s stagger + buffer.
const drawing = ref(true);
let timer = null;

onMounted(() => {
  timer = setTimeout(() => {
    drawing.value = false;
  }, 1500);
});

onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
});
</script>

<template>
  <svg
    :class="{ 'logo-mark-svg': true, 'logo-draw': drawing }"
    xmlns="http://www.w3.org/2000/svg"
    viewBox="388 208 632 352"
    aria-hidden="true"
    focusable="false"
  >
    <g transform="translate(0,768) scale(0.1,-0.1)" stroke="none">
      <path class="mark-base" pathLength="1" d="M4000 5476 c0 -2 37 -73 83 -157 125 -230 669 -1245 852 -1589 164 -308 611 -1142 663 -1237 30 -57 58 -103 61 -103 3 0 109 188 235 418 358 650 921 1677 1036 1889 58 106 108 193 111 193 3 0 129 -234 280 -520 151 -286 276 -520 279 -520 3 0 37 50 75 112 39 61 76 121 83 132 15 23 26 0 -322 659 -299 566 -384 722 -391 722 -6 0 -216 -377 -470 -845 -40 -74 -218 -400 -395 -725 -178 -324 -354 -648 -393 -720 -111 -208 -118 -219 -132 -203 -7 7 -105 186 -218 398 -338 636 -579 1086 -773 1445 -100 187 -186 348 -190 358 -6 16 14 17 294 17 l300 0 74 -127 c41 -71 173 -294 293 -498 120 -203 270 -458 332 -565 63 -107 122 -205 131 -217 17 -23 18 -21 90 110 39 73 72 138 72 145 0 15 -78 149 -733 1259 l-102 173 -612 0 c-337 0 -613 -2 -613 -4z" />
      <path class="mark-accent" pathLength="1" d="M8774 5348 c-887 -1366 -1720 -2633 -1732 -2633 -6 0 -62 79 -125 175 l-113 175 -162 3 c-89 1 -162 0 -162 -4 0 -9 554 -864 560 -864 3 0 81 116 173 258 93 141 295 449 450 684 278 422 757 1153 1145 1748 l199 305 296 3 c163 1 297 0 297 -3 0 -2 -13 -26 -29 -52 -16 -27 -156 -273 -311 -548 -155 -275 -310 -549 -345 -610 -55 -96 -603 -1066 -633 -1120 -9 -17 -28 15 -140 245 l-129 265 -88 -133 -88 -132 117 -243 c214 -439 291 -591 301 -590 10 0 313 529 907 1578 168 297 438 774 600 1060 162 286 300 530 307 543 l12 22 -611 0 -610 0 -86 -132z" />
    </g>
  </svg>
</template>
