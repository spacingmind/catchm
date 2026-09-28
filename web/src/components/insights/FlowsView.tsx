import "./flow.css"

import { useMemo, useRef, type KeyboardEvent } from "react"
import { Graph, layout } from "@dagrejs/dagre"
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
  type ReactFlowInstance,
} from "@xyflow/react"
import { Route as RouteIcon } from "lucide-react"

import { Disclosure } from "@/components/Disclosure"
import { SourceLink } from "@/components/insights/Evidence"
import { narrowScreen, ownerName, type JumpToEvidence } from "@/components/insights/evidence"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { Analysis, Flow, FlowStep } from "@/lib/api"
import { cn } from "cn"

/*
 * Flows (docs/plans/active/meeting-insights.md, decision 4): each process
 * the flows pass found, as a card with its steps laid out left to right
 * by dagre. Nodes are steps (order badge, text, actor); edges are the
 * `next` links, labelled with their condition when they branch.
 */

const STEP_W = 184
const STEP_H = 92
/** Opening zoom never goes below this: a long flow opens at its first
 *  steps, readable, and pans, rather than shrinking to fit. */
const MIN_OPEN_ZOOM = 0.7

/** A step's own lines where the model could tell, else its flow's. */
function stepEvidence(flow: Flow, step: FlowStep): number[] {
  return step.evidence && step.evidence.length > 0 ? step.evidence : (flow.evidence ?? [])
}

interface StepData extends Record<string, unknown> {
  order: number
  text: string
  actor?: string
  jump: number[]
  hasIn: boolean
  hasOut: boolean
}

type StepNode = Node<StepData, "step">

function StepNodeView({ data }: NodeProps<StepNode>) {
  return (
    <>
      {data.hasIn && <Handle type="target" position={Position.Left} isConnectable={false} />}
      <div
        style={{ width: STEP_W, minHeight: STEP_H - 12 }}
        title={data.text}
        className={cn(
          "flex items-start gap-2 rounded-lg border border-border bg-card px-3 py-2 text-card-foreground shadow-sm",
          data.jump.length > 0 && "cursor-pointer hover:border-ring hover:bg-muted",
        )}
      >
        <span
          aria-hidden="true"
          className="grid size-6 shrink-0 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground tabular-nums"
        >
          {data.order}
        </span>
        <div className="grid min-w-0 gap-0.5">
          <p className="line-clamp-3 text-sm">{data.text}</p>
          {data.actor && <p className="truncate text-xs text-muted-foreground">{data.actor}</p>}
        </div>
      </div>
      {data.hasOut && <Handle type="source" position={Position.Right} isConnectable={false} />}
    </>
  )
}

const nodeTypes = { step: StepNodeView }

const NODE_HELP = "Press Enter to see where this step was said in the transcript."
const ARIA_LABELS = {
  "node.a11yDescription.default": NODE_HELP,
  "node.a11yDescription.keyboardDisabled": NODE_HELP,
  "controls.ariaLabel": "Zoom controls",
  "controls.zoomIn.ariaLabel": "Zoom in",
  "controls.zoomOut.ariaLabel": "Zoom out",
  "controls.fitView.ariaLabel": "Fit to view",
}

const byOrder = (a: FlowStep, b: FlowStep) => a.order - b.order

function stepLabel(s: FlowStep): string {
  return `Step ${s.order}: ${s.text}${s.actor ? `. Who: ${s.actor}` : ""}`
}

/** Dagre positions (centres) for the steps and their edges. Actors are
 *  person keys when they name a listed person: shown as the name. */
