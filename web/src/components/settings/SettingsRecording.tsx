import { useEffect, useRef, useState } from "react"

import { SegmentedControl, SettingRow, SettingsSection, SaveStatus } from "@/components/settings/pieces"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getAudioSources, getDevices, type AudioSources, type Device } from "@/lib/api"
import {
  effectiveSource,
  readDevicePref,
  readSourcePref,
  writeDevicePref,
  writeSourcePref,
  type AudioSource,
} from "@/lib/recordPrefs"

export function SettingsRecording() {
  const [source, setSource] = useState<AudioSource>(readSourcePref)
  const [device, setDevice] = useState<string>(readDevicePref)
  const [devices, setDevices] = useState<Device[]>([])
  const [sources, setSources] = useState<AudioSources | null>(null)
  const [saveState, setSaveState] = useState<"idle" | "saved">("idle")
  const saveTimer = useRef<number | null>(null)

  const flashSaved = () => {
    setSaveState("saved")
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => setSaveState("idle"), 2000)
  }
  useEffect(
    () => () => {
      if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
    },
    [],
  )

  // Device list: prefer M19's /api/audio/sources, fall back to
  // /api/devices, and degrade to an empty list if neither works.
  useEffect(() => {
    let cancelled = false
    getAudioSources()
      .then((s) => {
        if (cancelled) return
        setSources(s)
        setDevices(s.mic_devices ?? [])
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

  const systemAvailable = sources?.system?.supported === true
  // Unknown (M19 backend missing) vs known-false get slightly different words.
  const systemReason =
    sources?.system?.supported === false && sources.system.reason
      ? sources.system.reason
      : "System audio needs a newer CatchM backend"

  const effective = effectiveSource(source, sources)
  const storedUnavailable = effective !== source

  const handleSource = (next: AudioSource) => {
    setSource(next)
    writeSourcePref(next)
    flashSaved()
  }

  const handleDevice = (next: string) => {
    setDevice(next)
    writeDevicePref(next)
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
            storedUnavailable
              ? "Your saved choice isn't available right now — meetings start with Microphone."
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
                  disabled: !systemAvailable,
                  title: systemReason,
                },
                {
                  value: "both",
                  label: "Both",
                  disabled: !systemAvailable,
                  title: systemReason,
                },
              ]}
            />
          }
        />
        {storedUnavailable && (
          <p className="pb-4 text-sm text-muted-foreground">
            {systemReason}. Starting meetings with{" "}
            <strong className="font-medium text-foreground">Microphone</strong> until it's available.
          </p>
        )}
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
