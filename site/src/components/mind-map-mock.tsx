import { useId } from "react";
import { Minus, Plus, Maximize } from "lucide-react";

/*
 * A drawing of the app's Mind map tab (web/src/components/insights/
 * MindMapView.tsx), not a screenshot: the real tree is laid out left to
 * right (meeting → topics → decisions and actions), with the same node
 * widths, column gap and row height, and the same branch colours. The
 * sample is the Q3 Roadmap Sync meeting from the product screenshots.
 * Decorative: the surrounding copy says the same in words.
 */

const ROOT_W = 200;
const TOPIC_W = 210;
const LEAF_W = 250;
const GAP = 64;
const ROW = 92;
const TOPIC_X = ROOT_W + GAP;
const LEAF_X = TOPIC_X + TOPIC_W + GAP;

type Leaf =
  | { kind: "decision"; lines: string[] }
  | { kind: "action"; lines: string[]; owner: string; due: string };

const topics: { label: string; leaves: Leaf[] }[] = [
  {
    label: "Pricing tiers",
    leaves: [
      { kind: "decision", lines: ["Ship the new pricing tiers in Q3;", "delay dark mode to Q4."] },
      { kind: "action", lines: ["Draft updated pricing one-pager"], owner: "Alice Nguyen", due: "due Aug 1, 2026" },
      { kind: "action", lines: ["Schedule pricing rollout comms"], owner: "Chi Le", due: "due Oct 15, 2026" },
    ],
  },
  {
    label: "Onboarding redesign",
    leaves: [
      { kind: "decision", lines: ["Two-week discovery spike", "before scoping."] },
      { kind: "action", lines: ["Circulate onboarding discovery", "brief"], owner: "Bao Tran", due: "due Sep 10, 2026" },
    ],
  },
];

// Lay out like d3's tree(): leaves ROW apart, a 1.3 gap between branches,
// parents centred over their children.
let slot = 0;
const laid = topics.map((t, ti) => {
  if (ti > 0) slot += 0.3;
  const leaves = t.leaves.map((leaf) => ({ leaf, y: slot++ * ROW }));
  const first = leaves[0].y;
  const last = leaves[leaves.length - 1].y;
  return { ...t, leaves, y: (first + last) / 2, branch: (ti % 6) + 1 };
});
const rootY = (laid[0].y + laid[laid.length - 1].y) / 2;
const PAD = 40;
const minY = Math.min(0, ...laid.map((t) => t.y)) - 44;
const maxY = Math.max(...laid.flatMap((t) => t.leaves.map((l) => l.y))) + 44;
const VB_W = LEAF_X + LEAF_W + PAD;
const VB_H = maxY - minY + PAD;
const oy = (y: number) => y - minY + PAD / 2;
const color = (b: number) => `var(--branch-${b})`;

function edge(x1: number, y1: number, x2: number, y2: number) {
  const mx = x1 + (x2 - x1) / 2;
  return `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`;
}

function leafHeight(l: Leaf) {
  return l.kind === "action" ? 40 + l.lines.length * 17 : 22 + l.lines.length * 17;
}

