import { useEffect, useState, type FormEvent } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
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
import { cloudLogin, cloudLogout, cloudSignup, cloudStatus, cloudSync, type CloudStatus, type SyncResult } from "@/lib/api"

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

export function SettingsCloud() {
  const [status, setStatus] = useState<CloudStatus | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [authBusy, setAuthBusy] = useState<"signup" | "login" | null>(null)
  const [authError, setAuthError] = useState<string | null>(null)

  // Recovery code lives ONLY here, only until the user acknowledges the
  // dialog (or the component unmounts). Never localStorage, never
  // refetchable — the server returns it exactly once.
  const [recoveryCode, setRecoveryCode] = useState<string | null>(null)

  const [syncing, setSyncing] = useState(false)
  const [syncResult, setSyncResult] = useState<SyncResult | null>(null)
  const [syncError, setSyncError] = useState<string | null>(null)

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

  const handleAuth = async (mode: "signup" | "login") => {
    setAuthBusy(mode)
    setAuthError(null)
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
      setSyncResult(await cloudSync())
      refresh()
    } catch (err) {
      setSyncResult(null)
      setSyncError(err instanceof Error ? err.message : String(err))
    } finally {
      setSyncing(false)
    }
  }

  const loggedIn = status?.logged_in === true

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Cloud</h1>

      {loadError && <p className="mb-4 text-sm text-destructive">{loadError}</p>}

      {status && !loggedIn && (
        <Card>
          <CardHeader>
            <CardTitle>Account</CardTitle>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={(e: FormEvent<HTMLFormElement>) => {
                e.preventDefault()
                handleAuth("login")
              }}
              className="grid gap-3"
            >
              <div className="grid gap-1.5">
                <Label htmlFor="cloud-email">Email</Label>
                <Input
                  id="cloud-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="cloud-password">Password</Label>
                <Input
                  id="cloud-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
              </div>
              <div className="flex gap-2">
                <Button
                  type="submit"
                  className="w-fit"
                  disabled={authBusy !== null || !email.trim() || !password.trim()}
                >
                  {authBusy === "login" ? "Logging in…" : "Log in"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="w-fit"
                  disabled={authBusy !== null || !email.trim() || !password.trim()}
                  onClick={() => handleAuth("signup")}
                >
                  {authBusy === "signup" ? "Signing up…" : "Sign up"}
                </Button>
              </div>
            </form>
            {authError && <p className="mt-3 text-sm text-destructive">{authError}</p>}
          </CardContent>
        </Card>
      )}

      {status && loggedIn && (
        <div className="grid gap-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>{status.email}</CardTitle>
              <Button size="sm" variant="outline" className="w-fit" onClick={handleLogout}>
                Log out
              </Button>
            </CardHeader>
            <CardContent className="grid gap-2">
              <p className="text-sm text-muted-foreground">Logged in {formatWhen(status.logged_in_at)}</p>
              <p className="text-sm text-muted-foreground">Last sync {formatWhen(status.last_sync_at)}</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Sync</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3">
              <Button className="w-fit" onClick={handleSync} disabled={syncing}>
                {syncing ? "Syncing…" : "Sync now"}
              </Button>
              {syncError && <p className="text-sm text-destructive">{syncError}</p>}
              {syncResult && (
                <div className="grid gap-1 text-sm text-muted-foreground">
                  <p>
                    Pushed {syncResult.pushed?.length ?? 0} · Pulled {syncResult.pulled?.length ?? 0} · Conflicted{" "}
                    {syncResult.conflicted?.length ?? 0}
                  </p>
                  {(syncResult.conflicted?.length ?? 0) > 0 && (
                    <div className="grid gap-1">
                      <p>Conflicted meetings (local copies saved as conflict-* files):</p>
                      {syncResult.conflicted?.map((id) => (
                        <p key={id} className="font-mono text-xs">
                          {id}
                        </p>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      <Dialog
        open={recoveryCode !== null}
        onOpenChange={(open) => {
          // The only way out is the explicit acknowledgment button.
          if (!open) return
        }}
      >
        <DialogContent className="sm:max-w-md" onPointerDownOutside={(e) => e.preventDefault()}>
          <DialogHeader>
            <DialogTitle>Save your recovery code</DialogTitle>
            <DialogDescription>
              This code is shown only once. If you ever lose your password and have no linked device, it is the only way
              to recover your meetings. Write it down or store it in a password manager now.
            </DialogDescription>
          </DialogHeader>
          <p className="select-all rounded-md bg-muted px-4 py-3 text-center font-mono text-lg tracking-wider break-all">
            {recoveryCode}
          </p>
          <DialogFooter>
            <Button
              type="button"
              onClick={() => {
                void navigator.clipboard?.writeText(recoveryCode ?? "")
              }}
              variant="outline"
            >
              Copy
            </Button>
            <Button type="button" onClick={() => setRecoveryCode(null)}>
              I've saved my recovery code
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
