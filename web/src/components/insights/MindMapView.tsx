import "./flow.css"

import { useMemo, useState, type KeyboardEvent } from "react"
import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react"
import { hierarchy, tree } from "d3-hierarchy"
import { CheckSquare, ChevronRight, Circle, GitBranch } from "lucide-react"

import { Disclosure } from "@/components/Disclosure"
import { SourceLink } from "@/components/insights/Evidence"
import { dueLabel, narrowScreen, ownerName, resolveRef, type JumpToEvidence } from "@/components/insights/evidenceData"
import { Badge } from "@/components/ui/badge"
import type { Analysis, MindNode } from "@/lib/api"
import { cn } from "cn"

/*
 * The mind map (docs/plans/active/meeting-insights.md, decision 3): the
 * server derives the tree (meeting → topics → decisions and actions);
 * this lays it out left to right with d3-hierarchy's tree() and draws it
 * with React Flow. Clicking a topic folds it; clicking a decision or
 * action opens its source line in the transcript.
 */

const ROOT_W = 200
const TOPIC_W = 210
const LEAF_W = 250
const COL_GAP = 64
/** Vertical distance between neighbouring leaves; fits two lines of text plus the owner/due row. */
const ROW = 92
const BRANCH_COLORS = 6

/** Column x for a depth: each column is as wide as its nodes, plus a gap. */
function columnX(depth: number): number {
  const widths = [ROOT_W, TOPIC_W]
  let x = 0
  for (let d = 0; d < depth; d++) x += (widths[d] ?? LEAF_W) + COL_GAP
  return x
}

interface IdNode {
  id: string
  node: MindNode
  children: IdNode[]
}

/** Path ids ("0", "0.2", "0.2.1"): unique and stable across folds. */
function withIds(node: MindNode, id = "0"): IdNode {
  return { id, node, children: (node.children ?? []).map((c, i) => withIds(c, `${id}.${i}`)) }
}

type MindKind = "root" | "topic" | "decision" | "action" | "item"

/** What describe() works out for a tree node. */
type MindFields = {
  kind: MindKind
  label: string
  count: number
  owner?: string
  due?: string
  /** Seqs to jump to; empty when the node doesn't open the transcript. */
  jump: number[]
}

type MindData = MindFields & {
  /** 1..BRANCH_COLORS; 0 on the root. */
  branch: number
  collapsed: boolean
}

type MindFlowNode = Node<MindData, "mind">

const KIND_LABEL: Record<MindKind, string> = {
  root: "Meeting",
  topic: "Topic",
  decision: "Decision",
  action: "Action item",
  item: "Item",
}

function itemsText(n: number): string {
  return n === 1 ? "1 item" : `${n} items`
}

function LeafIcon({ kind, color }: { kind: MindKind; color: string }) {
  const Icon = kind === "decision" ? GitBranch : kind === "action" ? CheckSquare : Circle
  return <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" style={{ color }} />
}

function MindNodeView({ data }: NodeProps<MindFlowNode>) {
  const color = data.branch ? `var(--branch-${data.branch})` : "var(--primary)"
  const hasChildren = data.count > 0 && !data.collapsed
  let body
  if (data.kind === "root") {
    body = (
      <div
        style={{ width: ROOT_W }}
        className="line-clamp-3 rounded-xl bg-primary px-4 py-3 text-sm font-semibold text-primary-foreground shadow-sm"
      >
        {data.label}
      </div>
    )
  } else if (data.kind === "topic") {
    body = (
      <div
        style={{ width: TOPIC_W, borderLeftColor: color }}
        className="flex cursor-pointer items-center gap-2 rounded-lg border border-l-4 border-border bg-card px-3 py-2 text-card-foreground shadow-sm hover:bg-muted"
      >
        <ChevronRight
          aria-hidden="true"
          className={cn("size-4 shrink-0 text-muted-foreground motion-safe:transition-transform", !data.collapsed && "rotate-90")}
        />
        <div className="min-w-0">
          <p className="line-clamp-2 text-sm font-medium">{data.label}</p>
          <p className="text-xs text-muted-foreground">{itemsText(data.count)}</p>
        </div>
      </div>
    )
  } else {
    const jumpable = data.jump.length > 0
    body = (
      <div
        style={{ width: LEAF_W }}
        title={data.label}
        className={cn(
          "flex items-start gap-2 rounded-lg border border-border bg-card px-3 py-2 text-card-foreground",
          jumpable && "cursor-pointer hover:border-ring hover:bg-muted",
        )}
      >
        <LeafIcon kind={data.kind} color={color} />
        <div className="grid min-w-0 gap-1">
          <p className="line-clamp-2 text-sm">{data.label}</p>
          {(data.owner || data.due) && (
            <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
              {data.owner && (
                <Badge variant="outline" className="max-w-32">
                  <span className="min-w-0 truncate">{data.owner}</span>
                </Badge>
              )}
              {data.due && <span className="truncate">{data.due}</span>}
            </div>
          )}
        </div>
      </div>
    )
  }
  return (
    <>
      {data.kind !== "root" && <Handle type="target" position={Position.Left} isConnectable={false} />}
      {body}
      {hasChildren && <Handle type="source" position={Position.Right} isConnectable={false} />}
    </>
  )
}