function layoutFlow(a: Analysis, flow: Flow, canJump: boolean) {
  const steps = [...(flow.steps ?? [])].sort(byOrder).map((s) => ({ ...s, actor: ownerName(a, s.actor) }))
  const ids = new Set(steps.map((s) => s.id))
  const g = new Graph()
  g.setGraph({ rankdir: "LR", nodesep: 28, ranksep: 64, edgesep: 20, marginx: 12, marginy: 12 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const s of steps) g.setNode(s.id, { width: STEP_W, height: STEP_H })
  const edges: Edge[] = []
  const hasIn = new Set<string>()
  const hasOut = new Set<string>()
  for (const s of steps) {
    for (const n of s.next ?? []) {
      if (!ids.has(n.to)) continue
      // Room for the condition label, so dagre keeps it off the nodes.
      const label = n.condition ? { width: Math.min(160, n.condition.length * 7), height: 18, labelpos: "c" } : {}
      g.setEdge(s.id, n.to, label)
      hasOut.add(s.id)
      hasIn.add(n.to)
      edges.push({
        id: `${s.id}->${n.to}`,
        source: s.id,
        target: n.to,
        type: "smoothstep",
        label: n.condition || undefined,
        labelBgPadding: [4, 2],
        labelBgBorderRadius: 4,
        markerEnd: { type: MarkerType.ArrowClosed, color: "var(--input)", width: 16, height: 16 },
        // The list view spells out the branches; "Edge from s1 to s2" is noise.
        domAttributes: { "aria-hidden": true },
      })
    }
  }
  layout(g)
  const nodes: StepNode[] = steps.map((s) => {
    const p = g.node(s.id)
    const jump = canJump ? stepEvidence(flow, s) : []
    return {
      id: s.id,
      type: "step",
      position: { x: p.x, y: p.y },
      data: { order: s.order, text: s.text, actor: s.actor, jump, hasIn: hasIn.has(s.id), hasOut: hasOut.has(s.id) },
      ariaLabel: stepLabel(s),
      ariaRole: jump.length > 0 ? "button" : "group",
    }
  })
  const { width = STEP_W, height = STEP_H } = g.graph()
  return { steps, nodes, edges, width, height }
}

function FlowCard({
  analysis,
  flow,
  canJump,
  onJump,
}: {
  analysis: Analysis
  flow: Flow
  canJump: boolean
  onJump: JumpToEvidence
}) {
  const { steps, nodes, edges, width, height } = useMemo(() => layoutFlow(analysis, flow, canJump), [analysis, flow, canJump])
  const actor = ownerName(analysis, flow.actor)
  const jumpById = useMemo(() => new Map(nodes.map((n) => [n.id, n.data.jump])), [nodes])
  const orderById = useMemo(() => new Map(steps.map((s) => [s.id, s.order])), [steps])

  const activate = (id: string) => {
    const jump = jumpById.get(id)
    if (jump && jump.length > 0) onJump(jump)
  }
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Enter" && e.key !== " ") return
    const target = e.target as HTMLElement
    const id = target.classList.contains("react-flow__node") ? target.dataset.id : undefined
    if (!id) return
    e.preventDefault()
    activate(id)
  }

  // Dagre's coordinates are centres (nodeOrigin 0.5) spanning 0..width, 0..height.
  const boxRef = useRef<HTMLDivElement>(null)
  const boxHeight = Math.min(420, Math.max(180, height + 32))
  const openView = (rf: ReactFlowInstance<StepNode>) => {
    const box = boxRef.current
    if (!box) return
    const zoom = Math.min(1, Math.max(MIN_OPEN_ZOOM, (box.clientWidth - 24) / width))
    void rf.setViewport({ x: 12, y: Math.max(8, (box.clientHeight - height * zoom) / 2), zoom })
  }

  const flowJump = canJump ? (flow.evidence ?? []) : []
  const descId = `flow-${flow.id}-desc`
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">
          <h3 className="flex items-center gap-2 break-words">
            <RouteIcon aria-hidden="true" className="size-4 shrink-0" />
            {flow.name}
          </h3>
        </CardTitle>
        <CardDescription id={descId} className="grid gap-0.5 text-pretty">
          {actor && <span>Who: {actor}</span>}
          {flow.trigger && <span>Starts when: {flow.trigger}</span>}
          <span>
            {steps.length === 1 ? "1 step" : `${steps.length} steps`}
            {flowJump.length > 0 && (
              <>
                {" · "}
                <SourceLink onClick={() => onJump(flowJump)} />
              </>
            )}
          </span>
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3">
        {steps.length > 0 && (
          <div
            ref={boxRef}
            className="catchm-flow overflow-hidden rounded-lg border border-border"
            style={{ height: boxHeight }}
          >
            <ReactFlow
              aria-label={`Steps of ${flow.name}`}
              aria-describedby={descId}
              nodes={nodes}
              edges={edges}
              nodeTypes={nodeTypes}
              nodeOrigin={[0.5, 0.5]}
              onNodeClick={(_, n) => activate(n.id)}
              onKeyDown={onKeyDown}
              nodesDraggable={false}
              nodesConnectable={false}
              elementsSelectable={false}
              edgesFocusable={false}
              panActivationKeyCode={null}
              deleteKeyCode={null}
              preventScrolling={false}
              zoomOnScroll={false}
              onInit={openView}
              fitViewOptions={{ padding: 0.1, maxZoom: 1 }}
              minZoom={0.3}
              maxZoom={1.5}
              ariaLabelConfig={ARIA_LABELS}
            >
              <Background gap={20} size={1} />
              <Controls showInteractive={false} position="bottom-left" />
            </ReactFlow>
          </div>
        )}
        <Disclosure label="List view" defaultOpen={narrowScreen()}>
          <ol className="grid gap-2 text-sm">
            {steps.map((s) => {
              const branches = (s.next ?? []).filter((n) => orderById.has(n.to))
              // Only spell out "next" when it isn't simply the following step.
              const plain = branches.length === 1 && !branches[0].condition && orderById.get(branches[0].to) === s.order + 1
              const jump = canJump ? stepEvidence(flow, s) : []
              return (
                <li key={s.id} className="break-words">
                  <span className="font-medium tabular-nums">{s.order}.</span> {s.text}
                  {s.actor && <span className="text-muted-foreground"> · {s.actor}</span>}
                  {jump.length > 0 && (
                    <>
                      {" "}
                      <SourceLink onClick={() => onJump(jump)} />
                    </>
                  )}
                  {!plain && branches.length > 0 && (
                    <ul className="mt-1 grid gap-0.5 pl-5 text-muted-foreground">
                      {branches.map((n) => (
                        <li key={n.to}>
                          {n.condition
                            ? `If ${n.condition}: go to step ${orderById.get(n.to)}`
                            : `Then step ${orderById.get(n.to)}`}
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
          </ol>
        </Disclosure>
      </CardContent>
    </Card>
  )
}

export function FlowsView({ analysis, canJump, onJump }: { analysis: Analysis; canJump: boolean; onJump: JumpToEvidence }) {
  return (
    <div className="grid gap-6">
      {(analysis.flows ?? []).map((f) => (
        <FlowCard key={f.id} analysis={analysis} flow={f} canJump={canJump} onJump={onJump} />
      ))}
    </div>
  )
}
