import { useEffect, useState, type FormEvent } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getDevices, startMeeting, type Device, type Meeting, type SttEngine } from "@/lib/api"

interface NewMeetingDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onStarted: (meeting: Meeting) => void
}

export function NewMeetingDialog({ open, onOpenChange, onStarted }: NewMeetingDialogProps) {
  const [title, setTitle] = useState("")
  const [devices, setDevices] = useState<Device[]>([])
  const [device, setDevice] = useState("")
  const [engine, setEngine] = useState<SttEngine>("whisper")
  const [lang, setLang] = useState("")
  const [translateTo, setTranslateTo] = useState("")
  const [twoWay, setTwoWay] = useState(false)
  const [tags, setTags] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) {
      setTitle("")
      setDevice("")
      setEngine("whisper")
      setLang("")
      setTranslateTo("")
      setTwoWay(false)
      setTags("")
      setError(null)
      setSubmitting(false)
      return
    }
    getDevices()
      .then(setDevices)
      .catch(() => setDevices([]))
  }, [open])

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!title.trim() || submitting) return
    if (twoWay && (!lang.trim() || !translateTo.trim())) {
      setError("Two-way translation needs both the source language and language B.")
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const parsedTags = [...new Set(
        tags.split(",").map((t) => t.trim()).filter(Boolean),
      )]
      const meeting = await startMeeting({
        title: title.trim(),
        device: device || undefined,
        engine,
        lang: lang.trim() || undefined,
        translate_to: translateTo.trim() || undefined,
        two_way: twoWay || undefined,
        tags: parsedTags.length > 0 ? parsedTags : undefined,
      })
      onStarted(meeting)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Record a meeting</DialogTitle>
            <DialogDescription>
              Start capturing the conversation, decisions, and follow-ups.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4">
            <div className="grid gap-1.5">
              <Label htmlFor="meeting-title">Title</Label>
              <Input
                id="meeting-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Weekly sync"
                required
                autoFocus
              />
            </div>

            <div className="grid gap-1.5">
              <Label htmlFor="meeting-device">Device</Label>
              <Select value={device} onValueChange={setDevice}>
                <SelectTrigger id="meeting-device" className="w-full">
                  <SelectValue placeholder="Default device" />
                </SelectTrigger>
                <SelectContent>
                  {devices.map((d) => (
                    <SelectItem key={d.ID} value={d.ID}>
                      {d.Name}
                      {d.IsDefault ? " (default)" : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-1.5">
              <Label htmlFor="meeting-engine">Engine</Label>
              <Select value={engine} onValueChange={(v) => setEngine(v as SttEngine)}>
                <SelectTrigger id="meeting-engine" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="whisper">whisper</SelectItem>
                  <SelectItem value="soniox">soniox</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-1.5">
              <Label htmlFor="meeting-lang">
                {twoWay ? "Language A" : "Source language"}
              </Label>
              <Input
                id="meeting-lang"
                value={lang}
                onChange={(e) => setLang(e.target.value)}
                placeholder={twoWay ? "Required, e.g. en" : "Optional, e.g. en"}
                required={twoWay}
              />
            </div>

            <div className="grid gap-1.5">
              <Label htmlFor="meeting-translate-to">
                {twoWay ? "Language B" : "Translate to"}
              </Label>
              <Input
                id="meeting-translate-to"
                value={translateTo}
                onChange={(e) => setTranslateTo(e.target.value)}
                placeholder={twoWay ? "Required, e.g. vi" : "Optional, e.g. es"}
                required={twoWay}
              />
            </div>

            <div className="flex items-center gap-2">
              <input
                id="meeting-two-way"
                type="checkbox"
                className="size-4 accent-primary"
                checked={twoWay}
                onChange={(e) => setTwoWay(e.target.checked)}
              />
              <Label htmlFor="meeting-two-way">Two-way translation</Label>
            </div>

            <div className="grid gap-1.5">
              <Label htmlFor="meeting-tags">Tags</Label>
              <Input
                id="meeting-tags"
                value={tags}
                onChange={(e) => setTags(e.target.value)}
                placeholder="Optional, comma-separated, e.g. acme, q3"
              />
            </div>
          </div>

          {error && <p className="mt-3 text-sm text-destructive">{error}</p>}

          <DialogFooter>
            <Button type="submit" disabled={submitting || !title.trim()}>
              {submitting ? "Starting…" : "Start meeting"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
