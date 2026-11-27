import { useEffect, useRef, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import { QRCodeSVG } from "qrcode.react"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { StatusMessage } from "@/components/StatusMessage"
import { oneTimeCodeProps } from "@/components/settings/CloudCodeStep"
import { SettingRow, SettingsSection, descId } from "@/components/settings/pieces"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  ApiError,
  cloudRevokeOtherSessions,
  cloudRevokeSession,
  cloudSessions,
  cloudTotpDisable,
  cloudTotpEnable,
  cloudTotpSetup,
  type CloudSession,
} from "@/lib/api"
import { formatDateTime } from "@/lib/format"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** The server ended this device's session (signed out elsewhere, or expired). */
const sessionEnded = (err: unknown) => err instanceof ApiError && err.code === "session_ended"

/**
 * Settings → Cloud sync → Security, while logged in: two-factor
 * authentication and the signed-in devices list. onStatusChange asks the
 * parent to re-read /api/cloud/status (two-factor on/off, or this device
 * signed out); onSessionEnded(message) is for when this device is no
 * longer logged in.
 */
export function CloudSecurity({
  totpEnabled,
  onStatusChange,
  onSessionEnded,
}: {
  totpEnabled: boolean
  onStatusChange: () => void
  onSessionEnded: (message: string | null) => void
}) {
  const { t } = useTranslation()
  return (
    <SettingsSection title={t("settings.cloudSync.security.title")} description={t("settings.cloudSync.security.desc")}>
      <TwoFactor totpEnabled={totpEnabled} onStatusChange={onStatusChange} onSessionEnded={onSessionEnded} />
      <Devices onSessionEnded={onSessionEnded} />
    </SettingsSection>
  )
}

