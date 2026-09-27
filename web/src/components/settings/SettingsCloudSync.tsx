import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { Disclosure } from "@/components/Disclosure"
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
  cloudServer,
  cloudSignup,
  cloudStatus,
  cloudSync,
  type CloudStatus,
  type SyncResult,
} from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { isAbsoluteUrl } from "@/lib/url"

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

  // Advanced: cloud server URL (M18). Editable only while logged out.
  const [serverUrl, setServerUrl] = useState("")
  const [serverSaving, setServerSaving] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)
  const [serverNote, setServerNote] = useState<string | null>(null)
  const [serverConfirmOpen, setServerConfirmOpen] = useState(false)
  const serverUrlRef = useRef<HTMLInputElement | null>(null)

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

  // Keep the Advanced input in sync with the server until the user edits it.
  useEffect(() => {
    if (!serverUrl) setServerUrl(status?.api_base_url ?? "")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.api_base_url])

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
    if (!email.trim()) errs.email = "Enter your email."
    else if (!email.includes("@")) errs.email = "Enter an email address like name@example.com."
    if (!password.trim()) errs.password = mode === "signup" ? "Choose a password." : "Enter your password."
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
      return { message: "Email or password is incorrect. Check both, then try again.", field: "password" }
    if (kind === "signup" && code === 409)
      return {
        message: "An account with this email already exists. Log in instead, or use a different email.",
        field: "email",
      }
    if (kind === "recover" && code === 401)
      return { message: "That recovery code doesn’t match this email. Check it, then try again.", field: "recoverCode" }
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
    if (!email.trim()) errs.recoverEmail = "Enter your email."
    if (!recoveryCodeInput.trim()) errs.recoverCode = "Enter your recovery code."
    if (!newPassword.trim()) errs.newPassword = "Enter a new password."
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
        setLogoutError(`Couldn’t log out: ${errorMessage(err)}. Check that CatchM is still running, then try again.`),
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

  const submitServer = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const v = serverUrl.trim()
    setServerNote(null)
    if (!v) {
      setServerError("Enter a server URL, like https://catchm-cloud.example.com.")
      serverUrlRef.current?.focus()
      return
    }
    if (!isAbsoluteUrl(v, ["http", "https"])) {
      setServerError("Enter an absolute https:// URL (plain http:// works only for localhost).")
      serverUrlRef.current?.focus()
      return
    }
    setServerError(null)
    // The server normalizes by trimming trailing slashes. Re-saving the
    // same URL would still clear the stored account, so skip it.
    if (v.replace(/\/+$/, "") === status?.api_base_url) {
      setServerNote("Already using this server.")
      return
    }
    // Nothing to lose without a stored account, so skip the confirm.
    if (accountStored) setServerConfirmOpen(true)
    else void handleSaveServer()
  }

  // Changing server clears the account stored on this device (email and
  // key params; see handleCloudServer), so that case confirms first.
  const handleSaveServer = async () => {
    if (serverSaving) return
    setServerSaving(true)
    setServerError(null)
    try {
      const res = await cloudServer(serverUrl.trim())
      setServerUrl(res.api_base_url)
      setServerNote("Saved")
      setServerConfirmOpen(false)
      refresh()
    } catch (err) {
      // Shown in the dialog (if open) and, after it's dismissed, next to
      // the field.
      setServerError(errorMessage(err))
      if (!serverConfirmOpen) serverUrlRef.current?.focus()
    } finally {
      setServerSaving(false)
    }
  }

  const loggedIn = status?.logged_in === true
  // Older backends don't send account_stored; assume an account is
  // stored so the confirm still guards the change.
  const accountStored = loggedIn || status?.account_stored !== false

  const describedBy = (field: AuthField, errorId: string) =>
    [fieldErrors[field] && errorId, authError?.field === field && AUTH_ERROR_ID].filter(Boolean).join(" ") ||
    undefined

  const loadErrorBlock = loadError && (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
      <StatusMessage tone="error" className="min-w-0 break-words">
        Couldn’t reach CatchM for your cloud status: {loadError}. Check that CatchM is still running, then try again.
      </StatusMessage>
      <Button size="sm" variant="outline" onClick={refresh}>
        Try again
      </Button>
    </div>
  )

  if (status === null) {
    if (loadError) {
      return (
        <SettingsSection title="Cloud sync" description="Encrypted sync of your meetings across devices.">
          {loadErrorBlock}
        </SettingsSection>
      )
    }
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">Loading cloud sync settings…</span>
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
        title="Cloud sync"
        description="Your meetings stay on this device by default. Signing up only turns on encrypted sync across your devices — your meetings are encrypted before they ever leave this computer, and CatchM cannot read them."
      >
        {loadErrorBlock}

        {!loggedIn && mode !== "forgot" && (
          <form noValidate onSubmit={(e) => void handleAuth(e)} className="grid gap-3">
            <SettingRow
              label="Email"
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
              label="Password"
              htmlFor="cloud-password"
              description={mode === "signup" ? "Choose a password for your new CatchM cloud account." : undefined}
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
                  {authBusy === "login" ? "Logging in…" : "Log in"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={authBusy !== null}
                  onClick={() => switchMode("signup")}
                >
                  Create an account
                </Button>
                <Button type="button" variant="link" disabled={authBusy !== null} onClick={() => switchMode("forgot")}>
                  Forgot password?
                </Button>
              </div>
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={authBusy !== null} aria-busy={authBusy === "signup" || undefined}>
                  {authBusy === "signup" ? "Signing up…" : "Sign up"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={authBusy !== null}
                  onClick={() => switchMode("login")}
                >
                  Log in instead
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
              Setting a new password rotates your recovery code — the old code stops working once this succeeds.
            </p>
            <SettingRow
              label="Email"
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
              label="Recovery code"
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
              label="New password"
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
                {authBusy === "recover" ? "Recovering…" : "Recover and set new password"}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={authBusy !== null}
                onClick={() => switchMode("login")}
              >
                Back
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
              <StatusMessage>
                {syncResult && (
                  <>
                    Pushed <span className="tabular-nums">{syncResult.pushed?.length ?? 0}</span> · Pulled{" "}
                    <span className="tabular-nums">{syncResult.pulled?.length ?? 0}</span> · Conflicted{" "}
                    <span className="tabular-nums">{conflicted.length}</span>
                  </>
                )}
              </StatusMessage>
              {conflicted.length > 0 && (
                <div className="grid gap-1">
                  <p>Conflicted meetings (local copies saved as conflict-* files):</p>
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

      <SettingsSection title="Advanced" description="Self-hosting or pointing this install at a different server.">
        <Disclosure label="Cloud server URL">
          <SettingRow
            label="Server"
            htmlFor={loggedIn ? undefined : "cloud-server-url"}
            id="cloud-server-url"
            description={
              loggedIn
                ? "Log out to change server — the session and sync manifest belong to one server."
                : accountStored
                  ? "Changing servers clears the stored account — sign up or log in again afterwards."
                  : "The CatchM cloud server this device signs up and syncs with."
            }
            control={
              loggedIn ? (
                <p translate="no" className="w-96 truncate text-sm text-muted-foreground" title={status.api_base_url}>
                  {status.api_base_url}
                </p>
              ) : (
                <form noValidate onSubmit={submitServer} className="grid w-96 justify-items-end gap-1">
                  <div className="flex w-full gap-2">
                    <Input
                      id="cloud-server-url"
                      ref={serverUrlRef}
                      name="cloud_server_url"
                      type="url"
                      {...noAutofill}
                      value={serverUrl}
                      onChange={(e) => {
                        setServerUrl(e.target.value)
                        setServerError(null)
                        setServerNote(null)
                      }}
                      aria-invalid={serverError ? true : undefined}
                      aria-describedby={
                        serverError ? `${descId("cloud-server-url")} cloud-server-url-error` : descId("cloud-server-url")
                      }
                      className="w-full min-w-0"
                      placeholder="https://catchm-cloud.example.com…"
                    />
                    <Button type="submit" disabled={serverSaving} aria-busy={serverSaving || undefined}>
                      {serverSaving ? "Saving…" : "Save server URL"}
                    </Button>
                  </div>
                  <StatusMessage id="cloud-server-url-error" tone="error" className="break-words">
                    {serverError}
                  </StatusMessage>
                  <StatusMessage>{!serverError && serverNote}</StatusMessage>
                </form>
              )
            }
          />
        </Disclosure>
      </SettingsSection>

      <ConfirmDialog
        open={serverConfirmOpen}
        onOpenChange={setServerConfirmOpen}
        title="Change cloud server?"
        description={
          <>
            CatchM will switch to <span translate="no" className="break-all">{serverUrl.trim()}</span> and forget
            any account stored on this device for the current server. You’ll need to sign up or log in again on
            the new server.
          </>
        }
        confirmLabel="Change server"
        destructive
        pending={serverSaving}
        error={serverError}
        onConfirm={() => void handleSaveServer()}
      />

      <RecoveryCodeDialog code={recoveryCode} onAcked={() => setRecoveryCode(null)} />
    </div>
  )
}