const nodeTypes = { mind: MindNodeView }

/** Screen-reader text for a node (React Flow puts it on the focusable wrapper). */
function ariaLabelFor(d: MindData): string {
  switch (d.kind) {
    case "root":
      return `Meeting: ${d.label}`
    case "topic":
      return `Topic: ${d.label}, ${itemsText(d.count)}`
    default: {
      const parts = [`${KIND_LABEL[d.kind]}: ${d.label}`]
      if (d.owner) parts.push(`Owner: ${d.owner}`)
      if (d.due) parts.push(d.due)
      return parts.join(". ")
    }
  }
}

/** Kind, display fields and jump target for one tree node. */
function describe(a: Analysis, n: IdNode, depth: number, canJump: boolean): MindFields {
  const count = n.children.length
  if (depth === 0) return { kind: "root", label: n.node.label, count, jump: [] }
  const ref = resolveRef(a, n.node.ref)
  // Anything with children folds like a topic, e.g. the "Other" branch.
  if (count > 0 || ref?.kind === "topic" || depth === 1) return { kind: "topic", label: n.node.label, count, jump: [] }
  if (!ref) return { kind: "item", label: n.node.label, count, jump: [] }
  const jump = canJump ? ref.item.evidence ?? [] : []
  if (ref.kind === "action") {
    return {
      kind: "action",
      label: n.node.label,
      count,
      owner: ownerName(a, ref.item.owner),
      due: dueLabel(ref.item),
      jump,
    }
  }
  return { kind: ref.kind, label: n.node.label, count, jump }
}

