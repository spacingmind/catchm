import { useEffect, useRef, useState, type FormEvent } from "react"
import { ChevronDown } from "lucide-react"
import { cn } from "cn"

import { RecoveryCodeDialog } from "@/components/RecoveryCodeDialog"
import { SettingRow, SettingsSection } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { notifyCloudSyncConflict } from "@/lib/wailsRuntime"
import {
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

const STATUS_POLL_MS = 30_000

/** Zero time as marshaled by Go's time.Time — means "never synced". */
const ZERO_TIME = "0001-01-01T00:00:00Z"

function formatWhen(iso: string | undefined): string {
  if (!iso || iso === ZERO_TIME) return "never"
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function SettingsCloudSync() {
  const [status, setStatus] = useState<CloudStatus | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [fieldErrors, setFieldErrors] = useState<{
    email?: string
    password?: string
    recoverEmail?: string
    recoverCode?: string
    newPassword?: string
  }>({})
  const emailRef = useRef<HTMLInputElement | null>(null)
  const passwordRef = useRef<HTMLInputElement | null>(null)
  const recoverEmailRef = useRef<HTMLInputElement | null>(null)
  const recoverCodeRef = useRef<HTMLInputElement | null>(null)
  const newPasswordRef = useRef<HTMLInputElement | null>(null)
  const [authBusy, setAuthBusy] = useState<"signup" | "login" | "recover" | null>(null)
  const [authError, setAuthError] = useState<string | null>(null)

  // "Forgot password?" recovery form: separate fields so a failed
  // attempt never clears what the user typed.
  const [mode, setMode] = useState<"auth" | "forgot">("auth")
  const [recoveryCodeInput, setRecoveryCodeInput] = useState("")
  const [newPassword, setNewPassword] = useState("")

  // Recovery code lives ONLY here, only until the user acknowledges the
  // dialog (or the component unmounts). Never localStorage, never
  // refetchable — the server returns it exactly once.
  const [recoveryCode, setRecoveryCode] = useState<string | null>(null)

  const [syncing, setSyncing] = useState(false)
  const [syncResult, setSyncResult] = useState<SyncResult | null>(null)
  const [syncError, setSyncError] = useState<string | null>(null)

  // Advanced: cloud server URL (M18). Editable only while logged out.
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [serverUrl, setServerUrl] = useState("")
  const [serverSaving, setServerSaving] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)
  const [serverSaved, setServerSaved] = useState(false)

  const refresh = () => {
    cloudStatus()
      .then((s) => {
        setStatus(s)
        setLoadError(null)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
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

  const validateAuthFields = (needPassword: boolean) => {
    const errs: { email?: string; password?: string } = {}
    if (!email.trim()) errs.email = "Enter your email."
    else if (!email.includes("@")) errs.email = "That doesn't look like an email address."
    if (needPassword && !password.trim()) errs.password = "Enter your password."
    setFieldErrors(errs)
    if (errs.email) emailRef.current?.focus()
    else if (errs.password) passwordRef.current?.focus()
    return Object.keys(errs).length === 0
  }

  const handleAuth = async (mode: "signup" | "login", fromButton = false) => {
    if (authBusy !== null) return
    // The login form submits (Enter); the signup button goes through the
    // same validation first.
    if (fromButton && !validateAuthFields(true)) return
    setAuthBusy(mode)
    setAuthError(null)
    setFieldErrors({})
    try {
      if (mode === "signup") {
        const res = await cloudSignup(email, password)
        setRecoveryCode(res.recovery_code)
      } else {
        await cloudLogin(email, password)
      }
      setPassword("")
      refresh()
    } catch (err) {
      setAuthError(err instanceof Error ? err.message : String(err))
    } finally {
      setAuthBusy(null)
    }
  }

  const handleRecover = async () => {
    if (authBusy !== null) return
    const errs: typeof fieldErrors = {}
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
      setMode("auth")
      refresh()
    } catch (err) {
      // Wrong code: inline error, typed values stay put.
      setAuthError(err instanceof Error ? err.message : String(err))
    } finally {
      setAuthBusy(null)
    }
  }

  const handleLogout = () => {
    cloudLogout()
      .then(() => {
        setSyncResult(null)
        setSyncError(null)
        refresh()
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
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
      setSyncError(err instanceof Error ? err.message : String(err))
    } finally {
      setSyncing(false)
    }
  }

  const handleSaveServer = async () => {
    setServerSaving(true)
    setServerError(null)
    setServerSaved(false)
    try {
      const res = await cloudServer(serverUrl.trim())
      setServerUrl(res.api_base_url)
      setServerSaved(true)
      refresh()
    } catch (err) {
      setServerError(err instanceof Error ? err.message : String(err))
    } finally {
      setServerSaving(false)
    }
  }

  const loggedIn = status?.logged_in === true

  if (status === null) {
    return (
      <div aria-busy="true" className="grid gap-4">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
      </div>
    )
  }

  return (
    <div>
      <SettingsSection
        title="Cloud sync"
        description="Your meetings stay on this device by default. Signing up only turns on encrypted sync across your devices — your meetings are encrypted before they ever leave this computer, and CatchM cannot read them."
      >
        {loadError && <p className="mb-4 text-sm text-destructive">{loadError}</p>}

        {!loggedIn && mode === "auth" && (
          <form
            onSubmit={(e: FormEvent<HTMLFormElement>) => {
              e.preventDefault()
              if (!validateAuthFields(true)) return
              void handleAuth("login")
            }}
            className="grid gap-3"
          >
            <SettingRow
              label="Email"
              htmlFor="cloud-email"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="cloud-email"
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    aria-invalid={fieldErrors.email ? true : undefined}
                    aria-describedby={fieldErrors.email ? "cloud-email-error" : undefined}
                    className="w-full"
                    ref={emailRef}
                  />
                  {fieldErrors.email && (
                    <p id="cloud-email-error" className="text-sm text-destructive">
                      {fieldErrors.email}
                    </p>
                  )}
                </div>
              }
            />
            <SettingRow
              label="Password"
              htmlFor="cloud-password"
              control={
                <div className="grid w-72 justify-items-end gap-1">
                  <Input
                    id="cloud-password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    aria-invalid={fieldErrors.password ? true : undefined}
                    aria-describedby={fieldErrors.password ? "cloud-password-error" : undefined}
                    className="w-full"
                    ref={passwordRef}
                  />
                  {fieldErrors.password && (
                    <p id="cloud-password-error" className="text-sm text-destructive">
                      {fieldErrors.password}
                    </p>
                  )}
                </div>
              }
            />
            <div className="flex items-center gap-2">
              <Button
                type="submit"
                disabled={authBusy !== null}
              >
                {authBusy === "login" ? "Logging in…" : "Log in"}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={authBusy !== null}
                onClick={() => void handleAuth("signup", true)}
              >
                {authBusy === "signup" ? "Signing up…" : "Sign up"}
              </Button>
              <button
                type="button"
                className="rounded-md text-sm text-muted-foreground underline underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                onClick={() => {
                  setMode("forgot")
                  setAuthError(null)
                }}
              >
                Forgot password?
              </button>
            </div>
            {authError && <p className="mt-3 text-sm text-destructive">{authError}</p>}
          </form>
        )}

        {!loggedIn && mode === "forgot" && (
          <form
            onSubmit={(e: FormEvent<HTMLFormElement>) => {
              e.preventDefault()
              void handleRecover()
            }}
            className="grid gap-3"
          >
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
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    aria-invalid={fieldErrors.recoverEmail ? true : undefined}
                    aria-describedby={fieldErrors.recoverEmail ? "recover-email-error" : undefined}
                    className="w-full"
                  />
                  {fieldErrors.recoverEmail && (
                    <p id="recover-email-error" className="text-sm text-destructive">
                      {fieldErrors.recoverEmail}
                    </p>
                  )}
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
                    autoComplete="one-time-code"
                    spellCheck={false}
                    value={recoveryCodeInput}
                    onChange={(e) => setRecoveryCodeInput(e.target.value)}
                    placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXX"
                    aria-invalid={fieldErrors.recoverCode ? true : undefined}
                    aria-describedby={fieldErrors.recoverCode ? "recover-code-error" : undefined}
                    className="w-full font-mono"
                  />
                  {fieldErrors.recoverCode && (
                    <p id="recover-code-error" className="text-sm text-destructive">
                      {fieldErrors.recoverCode}
                    </p>
                  )}
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
                    autoComplete="new-password"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    aria-invalid={fieldErrors.newPassword ? true : undefined}
                    aria-describedby={fieldErrors.newPassword ? "recover-new-password-error" : undefined}
                    className="w-full"
                  />
                  {fieldErrors.newPassword && (
                    <p id="recover-new-password-error" className="text-sm text-destructive">
                      {fieldErrors.newPassword}
                    </p>
                  )}
                </div>
              }
            />
            <div className="flex items-center gap-2">
              <Button type="submit" disabled={authBusy !== null}>
                {authBusy === "recover" ? "Recovering…" : "Recover and set new password"}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={authBusy !== null}
                onClick={() => {
                  setMode("auth")
                  setAuthError(null)
                  setFieldErrors({})
                }}
              >
                Back
              </Button>
            </div>
            {authError && <p className="mt-3 text-sm text-destructive">{authError}</p>}
          </form>
        )}

        {loggedIn && (
          <div className="grid gap-1">
            <SettingRow
              label="Account"
              description={`Logged in ${formatWhen(status.logged_in_at)}.`}
              control={
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{status.email}</span>
                  <Button size="sm" variant="outline" onClick={handleLogout}>
                    Log out
                  </Button>
                </div>
              }
            />
            <SettingRow
              label="Last sync"
              description={formatWhen(status.last_sync_at)}
              control={
                <Button onClick={handleSync} disabled={syncing}>
                  {syncing ? "Syncing…" : "Sync now"}
                </Button>
              }
            />
            {(syncError || syncResult) && (
              <div className="grid gap-1 pt-2 text-sm text-muted-foreground">
                {syncError && <p className="text-destructive">{syncError}</p>}
                {syncResult && (
                  <p>
                    Pushed {syncResult.pushed?.length ?? 0} · Pulled {syncResult.pulled?.length ?? 0} · Conflicted{" "}
                    {syncResult.conflicted?.length ?? 0}
                  </p>
                )}
                {(syncResult?.conflicted?.length ?? 0) > 0 && (
                  <div className="grid gap-1">
                    <p>Conflicted meetings (local copies saved as conflict-* files):</p>
                    {syncResult?.conflicted?.map((id) => (
                      <p key={id} className="font-mono text-xs">
                        {id}
                      </p>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </SettingsSection>

      <SettingsSection title="Advanced" description="Self-hosting or pointing this install at a different server.">
        <button
          type="button"
          onClick={() => setAdvancedOpen((o) => !o)}
          aria-expanded={advancedOpen}
          className="flex w-full items-center gap-2 rounded-md py-2 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <ChevronDown className={cn("size-4 transition-transform", advancedOpen && "rotate-180")} />
          {advancedOpen ? "Hide" : "Show"} cloud server URL
        </button>

        {advancedOpen && (
          <SettingRow
            label="Server"
            htmlFor="cloud-server-url"
            description={
              loggedIn
                ? "Log out to change server — the session and sync manifest belong to one server."
                : "Changing servers clears the stored account — sign up or log in again afterwards."
            }
            control={
              loggedIn ? (
                <p className="w-96 truncate text-sm text-muted-foreground" title={status.api_base_url}>
                  {status.api_base_url}
                </p>
              ) : (
                <div className="grid w-96 justify-items-end gap-1">
                  <div className="flex w-full gap-2">
                    <Input
                      id="cloud-server-url"
                      type="url"
                      value={serverUrl}
                      onChange={(e) => {
                        setServerUrl(e.target.value)
                        setServerError(null)
                        setServerSaved(false)
                      }}
                      aria-invalid={serverError ? true : undefined}
                      className="w-full"
                      placeholder="https://catchm-cloud.example.com"
                    />
                    <Button
                      onClick={() => void handleSaveServer()}
                      disabled={serverSaving || !serverUrl.trim()}
                    >
                      {serverSaving ? "Saving…" : "Save"}
                    </Button>
                  </div>
                  {serverError && <p className="text-sm text-destructive">{serverError}</p>}
                  {serverSaved && !serverError && <p className="text-sm text-muted-foreground">Saved</p>}
                </div>
              )
            }
          />
        )}
      </SettingsSection>

      <RecoveryCodeDialog code={recoveryCode} onAcked={() => setRecoveryCode(null)} />
    </div>
  )
}
