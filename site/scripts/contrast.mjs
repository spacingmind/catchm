// WCAG contrast ratios for the dark palette in src/app/globals.css.
// Usage: node scripts/contrast.mjs
const lum = (h) => {
  const c = [1, 3, 5]
    .map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

const dark = { page: "#141312", band: "#1c1a18", card: "#262321", panel: "#2c2926" };
const text = { ink: "#f4eedf", muted: "#d3cabb", subtle: "#aba295", accent: "#d9906a", "chip-fg": "#e3a582" };
const min = { ink: 7, muted: 7, subtle: 4.5, accent: 4.5, "chip-fg": 4.5 };

let failed = false;
for (const [name, fg] of Object.entries(text)) {
  const row = Object.entries(dark).map(([s, bg]) => {
    const r = ratio(fg, bg);
    if (r < min[name]) failed = true;
    return `${s} ${r.toFixed(1)}`;
  });
  console.log(name.padEnd(8), fg, row.join("  "), `(min ${min[name]})`);
}
const pairs = [
  ["cream on primary", "#fbf7ea", "#a64f37"],
  ["primary vs page", "#a64f37", "#141312"],
  ["cream on deep band", "#fbf7ea", "#43221a"],
  ["deep-muted on deep band", "#e4d3c6", "#43221a"],
  ["deep-muted on deep card", "#e4d3c6", "#2e150e"],
  ["chip-fg on chip (accent 16% over card)", "#e3a582", "#3a2d27"],
];
for (const [n, a, b] of pairs) console.log(n.padEnd(40), ratio(a, b).toFixed(1));
const lines = { "line vs card": ["#4b4540", "#262321"], "line-strong vs card": ["#6e665e", "#262321"] };
for (const [n, [a, b]] of Object.entries(lines)) console.log(n.padEnd(40), ratio(a, b).toFixed(2), "(decorative)");
for (const [n, c] of Object.entries({ b1: "#d9906a", b2: "#a3b899", b3: "#94aec6", b4: "#c49bb0", b5: "#d6b673", b6: "#b9ab9b" })) {
  const r = ratio(c, dark.card);
  if (r < 3) failed = true;
  console.log(`branch ${n}`.padEnd(40), r.toFixed(1), "on card (non-text min 3)");
}
process.exit(failed ? 1 : 0);