function TwoFactor({
  totpEnabled,
  onStatusChange,
  onSessionEnded,
}: {
  totpEnabled: boolean
  onStatusChange: () => void
  onSessionEnded: (message: string | null) => void
}) {
  const { t } = useTranslation()
  // "setup": QR + confirm code; "disable": a code to turn it off.
  const [panel, setPanel] = useState<"setup" | "disable" | null>(null)
  const [setup, setSetup] = useState<{ secret: string; otpauth_uri: string } | null>(null)
  const [starting, setStarting] = useState(false)
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [copy, setCopy] = useState<boolean | null>(null)
  const codeRef = useRef<HTMLInputElement | null>(null)

  useEffect(() => {
    if (panel === "disable" || (panel === "setup" && setup)) codeRef.current?.focus()
  }, [panel, setup])

  const close = () => {
    setPanel(null)
    setSetup(null)
    setCode("")
    setError(null)
    setCopy(null)
  }

  /** Shared failure handling; true when it handled the error. */
  const handled = (err: unknown) => {
    if (sessionEnded(err)) {
      onSessionEnded(errorMessage(err))
      return true
    }
    if (err instanceof ApiError && (err.code === "totp_already_enabled" || err.code === "totp_not_enabled")) {
      // The server knows better than this device: show its state.
      close()
      setNotice(err.message)
      onStatusChange()
      return true
    }
    return false
  }

  const startSetup = async () => {
    if (starting) return
    setStarting(true)
    setNotice(null)
    setError(null)
    try {
      setSetup(await cloudTotpSetup())
      setPanel("setup")
    } catch (err) {
      if (!handled(err)) setError(errorMessage(err))
    } finally {
      setStarting(false)
    }
  }

  const openDisable = () => {
    setNotice(null)
    setError(null)
    setCode("")
    setPanel("disable")
  }

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (busy || !panel) return
    if (!code.trim()) {
      setError(t("settings.cloudSync.codeRequired"))
      codeRef.current?.focus()
      return
    }
    setBusy(true)
    setError(null)
    try {
      if (panel === "setup") await cloudTotpEnable(code.trim())
      else await cloudTotpDisable(code.trim())
      const done = panel === "setup" ? t("settings.cloudSync.security.enabledNotice") : t("settings.cloudSync.security.disabledNotice")
      close()
      setNotice(done)
      onStatusChange()
    } catch (err) {
      if (!handled(err)) {
        setError(errorMessage(err))
        codeRef.current?.select()
      }
    } finally {
      setBusy(false)
    }
  }

  const copyKey = () => {
    if (!setup) return
    const write = navigator.clipboard?.writeText(setup.secret)
    if (!write) {
      setCopy(false)
      return
    }
    write.then(
      () => setCopy(true),
      () => setCopy(false),
    )
  }

  return (
    <div className="border-b border-border">
      {/* Wrapped so the row draws no divider of its own: this block's
          border sits below the setup panel instead. */}
      <div>
        <SettingRow
          label={t("settings.cloudSync.security.twoFactorLabel")}
          id="cloud-2fa"
          description={
            totpEnabled ? t("settings.cloudSync.security.twoFactorDescOn") : t("settings.cloudSync.security.twoFactorDescOff")
          }
          control={
            <div className="flex items-center gap-2">
              {totpEnabled ? (
                <Badge variant="secondary" className="border-transparent bg-primary/15 text-primary">
                  {t("settings.cloudSync.security.on")}
                </Badge>
              ) : (
                <Badge variant="outline">{t("settings.cloudSync.security.off")}</Badge>
              )}
              {panel === null &&
                (totpEnabled ? (
                  <Button size="sm" variant="outline" onClick={openDisable}>
                    {t("settings.cloudSync.security.turnOff")}
                  </Button>
                ) : (
                  <Button size="sm" onClick={() => void startSetup()} disabled={starting} aria-busy={starting || undefined}>
                    {starting ? t("settings.cloudSync.security.startingSetup") : t("settings.cloudSync.security.setUp")}
                  </Button>
                ))}
            </div>
          }
        />
      </div>

      {panel !== null && (
        <form noValidate onSubmit={(e) => void submit(e)} className="mb-4 grid gap-4 rounded-lg border border-border p-3">
          {panel === "setup" && setup && (
            <div className="grid gap-3">
              <p className="text-sm text-muted-foreground">{t("settings.cloudSync.security.setupScan")}</p>
              {/* Dark modules on white in either theme: scanners need the contrast. */}
              <div className="w-fit rounded-lg bg-white p-2">
                <QRCodeSVG
                  value={setup.otpauth_uri}
                  size={160}
                  marginSize={1}
                  title={t("settings.cloudSync.security.qrTitle")}
                />
              </div>
              <div className="grid gap-1.5">
                <p className="text-sm text-muted-foreground">{t("settings.cloudSync.security.setupKey")}</p>
                <div className="flex flex-wrap items-center gap-2">
                  <code translate="no" className="select-all rounded-md bg-muted px-2 py-1 font-mono text-sm break-all">
                    {setup.secret.replace(/(.{4})(?=.)/g, "$1 ")}
                  </code>
                  <Button type="button" size="sm" variant="outline" onClick={copyKey}>
                    {t("settings.cloudSync.security.copyKey")}
                  </Button>
                </div>
                <StatusMessage tone={copy === false ? "error" : "success"} className="text-xs">
                  {copy === null
                    ? null
                    : copy
                      ? t("settings.cloudSync.security.keyCopied")
                      : t("settings.cloudSync.security.keyCopyFailed")}
                </StatusMessage>
              </div>
            </div>
          )}
          {panel === "disable" && (
            <p className="text-sm text-muted-foreground">{t("settings.cloudSync.security.turnOffIntro")}</p>
          )}
          <div className="grid gap-1.5">
            <label htmlFor="cloud-2fa-code" className="text-sm font-medium">
              {panel === "setup" ? t("settings.cloudSync.security.setupCodeLabel") : t("settings.cloudSync.totpCodeLabel")}
            </label>
            <Input
              id="cloud-2fa-code"
              ref={codeRef}
              name="totp_code"
              {...oneTimeCodeProps}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="123456"
              aria-invalid={error ? true : undefined}
              aria-describedby={
                [panel === "setup" && descId("cloud-2fa-code"), error && "cloud-2fa-error"].filter(Boolean).join(" ") ||
                undefined
              }
              className="w-40 font-mono tracking-widest tabular-nums"
            />
            {panel === "setup" && (
              <p id={descId("cloud-2fa-code")} className="text-xs text-muted-foreground">
                {t("settings.cloudSync.security.setupCodeDesc")}
              </p>
            )}
            <StatusMessage id="cloud-2fa-error" tone="error" className="break-words">
              {error}
            </StatusMessage>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="submit"
              variant={panel === "disable" ? "destructive" : "default"}
              disabled={busy}
              aria-busy={busy || undefined}
            >
              {panel === "setup"
                ? busy
                  ? t("settings.cloudSync.security.turningOn")
                  : t("settings.cloudSync.security.turnOn")
                : busy
                  ? t("settings.cloudSync.security.turningOff")
                  : t("settings.cloudSync.security.turnOff")}
            </Button>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>
              {t("common.cancel")}
            </Button>
          </div>
        </form>
      )}
      <StatusMessage tone={panel === null && error ? "error" : "success"} className="mb-4 break-words">
        {panel === null ? (error ?? notice) : null}
      </StatusMessage>
    </div>
  )
}

/** ms epoch → the app's date-time format. */
const formatMs = (ms: number) => formatDateTime(new Date(ms).toISOString())

