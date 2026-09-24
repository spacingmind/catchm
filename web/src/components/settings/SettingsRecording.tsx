import { useEffect, useRef, useState } from "react"

import { SegmentedControl, SettingRow, SettingsSection, SaveStatus } from "@/components/settings/pieces"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getAudioSources, getDevices, type Device } from "@/lib/api"

/** UI-only prefs (non-secret), stored locally on this device. */
const PREFS_KEY = "catchm.prefs.recording"
const PREF_SOURCE = `${PREFS_KEY}.source`
const PREF_DEVICE = `${PREFS_KEY}.device`

export type AudioSource = "mic" | "system" | "both"

function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage unavailable — the pref just won't persist.
  }
}

export function SettingsRecording() {
  const [source, setSource] = useState<AudioSource>(() => (readStored(PREF_SOURCE) as AudioSource) ?? "mic")
  const [device, setDevice] = useState<string>(() => readStored(PREF_DEVICE) ?? "")
  const [devices, setDevices] = useState<Device[]>([])
  const [systemSupported, setSystemSupported] = useState<boolean | null>(null)
  const [systemReason, setSystemReason] = useState<string>("System audio isn't available yet")
  const [saveState, setSaveState] = useState<"idle" | "saved">("idle")
  const saveTimer = useRef<number | null>(null)

  // Quiet "Saved" indicator, cleared ~2s after the last write.
  const flashSaved = () => {
    setSaveState("saved")
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => setSaveState("idle"), 2000)
  }
  useEffect(() => () => {
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
  }, [])

  // Device list: prefer M19's /api/audio/sources, fall back to
  // /api/devices, and degrade to an empty list (select shows "None found")
  // if neither works.
  useEffect(() => {
    let cancelled = false
    getAudioSources()
      .then((s) => {
        if (cancelled) return
        setDevices(s.mic_devices ?? [])
        setSystemSupported(s.system?.supported ?? null)
        if (s.system?.reason) setSystemReason(s.system.reason)
      })
      .catch(() =>
        getDevices()
          .then((ds) => {
            if (!cancelled) setDevices(ds)
          })
          .catch(() => {
            if (!cancelled) setDevices([])
          }),
      )
    return () => {
      cancelled = true
    }
  }, [])

  const systemDisabledReason =
    systemSupported === true
      ? undefined
      : systemSupported === false
        ? systemReason
        : `${systemReason} (waiting for the audio backend)`

  const handleSource = (next: AudioSource) => {
    setSource(next)
    writeStored(PREF_SOURCE, next)
    flashSaved()
  }

  const handleDevice = (next: string) => {
    setDevice(next)
    writeStored(PREF_DEVICE, next)
    flashSaved()
  }

  return (
    <div>
      <SettingsSection
        title="Recording"
        description="Defaults used when you start a meeting. Stored on this device."
      >
        <SettingRow
          label="Audio source"
          htmlFor="recording-source"
          description={
            systemSupported === null
              ? "Microphone now; system audio needs a newer CatchM backend."
              : "What to capture while recording."
          }
          control={
            <SegmentedControl
              label="Audio source"
              value={source}
              onChange={handleSource}
              options={[
                { value: "mic", label: "Microphone" },
                {
                  value: "system",
                  label: "System audio",
                  disabled: systemSupported !== true,
                  title: systemDisabledReason,
                },
                {
                  value: "both",
                  label: "Both",
                  disabled: systemSupported !== true,
                  title: systemDisabledReason,
                },
              ]}
            />
          }
        />
        <SettingRow
          label="Microphone"
          htmlFor="recording-device"
          description={
            devices.length === 0 ? "No microphone found." : "Used for the microphone input."
          }
          control={
            <Select value={device || undefined} onValueChange={handleDevice}>
              <SelectTrigger id="recording-device" className="w-72">
                <SelectValue placeholder={devices.length === 0 ? "None found" : "Default device"} />
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
          }
        />
        <div className="flex items-center justify-end">
          <SaveStatus state={saveState} />
        </div>
      </SettingsSection>
    </div>
  )
}
