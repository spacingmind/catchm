import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"
import { useTranslation } from "react-i18next"

import { RecoveryCodeDialog } from "@/components/RecoveryCodeDialog"
import { StatusMessage } from "@/components/StatusMessage"
import { descId, SettingRow, SettingsSection } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { notifyCloudSyncConflict } from "@/lib/wailsRuntime"
import {
  ApiError,
  cloudLogin,
  cloudLogout,
  cloudRecover,
  cloudSignup,
  cloudStatus,
  cloudSync,
  type CloudStatus,
  type SyncResult,
} from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { pluralize } from "@/lib/utils"

const STATUS_POLL_MS = 30_000

/** Zero time as marshaled by Go's time.Time — means "never synced". */
const ZERO_TIME = "0001-01-01T00:00:00Z"

/** Formatted timestamp, or null when unset / Go's zero time. */
function formatWhen(iso: string | undefined): string | null {
  if (!iso || iso === ZERO_TIME) return null
  return formatDateTime(iso)
}

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Attributes shared by every email field (login, signup, recovery). */
const emailInputProps = {
  type: "email",
  name: "email",
  autoComplete: "username",
  spellCheck: false,
  autoCapitalize: "off",
} as const

type AuthField = "email" | "password" | "recoverEmail" | "recoverCode" | "newPassword"

const AUTH_ERROR_ID = "cloud-auth-error"