export function MindMapView({
  analysis,
  mindmap,
  canJump,
  onJump,
}: {
  analysis: Analysis
  mindmap: MindNode
  canJump: boolean
  onJump: JumpToEvidence
}) {
  const root = useMemo(() => withIds(mindmap), [mindmap])
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(() => new Set())

  const { nodes, edges, byId } = useMemo(() => {
    const h = hierarchy(root, (d) => (collapsed.has(d.id) ? undefined : d.children))
    const laidOut = tree<IdNode>()
      .nodeSize([ROW, 1])
      .separation((a, b) => (a.parent === b.parent ? 1 : 1.3))(h)
    const nodes: MindFlowNode[] = []
    const edges: Edge[] = []
    const byId = new Map<string, MindData>()
    // Pre-order, so Tab walks the map the way it reads: a topic, then its items.
    laidOut.eachBefore((p) => {
      const top = p.ancestors().find((x) => x.depth === 1)
      const branch = top ? (root.children.indexOf(top.data) % BRANCH_COLORS) + 1 : 0
      const data: MindData = {
        ...describe(analysis, p.data, p.depth, canJump),
        branch,
        collapsed: collapsed.has(p.data.id),
      }
      byId.set(p.data.id, data)
      const interactive = data.kind === "topic" || data.jump.length > 0
      nodes.push({
        id: p.data.id,
        type: "mind",
        position: { x: columnX(p.depth), y: p.x },
        data,
        ariaLabel: ariaLabelFor(data),
        ariaRole: interactive ? "button" : "group",
        domAttributes: data.kind === "topic" ? { "aria-expanded": !data.collapsed } : undefined,
      })
      if (p.parent) {
        edges.push({
          id: `${p.parent.data.id}->${p.data.id}`,
          source: p.parent.data.id,
          target: p.data.id,
          style: { stroke: `var(--branch-${branch})`, strokeWidth: 2 },
          // The list view carries the structure; "Edge from 0 to 0.1" is noise.
          domAttributes: { "aria-hidden": true },
        })
      }
    })
    return { nodes, edges, byId }
  }, [root, collapsed, analysis, canJump])

  const activate = (id: string) => {
    const d = byId.get(id)
    if (!d) return
    if (d.kind === "topic") {
      setCollapsed((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        return next
      })
    } else if (d.jump.length > 0) {
      onJump(d.jump)
    }
  }

  // React Flow's node wrapper holds the focus; Enter/Space on it acts like a click.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Enter" && e.key !== " ") return
    const target = e.target as HTMLElement
    if (!target.classList.contains("react-flow__node")) return
    const id = target.dataset.id
    if (!id) return
    e.preventDefault()
    activate(id)
  }

  return (
    <div className="grid gap-3">
      <p className="text-sm text-muted-foreground">
        {canJump
          ? "Click a topic to open or close it. Click a decision or action item to see where it was said."
          : "Click a topic to open or close it."}
      </p>
      <div className="catchm-flow h-[min(65vh,560px)] min-h-72 overflow-hidden rounded-xl border border-border">
        <ReactFlow
          aria-label={`Mind map of ${mindmap.label}`}
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          nodeOrigin={[0, 0.5]}
          onNodeClick={(_, n) => activate(n.id)}
          onKeyDown={onKeyDown}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          edgesFocusable={false}
          panActivationKeyCode={null}
          deleteKeyCode={null}
          // The page scrolls past the map; Ctrl + scroll, pinch or the buttons zoom.
          preventScrolling={false}
          zoomOnScroll={false}
          fitView
          fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
          minZoom={0.2}
          maxZoom={1.5}
          ariaLabelConfig={ARIA_LABELS}
        >
          <Background gap={20} size={1} />
          <Controls showInteractive={false} position="bottom-left" />
        </ReactFlow>
      </div>
      <Disclosure label="List view" defaultOpen={narrowScreen()}>
        <MindList node={root} depth={0} analysis={analysis} canJump={canJump} onJump={onJump} />
      </Disclosure>
    </div>
  )
}

/** React Flow's built-in screen-reader text, in the app's words. */
const NODE_HELP = "Press Enter to open or close a topic, or to see where an item was said in the transcript."
const ARIA_LABELS = {
  "node.a11yDescription.default": NODE_HELP,
  "node.a11yDescription.keyboardDisabled": NODE_HELP,
  "controls.ariaLabel": "Zoom controls",
  "controls.zoomIn.ariaLabel": "Zoom in",
  "controls.zoomOut.ariaLabel": "Zoom out",
  "controls.fitView.ariaLabel": "Fit to view",
}

/** The same tree as nested lists: the map's text alternative. */
function MindList({
  node,
  depth,
  analysis,
  canJump,
  onJump,
}: {
  node: IdNode
  depth: number
  analysis: Analysis
  canJump: boolean
  onJump: JumpToEvidence
}) {
  const d = describe(analysis, node, depth, canJump)
  const topicEvidence = depth === 1 && canJump ? (resolveRef(analysis, node.node.ref)?.item.evidence ?? []) : []
  const jump = d.jump.length > 0 ? d.jump : topicEvidence
  const kids = node.children.length > 0 && (
    <ul className={cn("grid gap-2", depth > 0 && "mt-2 border-l border-border pl-4")}>
      {node.children.map((c) => (
        <MindList key={c.id} node={c} depth={depth + 1} analysis={analysis} canJump={canJump} onJump={onJump} />
      ))}
    </ul>
  )
  if (depth === 0) {
    return (
      <div>
        <p className="mb-2 font-medium">{node.node.label}</p>
        {kids}
      </div>
    )
  }
  return (
    <li className="break-words text-sm">
      <span className={cn(d.kind === "topic" && "font-medium")}>
        {d.kind !== "topic" && <span className="text-muted-foreground">{KIND_LABEL[d.kind]}: </span>}
        {d.label}
      </span>
      {(d.owner || d.due) && (
        <span className="text-muted-foreground"> · {[d.owner, d.due].filter(Boolean).join(" · ")}</span>
      )}
      {jump.length > 0 && (
        <>
          {" "}
          <SourceLink onClick={() => onJump(jump)} />
        </>
      )}
      {kids}
    </li>
  )
}
