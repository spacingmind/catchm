import { useEffect, useState } from "react"

import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useAnnounceChange, useStatusCheck } from "@/hooks/useStatusCheck"
import { getSttStatus, getSummaryStatus, getTranslateStatus } from "@/lib/api"
import { navigate } from "@/lib/route"

/**
 * "Hide" on the card. The value is whether the required item
 * (transcription) was ready when last seen hidden: a "ready" mark comes
 * back into view if transcription later stops being ready.
 */
const HIDE_KEY = "catchm.prefs.getStarted.hidden"

type HiddenMark = "ready" | "not-ready"

function readHidden(): HiddenMark | null {
  try {
    const v = window.localStorage.getItem(HIDE_KEY)
    return v === "ready" || v === "not-ready" ? v : null
  } catch {
    return null
  }
}

function writeHidden(mark: HiddenMark) {
  try {
    window.localStorage.setItem(HIDE_KEY, mark)
  } catch {
    // Storage unavailable: the card just shows again next time.
  }
}

type ItemState = "ready" | "not-ready" | "checking" | "unknown"

const STATE_LABEL: Record<ItemState, string> = {
  ready: "Ready",
  "not-ready": "Not set up",
  checking: "Checking…",
  unknown: "Couldn’t check",
}

interface SetupItem {
  id: string
  title: string
  /** "Required" / "Recommended" / "Optional", shown before the description. */
  need: string
  description: string
  state: ItemState
  /** Why it isn't ready, in plain words. */
  reason?: string
  section: string
}

function itemState(ready: boolean | null, error: string | null): ItemState {
  if (ready !== null) return ready ? "ready" : "not-ready"
  return error ? "unknown" : "checking"
}

/**
 * First-run setup checklist at the top of Meetings: transcription
 * (required), summaries (recommended) and translation (optional), each
 * with its status and a way to the settings page that sets it up. It
 * stays out of the way: nothing renders until the required and
 * recommended checks have answered (so it never flashes in and out), and
 * it's gone once those two are ready. "Hide" is remembered, until
 * transcription stops being ready. Re-checks when the window regains
 * focus.
 */
export function GetStarted({ onHide }: { onHide?: () => void }) {
  const stt = useStatusCheck(getSttStatus)
  const summary = useStatusCheck(getSummaryStatus)
  const translate = useStatusCheck(getTranslateStatus)
  const [hidden, setHidden] = useState<HiddenMark | null>(readHidden)

  const sttEngine = stt.data ? stt.data[stt.data.default_engine] : undefined
  const sttReady = stt.data ? (sttEngine?.ready ?? false) : null
  const summaryReady = summary.data ? summary.data.ready : null
  const translateReady = translate.data
    ? translate.data.ollama_reachable && translate.data.live_model.installed
    : null

  // Hidden while transcription wasn't ready, and now it is: from here on
  // it breaking again should bring the card back.
  if (hidden === "not-ready" && sttReady === true) setHidden("ready")
  useEffect(() => {
    if (hidden) writeHidden(hidden)
  }, [hidden])

  const items: SetupItem[] = [
    {
      id: "transcription",
      title: "Transcription",
      need: "Required",
      description: "Turns what people say into text.",
      state: itemState(sttReady, stt.error),
      reason: sttReady === false ? sttEngine?.reason : undefined,
      section: "transcription",
    },
    {
      id: "summaries",
      title: "Summaries",
      need: "Recommended",
      description: "Writes the summary, decisions and action items after a meeting.",
      state: itemState(summaryReady, summary.error),
      reason: summaryReady === false ? summary.data?.reason : undefined,
      section: "summaries",
    },
    {
      id: "translation",
      title: "Translation",
      need: "Optional",
      description: "Only needed if you translate meetings.",
      state: itemState(translateReady, translate.error),
      reason:
        translateReady === false
          ? translate.data?.ollama_reachable
            ? "The translation model isn’t downloaded yet."
            : "Needs Ollama, a free app, running on this computer."
          : undefined,
      section: "translation",
    },
  ]

  // Spoken only when an item's status changes, once every check answered.
  const announcement = useAnnounceChange(
    items.some((i) => i.state === "checking")
      ? null
      : items.map((i) => `${i.title}: ${STATE_LABEL[i.state]}.`).join(" "),
  )

  const settled = (stt.data || stt.error) && (summary.data || summary.error)
  const needsSetup = sttReady === false || summaryReady === false
  const reshown = hidden === "ready" && sttReady === false
  const visible = Boolean(settled) && needsSetup && (!hidden || reshown)

  const hide = () => {
    setHidden(sttReady === false ? "not-ready" : "ready")
    onHide?.()
  }

  return (
    <>
      <StatusMessage className="sr-only">{visible ? announcement : null}</StatusMessage>
      {visible && (
        <Card className="mb-6">
          <CardHeader>
            <CardTitle className="text-lg font-semibold">
              <h2>Finish setting up CatchM</h2>
            </CardTitle>
            <CardDescription className="text-pretty">
              A few one-time steps, so CatchM can turn your meetings into text and summaries.
            </CardDescription>
            <CardAction>
              <Button size="sm" variant="ghost" onClick={hide}>
                Hide<span className="sr-only"> setup steps</span>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <ul className="grid">
              {items.map((item) => (
                <SetupRow key={item.id} item={item} />
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </>
  )
}

function SetupRow({ item }: { item: SetupItem }) {
  const { id, title, need, description, state, reason, section } = item
  const descriptionId = `get-started-${id}-desc`
  return (
    <li className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 border-b border-border py-3 last:border-b-0 last:pb-0">
      <div className="min-w-0 flex-1">
        <h3 className="text-sm font-medium">{title}</h3>
        <p id={descriptionId} className="mt-0.5 text-sm text-muted-foreground">
          {need}. {description}
          {reason && <span className="mt-0.5 block text-foreground">{reason}</span>}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Badge
          variant={state === "not-ready" && need === "Required" ? "destructive" : state === "ready" ? "secondary" : "outline"}
          className={
            state === "ready"
              ? "border-transparent bg-primary/15 text-primary"
              : state === "checking"
                ? "motion-safe:animate-pulse"
                : undefined
          }
        >
          {STATE_LABEL[state]}
        </Badge>
        {(state === "not-ready" || state === "unknown") && (
          <Button
            size="sm"
            variant={need === "Optional" ? "outline" : "default"}
            onClick={() => navigate({ name: "settings", section })}
            aria-describedby={descriptionId}
          >
            Set up<span className="sr-only"> {title.toLowerCase()}</span>
          </Button>
        )}
      </div>
    </li>
  )
}
