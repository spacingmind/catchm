import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { SegmentedControl, SettingRow, SettingsSection, SaveStatus, descId, labelId } from "@/components/settings/pieces"
import { useSaveStatus } from "@/components/settings/useConfigSave"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getAudioSources, getDevices, type AudioSources, type Device } from "@/lib/api"
import { sourceHint, sourceLabel } from "@/lib/labels"
import {
  effectiveSource,
  readDevicePref,
  readSourcePref,
  writeDevicePref,
  writeSourcePref,
  type AudioSource,
} from "@/lib/recordPrefs"

// Radix Select reserves "" (it means "no value"), so the stored "" pref
// — use the system default mic — gets a sentinel item value.
const DEFAULT_DEVICE = "__default__"

export function SettingsRecording() {
  const { t } = useTranslation()
  const [source, setSource] = useState<AudioSource>(readSourcePref)
  const [device, setDevice] = useState<string>(readDevicePref)
  // null until the device list has loaded (or failed to).
  const [devices, setDevices] = useState<Device[] | null>(null)
  const [sources, setSources] = useState<AudioSources | null>(null)
  const { save, flashSaved } = useSaveStatus()

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
  // Unknown (M19 backend missing) vs known-false get different words.
  // Shown under the segmented control; the backend's reason may or may
  // not end in a period.
  const systemReason =
    sources?.system?.supported === false && sources.system.reason
      ? `${sources.system.reason.replace(/[.\s]+$/, "")}.`
      : t("settings.recording.systemNeedsUpdate", { source: sourceLabel("system") })

  const effective = effectiveSource(source, sources)
  const storedUnavailable = effective !== source

  const deviceList = devices ?? []
  const noDevices = devices !== null && devices.length === 0
  // A saved mic that's no longer connected: keep it selected (it may come
  // back) but say so, instead of a blank trigger.
  const staleDevice = devices !== null && device !== "" && !devices.some((d) => d.ID === device)

  const handleSource = (next: AudioSource) => {
    setSource(next)
    writeSourcePref(next)
    flashSaved()
  }

  const handleDevice = (next: string) => {
    const value = next === DEFAULT_DEVICE ? "" : next
    setDevice(value)
    writeDevicePref(value)
    flashSaved()
  }

  const micDescription =
    devices === null
      ? t("settings.recording.micLooking")
      : noDevices
        ? t("settings.recording.micNone")
        : staleDevice
          ? t("settings.recording.micStale")
          : t("settings.recording.micDesc")

  return (
    <div>
      <SettingsSection title={t("settings.nav.recording")} description={t("settings.recording.desc")}>
        <SettingRow
          label={t("statusRow.audioSource")}
          id="recording-source"
          description={
            storedUnavailable
              ? t("settings.recording.sourceDescStored", { mic: sourceLabel("mic") })
              : sourceHint()
          }
          control={
            <SegmentedControl
              id="recording-source"
              labelledBy={labelId("recording-source")}
              describedBy={descId("recording-source")}
              value={source}
              onChange={handleSource}
              options={[
                { value: "mic", label: sourceLabel("mic") },
                {
                  value: "system",
                  label: sourceLabel("system"),
                  disabled: !systemAvailable,
                  reason: systemReason,
                },
                {
                  value: "both",
                  label: sourceLabel("both"),
                  disabled: !systemAvailable,
                  reason: systemReason,
                },
              ]}
            />
          }
        />
        <SettingRow
          label={t("statusRow.microphone")}
          htmlFor="recording-device"
          description={micDescription}
          control={
            <Select value={device || DEFAULT_DEVICE} onValueChange={handleDevice} disabled={deviceList.length === 0}>
              <SelectTrigger id="recording-device" className="w-72" aria-describedby={descId("recording-device")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DEFAULT_DEVICE}>{t("statusRow.defaultMic")}</SelectItem>
                {staleDevice && <SelectItem value={device}>{t("settings.recording.deviceStale")}</SelectItem>}
                {deviceList.map((d) => (
                  <SelectItem key={d.ID} value={d.ID}>
                    {d.Name}
                    {d.IsDefault ? t("statusRow.defaultSuffix") : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          }
        />
        <div className="flex items-center justify-end">
          <SaveStatus state={save.state} />
        </div>
      </SettingsSection>
    </div>
  )
}