export function MindMapMock({ className }: { className?: string }) {
  const dots = `mm-dots-${useId().replace(/:/g, "")}`;
  return (
    <svg
      viewBox={`0 0 ${VB_W} ${VB_H}`}
      aria-hidden="true"
      focusable="false"
      className={className}
      style={{ fontFamily: "var(--font-sans)" }}
    >
      <defs>
        <pattern id={dots} width="20" height="20" patternUnits="userSpaceOnUse">
          <circle cx="1" cy="1" r="1" style={{ fill: "var(--line)" }} />
        </pattern>
      </defs>
      <rect width={VB_W} height={VB_H} style={{ fill: "var(--bg)" }} />
      <rect width={VB_W} height={VB_H} fill={`url(#${dots})`} />

      {/* edges */}
      {laid.map((t) => (
        <g key={`e-${t.label}`} fill="none" strokeWidth="2" style={{ stroke: color(t.branch) }}>
          <path d={edge(ROOT_W, oy(rootY), TOPIC_X, oy(t.y))} />
          {t.leaves.map(({ y }, i) => (
            <path key={i} d={edge(TOPIC_X + TOPIC_W, oy(t.y), LEAF_X, oy(y))} />
          ))}
        </g>
      ))}

      {/* root */}
      <g transform={`translate(0 ${oy(rootY) - 26})`}>
        <rect width={ROOT_W} height="52" rx="12" style={{ fill: "var(--primary)" }} />
        <text x="16" y="31" fontSize="14" fontWeight="600" style={{ fill: "var(--primary-fg)" }}>
          Q3 Roadmap Sync
        </text>
      </g>

      {laid.map((t) => (
        <g key={t.label}>
          {/* topic */}
          <g transform={`translate(${TOPIC_X} ${oy(t.y) - 29})`}>
            <rect width={TOPIC_W} height="58" rx="10" style={{ fill: "var(--card)", stroke: "var(--line-strong)" }} />
            <rect x="0.5" y="8" width="4" height="42" rx="2" style={{ fill: color(t.branch) }} />
            <path d="m16 23 5 5-5 5" fill="none" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" transform="rotate(90 19 28)" style={{ stroke: "var(--muted)" }} />
            <text x="36" y="26" fontSize="14" fontWeight="500" style={{ fill: "var(--ink)" }}>
              {t.label}
            </text>
            <text x="36" y="44" fontSize="12" style={{ fill: "var(--muted)" }}>
              {t.leaves.length} items
            </text>
          </g>

          {/* decisions and actions */}
          {t.leaves.map(({ leaf, y }, i) => {
            const h = leafHeight(leaf);
            return (
              <g key={i} transform={`translate(${LEAF_X} ${oy(y) - h / 2})`}>
                <rect width={LEAF_W} height={h} rx="10" style={{ fill: "var(--card)", stroke: "var(--line-strong)" }} />
                <svg x="12" y="13" width="16" height="16" viewBox="0 0 24 24" fill="none" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ stroke: color(t.branch) }}>
                  {leaf.kind === "decision" ? (
                    <>
                      <line x1="6" x2="6" y1="3" y2="15" />
                      <circle cx="18" cy="6" r="3" />
                      <circle cx="6" cy="18" r="3" />
                      <path d="M18 9a9 9 0 0 1-9 9" />
                    </>
                  ) : (
                    <>
                      <path d="M21 10.5V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h12.5" />
                      <path d="m9 11 3 3L22 4" />
                    </>
                  )}
                </svg>
                {leaf.lines.map((line, li) => (
                  <text key={li} x="38" y={25 + li * 17} fontSize="13.5" style={{ fill: "var(--ink)" }}>
                    {line}
                  </text>
                ))}
                {leaf.kind === "action" && (
                  <g transform={`translate(38 ${leaf.lines.length * 17 + 14})`}>
                    <rect width={leaf.owner.length * 6.6 + 18} height="20" rx="10" fill="none" style={{ stroke: "var(--line-strong)" }} />
                    <text x="9" y="14" fontSize="11.5" fontWeight="500" style={{ fill: "var(--ink)" }}>
                      {leaf.owner}
                    </text>
                    <text x={leaf.owner.length * 6.6 + 28} y="14" fontSize="11.5" style={{ fill: "var(--muted)" }}>
                      {leaf.due}
                    </text>
                  </g>
                )}
              </g>
            );
          })}
        </g>
      ))}
    </svg>
  );
}

/** The Mind map tab: tab bar, the map, and React Flow's zoom buttons. */
export function MindMapWindow() {
  const tabs = ["Overview", "Mind map", "Flows", "Transcript"];
  return (
    <div aria-hidden="true">
      <div className="inline-flex h-9 items-center rounded-lg bg-tan p-[3px] text-sm font-medium text-muted">
        {tabs.map((t) => (
          <span
            key={t}
            className={
              t === "Mind map"
                ? "rounded-md bg-bg px-3 py-1 text-ink shadow-sm"
                : "px-3 py-1"
            }
          >
            {t}
          </span>
        ))}
      </div>
      <div className="relative mt-4 overflow-hidden rounded-xl border border-line-strong/70">
        <MindMapMock className="block h-auto w-full" />
        <div className="absolute bottom-3 left-3 flex flex-col overflow-hidden rounded-md border border-line-strong/70 bg-card text-ink">
          {[Plus, Minus, Maximize].map((Icon, i) => (
            <span key={i} className="grid size-6 place-items-center border-b border-line last:border-b-0 sm:size-7">
              <Icon className="size-3 sm:size-3.5" />
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}
