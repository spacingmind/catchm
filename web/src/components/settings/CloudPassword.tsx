import { useEffect, useRef, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { SettingRow } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { ApiError, cloudChangePassword, cloudRecover } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

type Panel = "change" | "forgot"

/**
 * Settings → Cloud sync → Security → Password, while logged in.
 * "Change password" needs the current one (checked by the server, so a
 * stolen session can't do it) and keeps the recovery code; "Forgot
 * password?" sets a new one with the recovery code, which is then
 * replaced (onRecovered shows the new code once).
 */
export function CloudPassword({
  email,
  onRecovered,
  onSessionEnded,
}: {
  email: string
  onRecovered: (recoveryCode: string, twoFactorDisabled: boolean) => void
  onSessionEnded: (message: string | null) => void
}) {
  const { t } = useTranslation()
  const [panel, setPanel] = useState<Panel | null>(null)
  const [current, setCurrent] = useState("")
  const [recoveryCode, setRecoveryCode] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [signOutOthers, setSignOutOthers] = useState(true)
  const [breached, setBreached] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const firstRef = useRef<HTMLInputElement | null>(null)

  useEffect(() => {
    if (panel) firstRef.current?.focus()
  }, [panel])

  const open = (p: Panel) => {
    setPanel(p)
    setCurrent("")
    setRecoveryCode("")
    setNext("")
    setConfirm("")
    setSignOutOthers(true)
    setBreached(null)
    setError(null)
    setNotice(null)
  }
  const close = () => {
    setPanel(null)
    setCurrent("")
    setRecoveryCode("")
    setNext("")
    setConfirm("")
    setBreached(null)
    setError(null)
  }

  const submit = async (e: FormEvent<HTMLFormElement> | null, acceptBreached = false) => {
    e?.preventDefault()
    if (busy || !panel) return
    const secret = panel === "change" ? current : recoveryCode
    if (!secret.trim() || !next) {
      setError(
        panel === "change" ? t("settings.cloudSync.password.fillChange") : t("settings.cloudSync.password.fillForgot"),
      )
      return
    }
    if (next !== confirm) {
      setError(t("settings.cloudSync.password.mismatch"))
      return
    }
    setBusy(true)
    setError(null)
    try {
      if (panel === "change") {
        const res = await cloudChangePassword(current, next, { signOutOthers, acceptBreached })
        close()
        setNotice(
          res.signed_out > 0
            ? t("settings.cloudSync.password.changedSignedOut", { count: res.signed_out })
            : t("settings.cloudSync.password.changed"),
        )
      } else {
        const res = await cloudRecover(email, recoveryCode.trim(), next)
        close()
        setNotice(t("settings.cloudSync.password.reset"))
        onRecovered(res.recovery_code, res.two_factor_disabled === true)
      }
    } catch (err) {
      if (err instanceof ApiError && err.code === "session_ended") {
        onSessionEnded(errorMessage(err))
        return
      }
      if (err instanceof ApiError && err.code === "password_breached") {
        setBreached(typeof err.body?.count === "number" ? err.body.count : 1)
        return
      }
      if (err instanceof ApiError && err.code === "wrong_password") setError(t("settings.cloudSync.password.wrongCurrent"))
      else if (err instanceof ApiError && err.code === "invalid_recovery_code")
        setError(t("settings.cloudSync.recoverBadCode"))
      else setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const field = (id: string, label: string, value: string, set: (v: string) => void, autoComplete: string, first = false) => (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <Input
        id={id}
        ref={first ? firstRef : undefined}
        type={autoComplete === "off" ? "text" : "password"}
        autoComplete={autoComplete}
        {...(autoComplete === "off" ? noAutofill : {})}
        value={value}
        onChange={(e) => {
          set(e.target.value)
          if (error) setError(null)
          if (breached !== null) setBreached(null)
        }}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? "cloud-password-error" : undefined}
        className="w-72"
      />
    </div>
  )

  return (
    <div className="border-b border-border">
      <div>
        <SettingRow
          label={t("settings.cloudSync.password.label")}
          id="cloud-password"
          description={t("settings.cloudSync.password.desc")}
          control={
            panel === null && (
              <div className="flex flex-wrap items-center justify-end gap-2">
                <Button size="sm" variant="outline" onClick={() => open("change")}>
                  {t("settings.cloudSync.password.change")}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => open("forgot")}>
                  {t("settings.cloudSync.forgotPassword")}
                </Button>
              </div>
            )
          }
        />
      </div>
      <StatusMessage tone="success" className="mb-4 -mt-2">
        {panel === null ? notice : null}
      </StatusMessage>

      {panel !== null && (
        <form noValidate onSubmit={(e) => void submit(e)} className="mb-4 grid gap-4 rounded-lg border border-border p-3">
          <p className="text-sm text-muted-foreground">
            {panel === "change" ? t("settings.cloudSync.password.changeIntro") : t("settings.cloudSync.password.forgotIntro")}
          </p>
          {panel === "change"
            ? field("cloud-password-current", t("settings.cloudSync.password.current"), current, setCurrent, "current-password", true)
            : field("cloud-password-recovery", t("settings.cloudSync.recoveryCodeLabel"), recoveryCode, setRecoveryCode, "off", true)}
          {field("cloud-password-new", t("settings.cloudSync.password.new"), next, setNext, "new-password")}
          {field("cloud-password-confirm", t("settings.cloudSync.password.confirm"), confirm, setConfirm, "new-password")}
          {panel === "change" && (
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={signOutOthers} onCheckedChange={(c) => setSignOutOthers(c === true)} />
              {t("settings.cloudSync.password.signOutOthers")}
            </label>
          )}
          {breached !== null && (
            <div className="grid gap-2 rounded-lg border border-border p-3">
              <p className="text-sm font-medium text-warning">{t("settings.cloudSync.breachedWarning", { count: breached })}</p>
              <div className="flex flex-wrap gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setBreached(null)}>
                  {t("settings.cloudSync.breachedChooseAnother")}
                </Button>
                <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void submit(null, true)}>
                  {t("settings.cloudSync.breachedUseAnyway")}
                </Button>
              </div>
            </div>
          )}
          <StatusMessage id="cloud-password-error" tone="error">
            {error}
          </StatusMessage>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" size="sm" disabled={busy || breached !== null} aria-busy={busy || undefined}>
              {busy
                ? t("settings.cloudSync.password.saving")
                : panel === "change"
                  ? t("settings.cloudSync.password.change")
                  : t("settings.cloudSync.recoverButton")}
            </Button>
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={close}>
              {t("common.cancel")}
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}