function Devices({ onSessionEnded }: { onSessionEnded: (message: string | null) => void }) {
  const { t } = useTranslation()
  const [sessions, setSessions] = useState<CloudSession[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [rowError, setRowError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [confirmPending, setConfirmPending] = useState(false)
  const [confirmError, setConfirmError] = useState<string | null>(null)

  const load = () => {
    setLoadError(null)
    cloudSessions()
      .then((res) => {
        // This device first, then most recently active (the server's order).
        setSessions([...res.sessions].sort((a, b) => Number(b.current) - Number(a.current)))
      })
      .catch((err: unknown) => {
        if (sessionEnded(err)) onSessionEnded(errorMessage(err))
        else setLoadError(errorMessage(err))
      })
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const signOut = async (s: CloudSession) => {
    if (busyId) return
    setBusyId(s.id)
    setRowError(null)
    setNotice(null)
    try {
      const res = await cloudRevokeSession(s.id)
      if (res.signed_out) {
        onSessionEnded(null)
        return
      }
      setSessions((prev) => prev?.filter((x) => x.id !== s.id) ?? null)
    } catch (err) {
      if (sessionEnded(err)) onSessionEnded(errorMessage(err))
      else if (err instanceof ApiError && err.code === "session_not_found") load()
      else setRowError(t("settings.cloudSync.security.signOutError", { error: errorMessage(err) }))
    } finally {
      setBusyId(null)
    }
  }

  const signOutOthers = async () => {
    setConfirmPending(true)
    setConfirmError(null)
    try {
      const res = await cloudRevokeOtherSessions()
      setConfirmOpen(false)
      setNotice(t("settings.cloudSync.security.signedOutOthers", { count: res.revoked }))
      load()
    } catch (err) {
      if (sessionEnded(err)) {
        setConfirmOpen(false)
        onSessionEnded(errorMessage(err))
      } else setConfirmError(errorMessage(err))
    } finally {
      setConfirmPending(false)
    }
  }

  const others = sessions?.filter((s) => !s.current).length ?? 0

  return (
    <div className="pt-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-medium">{t("settings.cloudSync.security.devicesLabel")}</h3>
          <p className="mt-0.5 text-sm text-muted-foreground">{t("settings.cloudSync.security.devicesDesc")}</p>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={others === 0}
          onClick={() => {
            setConfirmError(null)
            setConfirmOpen(true)
          }}
        >
          {t("settings.cloudSync.security.signOutOthers")}
        </Button>
      </div>

      {sessions === null && !loadError && (
        <div aria-busy="true" className="mt-3 grid gap-2">
          <span className="sr-only">{t("settings.cloudSync.security.loadingDevices")}</span>
          <Skeleton className="h-12 w-full rounded-lg" />
          <Skeleton className="h-12 w-full rounded-lg" />
        </div>
      )}
      {loadError && (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <StatusMessage tone="error" className="min-w-0 flex-1 break-words">
            {t("settings.cloudSync.security.devicesLoadError", { error: loadError })}
          </StatusMessage>
          <Button size="sm" variant="outline" onClick={load}>
            {t("common.tryAgain")}
          </Button>
        </div>
      )}
      {sessions !== null && (
        <ul className="mt-3 grid">
          {sessions.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center gap-3 border-b border-border py-3 last:border-b-0">
              <div className="min-w-0 flex-1">
                <p className="flex min-w-0 items-center gap-2 text-sm font-medium">
                  <span translate="no" className="truncate" title={s.device_name}>
                    {s.device_name}
                  </span>
                  {s.current && <Badge variant="outline">{t("settings.cloudSync.security.thisDevice")}</Badge>}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t("settings.cloudSync.security.lastActive", { date: formatMs(s.last_seen_at) })}
                </p>
              </div>
              <Button
                size="sm"
                variant="outline"
                disabled={busyId !== null}
                aria-busy={busyId === s.id || undefined}
                aria-label={busyId === s.id ? undefined : t("settings.cloudSync.security.signOutDevice", { name: s.device_name })}
                onClick={() => void signOut(s)}
              >
                {busyId === s.id ? t("settings.cloudSync.security.signingOut") : t("settings.cloudSync.security.signOut")}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <StatusMessage tone={rowError ? "error" : "success"} className="mt-2 break-words">
        {rowError ?? notice}
      </StatusMessage>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("settings.cloudSync.security.signOutOthersTitle")}
        description={t("settings.cloudSync.security.signOutOthersDesc")}
        confirmLabel={t("settings.cloudSync.security.signOutOthersConfirm")}
        pending={confirmPending}
        error={confirmError}
        onConfirm={() => void signOutOthers()}
      />
    </div>
  )
}