export function SettingsCloudSync() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<CloudStatus | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<AuthField, string>>>({})
  const emailRef = useRef<HTMLInputElement | null>(null)
  const passwordRef = useRef<HTMLInputElement | null>(null)
  const recoverEmailRef = useRef<HTMLInputElement | null>(null)
  const recoverCodeRef = useRef<HTMLInputElement | null>(null)
  const newPasswordRef = useRef<HTMLInputElement | null>(null)
  const fieldRefs: Record<AuthField, RefObject<HTMLInputElement | null>> = {
    email: emailRef,
    password: passwordRef,
    recoverEmail: recoverEmailRef,
    recoverCode: recoverCodeRef,
    newPassword: newPasswordRef,
  }
  const [authBusy, setAuthBusy] = useState<"signup" | "login" | "recover" | null>(null)
  // A server-side auth failure; `field` is the input it's about (focused
  // and described by the message), when there is one.
  const [authError, setAuthError] = useState<{ message: string; field?: AuthField } | null>(null)

  // Log in and sign up are separate modes so the password field carries
  // the right autocomplete (current- vs new-password) and each is a real
  // form submit. "Forgot password?" is its own form with separate fields
  // so a failed attempt never clears what the user typed.
  const [mode, setMode] = useState<"login" | "signup" | "forgot">("login")
  const [recoveryCodeInput, setRecoveryCodeInput] = useState("")
  const [newPassword, setNewPassword] = useState("")

  // Recovery code lives ONLY here, only until the user acknowledges the
  // dialog (or the component unmounts). Never localStorage, never
  // refetchable — the server returns it exactly once.
  const [recoveryCode, setRecoveryCode] = useState<string | null>(null)

  const [loggingOut, setLoggingOut] = useState(false)
  const [logoutError, setLogoutError] = useState<string | null>(null)

  const [syncing, setSyncing] = useState(false)
  const [syncResult, setSyncResult] = useState<SyncResult | null>(null)
  const [syncError, setSyncError] = useState<string | null>(null)

  const refresh = () => {
    cloudStatus()
      .then((s) => {
        setStatus(s)
        setLoadError(null)
      })
      .catch((err: unknown) => setLoadError(errorMessage(err)))
  }

  useEffect(() => {
    refresh()
    const timer = setInterval(refresh, STATUS_POLL_MS)
    return () => clearInterval(timer)
  }, [])

  // Move focus to the field a server-side auth error is about, once it
  // has rendered with aria-invalid/aria-describedby.
  useEffect(() => {
    if (authError?.field) fieldRefs[authError.field].current?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authError])

  const switchMode = (next: typeof mode) => {
    setMode(next)
    setAuthError(null)
    setFieldErrors({})
  }

  const validateAuthFields = () => {
    const errs: Partial<Record<AuthField, string>> = {}
    if (!email.trim()) errs.email = t("settings.cloudSync.emailRequired")
    else if (!email.includes("@")) errs.email = t("settings.cloudSync.emailInvalid")
    if (!password.trim()) errs.password = mode === "signup" ? t("settings.cloudSync.passwordNew") : t("settings.cloudSync.passwordRequired")
    setFieldErrors(errs)
    if (errs.email) emailRef.current?.focus()
    else if (errs.password) passwordRef.current?.focus()
    return Object.keys(errs).length === 0
  }

  /** Turns a failed auth request into copy with a next step. */
  const describeAuthError = (
    err: unknown,
    kind: "signup" | "login" | "recover",
  ): { message: string; field?: AuthField } => {
    const code = err instanceof ApiError ? err.status : 0
    if (kind === "login" && code === 401)
      return { message: t("settings.cloudSync.loginBadCredentials"), field: "password" }
    if (kind === "signup" && code === 409)
      return {
        message: t("settings.cloudSync.signupExists"),
        field: "email",
      }
    if (kind === "recover" && code === 401)
      return { message: t("settings.cloudSync.recoverBadCode"), field: "recoverCode" }
    return { message: errorMessage(err) }
  }

  const handleAuth = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (authBusy !== null) return
    if (!validateAuthFields()) return
    const kind = mode === "signup" ? "signup" : "login"
    setAuthBusy(kind)
    setAuthError(null)
    try {
      if (kind === "signup") {
        const res = await cloudSignup(email, password)
        setRecoveryCode(res.recovery_code)
      } else {
        await cloudLogin(email, password)
      }
      setPassword("")
      setMode("login")
      refresh()
    } catch (err) {
      setAuthError(describeAuthError(err, kind))
    } finally {
      setAuthBusy(null)
    }
  }

  const handleRecover = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (authBusy !== null) return
    const errs: Partial<Record<AuthField, string>> = {}
    if (!email.trim()) errs.recoverEmail = t("settings.cloudSync.emailRequired")
    if (!recoveryCodeInput.trim()) errs.recoverCode = t("settings.cloudSync.recoverCodeRequired")
    if (!newPassword.trim()) errs.newPassword = t("settings.cloudSync.newPasswordRequired")
    setFieldErrors(errs)
    if (errs.recoverEmail) recoverEmailRef.current?.focus()
    else if (errs.recoverCode) recoverCodeRef.current?.focus()
    else if (errs.newPassword) newPasswordRef.current?.focus()
    if (Object.keys(errs).length > 0) return
    setAuthBusy("recover")
    setAuthError(null)
    try {
      const res = await cloudRecover(email, recoveryCodeInput, newPassword)
      setRecoveryCode(res.recovery_code)
      setRecoveryCodeInput("")
      setNewPassword("")
      setMode("login")
      refresh()
    } catch (err) {
      // Wrong code: inline error, typed values stay put.
      setAuthError(describeAuthError(err, "recover"))
    } finally {
      setAuthBusy(null)
    }
  }

  const handleLogout = () => {
    if (loggingOut) return
    setLoggingOut(true)
    setLogoutError(null)
    cloudLogout()
      .then(() => {
        setSyncResult(null)
        setSyncError(null)
        refresh()
      })
      .catch((err: unknown) =>
        setLogoutError(t("settings.cloudSync.logoutError", { error: errorMessage(err) })),
      )
      .finally(() => setLoggingOut(false))
  }

  const handleSync = async () => {
    setSyncing(true)
    setSyncError(null)
    try {
      const result = await cloudSync()
      setSyncResult(result)
      // M22 §5: no background sync loop exists to poll for this (see
      // desktop_settings.go's NotifyCloudSyncConflict doc comment) — a
      // toast fires right here, where the conflict is already detected.
      notifyCloudSyncConflict(result.conflicted?.length ?? 0)
      refresh()
    } catch (err) {
      setSyncResult(null)
      setSyncError(errorMessage(err))
    } finally {
      setSyncing(false)
    }
  }

  const loggedIn = status?.logged_in === true

  const describedBy = (field: AuthField, errorId: string) =>
    [fieldErrors[field] && errorId, authError?.field === field && AUTH_ERROR_ID].filter(Boolean).join(" ") ||
    undefined

  const loadErrorBlock = loadError && (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
      <StatusMessage tone="error" className="min-w-0 break-words">
        {t("settings.cloudSync.loadError", { error: loadError })}
      </StatusMessage>
      <Button size="sm" variant="outline" onClick={refresh}>
        {t("common.tryAgain")}
      </Button>
    </div>
  )

  if (status === null) {
    if (loadError) {
      return (
        <SettingsSection title={t("settings.nav.cloudSync")} description={t("settings.cloudSync.desc")}>
          {loadErrorBlock}
        </SettingsSection>
      )
    }
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">{t("settings.cloudSync.loading")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
      </div>
    )
  }

  const loggedInAt = formatWhen(status.logged_in_at)
  const lastSyncAt = formatWhen(status.last_sync_at)
  const conflicted = syncResult?.conflicted ?? []

  return (
    <div>
      <SettingsSection
        title={t("settings.nav.cloudSync")}
        description={
          <>
            {t("settings.cloudSync.desc")}
            <span className="mt-1 block">{t("settings.cloudSync.summaryNote")}</span>
          </>
        }
      >
        {loadErrorBlock}

        {!loggedIn && mode !== "forgot" && (
          <form noValidate onSubmit={(e) => void handleAuth(e)} className="grid gap-3">
            <SettingRow
              label={t("settings.cloudSync.emailLabel")}
              htmlFor="cloud-email"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="cloud-email"
                    ref={emailRef}
                    {...emailInputProps}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="name@example.com…"
                    aria-invalid={fieldErrors.email || authError?.field === "email" ? true : undefined}
                    aria-describedby={describedBy("email", "cloud-email-error")}
                    className="w-full"
                  />
                  <StatusMessage id="cloud-email-error" tone="error">
                    {fieldErrors.email}
                  </StatusMessage>
                </div>
              }
            />
            <SettingRow
              label={t("settings.cloudSync.passwordLabel")}
              htmlFor="cloud-password"
              description={mode === "signup" ? t("settings.cloudSync.passwordDesc") : undefined}
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="cloud-password"
                    ref={passwordRef}
                    type="password"
                    name={mode === "signup" ? "new-password" : "password"}
                    autoComplete={mode === "signup" ? "new-password" : "current-password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-invalid={fieldErrors.password || authError?.field === "password" ? true : undefined}
                    aria-describedby={
                      [mode === "signup" && descId("cloud-password"), describedBy("password", "cloud-password-error")]
                        .filter(Boolean)
                        .join(" ") || undefined
                    }
                    className="w-full"
                  />
                  <StatusMessage id="cloud-password-error" tone="error">
                    {fieldErrors.password}
                  </StatusMessage>
                </div>
              }
            />
            {mode === "login" ? (
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={authBusy !== null} aria-busy={authBusy === "login" || undefined}>
                  {authBusy === "login" ? t("settings.cloudSync.loggingIn") : t("settings.cloudSync.logIn")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={authBusy !== null}
                  onClick={() => switchMode("signup")}
                >
                  {t("settings.cloudSync.createAccount")}
                </Button>
                <Button type="button" variant="link" disabled={authBusy !== null} onClick={() => switchMode("forgot")}>
                  {t("settings.cloudSync.forgotPassword")}
                </Button>
              </div>
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={authBusy !== null} aria-busy={authBusy === "signup" || undefined}>
                  {authBusy === "signup" ? t("settings.cloudSync.signingUp") : t("settings.cloudSync.signUp")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={authBusy !== null}
                  onClick={() => switchMode("login")}
                >
                  {t("settings.cloudSync.logInInstead")}
                </Button>
              </div>
            )}
            <StatusMessage id={AUTH_ERROR_ID} tone="error" className="break-words">
              {authError?.message}
            </StatusMessage>
          </form>
        )}

        {!loggedIn && mode === "forgot" && (
          <form noValidate onSubmit={(e) => void handleRecover(e)} className="grid gap-3">
            <p className="text-sm text-muted-foreground">
              {t("settings.cloudSync.recoverIntro")}
            </p>
            <SettingRow
              label={t("settings.cloudSync.emailLabel")}
              htmlFor="recover-email"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="recover-email"
                    ref={recoverEmailRef}
                    {...emailInputProps}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="name@example.com…"
                    aria-invalid={fieldErrors.recoverEmail ? true : undefined}
                    aria-describedby={describedBy("recoverEmail", "recover-email-error")}
                    className="w-full"
                  />
                  <StatusMessage id="recover-email-error" tone="error">
                    {fieldErrors.recoverEmail}
                  </StatusMessage>
                </div>
              }
            />
            <SettingRow
              label={t("settings.cloudSync.recoveryCodeLabel")}
              htmlFor="recover-code"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="recover-code"
                    ref={recoverCodeRef}
                    name="recovery_code"
                    autoComplete="off"
                    autoCapitalize="characters"
                    spellCheck={false}
                    translate="no"
                    value={recoveryCodeInput}
                    onChange={(e) => setRecoveryCodeInput(e.target.value)}
                    placeholder="XXXXX-XXXXX-XXXXX-…"
                    aria-invalid={fieldErrors.recoverCode || authError?.field === "recoverCode" ? true : undefined}
                    aria-describedby={describedBy("recoverCode", "recover-code-error")}
                    className="w-full font-mono"
                  />
                  <StatusMessage id="recover-code-error" tone="error">
                    {fieldErrors.recoverCode}
                  </StatusMessage>
                </div>
              }
            />
            <SettingRow
              label={t("settings.cloudSync.newPasswordLabel")}
              htmlFor="recover-new-password"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="recover-new-password"
                    ref={newPasswordRef}
                    type="password"
                    name="new-password"
                    autoComplete="new-password"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    aria-invalid={fieldErrors.newPassword ? true : undefined}
                    aria-describedby={describedBy("newPassword", "recover-new-password-error")}
                    className="w-full"
                  />
                  <StatusMessage id="recover-new-password-error" tone="error">
                    {fieldErrors.newPassword}
                  </StatusMessage>
                </div>
              }
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={authBusy !== null} aria-busy={authBusy === "recover" || undefined}>
                {authBusy === "recover" ? t("settings.cloudSync.recovering") : t("settings.cloudSync.recoverButton")}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={authBusy !== null}
                onClick={() => switchMode("login")}
              >
                {t("common.back")}
              </Button>
            </div>
            <StatusMessage id={AUTH_ERROR_ID} tone="error" className="break-words">
              {authError?.message}
            </StatusMessage>
          </form>
        )}

        {loggedIn && (
          <div className="grid gap-1">
            <SettingRow
              label="Account"
              description={loggedInAt ? `Logged in on ${loggedInAt}.` : "Logged in."}
              control={
                <div className="grid justify-items-end gap-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span translate="no" className="min-w-0 truncate text-sm font-medium" title={status.email}>
                      {status.email}
                    </span>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleLogout}
                      disabled={loggingOut}
                      aria-busy={loggingOut || undefined}
                    >
                      {loggingOut ? "Logging out…" : "Log out"}
                    </Button>
                  </div>
                  <StatusMessage tone="error" className="max-w-96 break-words">
                    {logoutError}
                  </StatusMessage>
                </div>
              }
            />
            <SettingRow
              label="Last sync"
              description={lastSyncAt ?? "Never"}
              control={
                <Button onClick={() => void handleSync()} disabled={syncing} aria-busy={syncing || undefined}>
                  {syncing ? "Syncing…" : "Sync now"}
                </Button>
              }
            />
            <div className={syncError || syncResult ? "grid gap-1 pt-2 text-sm text-muted-foreground" : undefined}>
              <StatusMessage tone="error" className="break-words">
                {syncError && `Couldn’t sync: ${syncError}. Check your connection, then sync again.`}
              </StatusMessage>
              <StatusMessage className="tabular-nums">
                {syncResult && (
                  <>
                    Uploaded {pluralize(syncResult.pushed?.length ?? 0, "meeting")} · Downloaded{" "}
                    {pluralize(syncResult.pulled?.length ?? 0, "meeting")}
                    {conflicted.length > 0 && ` · ${pluralize(conflicted.length, "meeting")} changed on two devices`}
                  </>
                )}
              </StatusMessage>
              {conflicted.length > 0 && (
                <div className="grid gap-1">
                  <p>
                    These meetings were changed on this computer and on another device. Both versions were kept:
                    the meeting now shows the other device’s version, and the version from this computer is saved
                    as a backup file in the meeting’s folder. The backup doesn’t appear in your meeting list.
                  </p>
                  <ul className="grid gap-1">
                    {conflicted.map((id) => (
                      <li key={id}>
                        <code translate="no" className="font-mono text-xs break-all">
                          {id}
                        </code>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          </div>
        )}
      </SettingsSection>

      <RecoveryCodeDialog code={recoveryCode} onAcked={() => setRecoveryCode(null)} />
    </div>
  )
}
