import { useEffect, useRef, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { SettingRow, descId } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ApiError, type CloudChallenge } from "@/lib/api"

/** Attributes for every one-time-code field (email codes, authenticator
 *  codes): the OS can fill an emailed code, and phones get digits. */
// oxlint-disable-next-line react/only-export-components
export const oneTimeCodeProps = {
  type: "text",
  inputMode: "numeric",
  autoComplete: "one-time-code",
  pattern: "[0-9 ]*",
  maxLength: 8,
  spellCheck: false,
  translate: "no",
} as const

/** Seconds left until `until` (ms epoch), ticking once a second. */
function useSecondsLeft(until: number | null): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (until === null) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [until])
  return until === null ? 0 : Math.max(0, Math.ceil((until - now) / 1000))
}

/** The server allows one new email code a minute. */
const RESEND_COOLDOWN_S = 60

/**
 * The "enter the code" step of logging in, signing up or recovering:
 * an emailed verification code (with "Send a new code") or a code from
 * an authenticator app. The parent owns the request: onSubmit sends the
 * code, and `error` is the server's answer about it.
 */
export function CloudCodeStep({
  challenge,
  email,
  busy,
  error,
  onSubmit,
  onResend,
  resentMessage,
  onBack,
  active = true,
}: {
  challenge: CloudChallenge
  email: string
  busy: boolean
  /** A server-side failure about the code (wrong, expired). */
  error: string | null
  onSubmit: (code: string) => void
  /** Email codes only: ask for a new one. Rejects with the ApiError. */
  onResend?: () => Promise<void>
  /** What a successful resend says (defaults to "We sent a new code…"). */
  resentMessage?: string
  onBack: () => void
  /** False while something modal covers the step (the recovery-code
   *  dialog after signup); the field takes focus when it turns true. */
  active?: boolean
}) {
  const { t } = useTranslation()
  const [code, setCode] = useState("")
  const [fieldError, setFieldError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)

  // A code just went out when this step opens, so a resend waits a minute.
  const [cooldownUntil, setCooldownUntil] = useState<number | null>(() =>
    challenge === "email_code" ? Date.now() + RESEND_COOLDOWN_S * 1000 : null,
  )
  const wait = useSecondsLeft(cooldownUntil)
  const [resending, setResending] = useState(false)
  const [resendNote, setResendNote] = useState<{ text: string; tone: "muted" | "error" } | null>(null)

  useEffect(() => {
    if (active) inputRef.current?.focus()
  }, [active])
  // A wrong code: back to the field, selected so typing replaces it.
  useEffect(() => {
    if (error) inputRef.current?.select()
  }, [error])

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (busy) return
    if (!code.trim()) {
      setFieldError(t("settings.cloudSync.codeRequired"))
      inputRef.current?.focus()
      return
    }
    setFieldError(null)
    onSubmit(code.trim())
  }

  const resend = async () => {
    if (!onResend || resending || wait > 0) return
    setResending(true)
    setResendNote(null)
    try {
      await onResend()
      setCooldownUntil(Date.now() + RESEND_COOLDOWN_S * 1000)
      setResendNote({ text: resentMessage ?? t("settings.cloudSync.codeResent", { email }), tone: "muted" })
    } catch (err) {
      const retryAfter = err instanceof ApiError && err.code === "resend_throttled" ? Number(err.body?.retry_after) : NaN
      if (Number.isFinite(retryAfter) && retryAfter > 0) {
        setCooldownUntil(Date.now() + retryAfter * 1000)
        setResendNote({ text: t("settings.cloudSync.resendWait", { seconds: retryAfter }), tone: "muted" })
      } else {
        const message = err instanceof Error ? err.message : String(err)
        setResendNote({ text: t("settings.cloudSync.resendError", { error: message }), tone: "error" })
      }
    } finally {
      setResending(false)
    }
  }

  const isEmail = challenge === "email_code"
  const shownError = fieldError ?? error
  return (
    <form noValidate onSubmit={submit} className="grid gap-3">
      <SettingRow
        label={isEmail ? t("settings.cloudSync.emailCodeLabel") : t("settings.cloudSync.totpCodeLabel")}
        htmlFor="cloud-code"
        description={
          isEmail ? t("settings.cloudSync.emailCodeIntro", { email }) : t("settings.cloudSync.totpCodeIntro")
        }
        control={
          <div className="grid w-72 justify-items-end gap-1">
            <Input
              id="cloud-code"
              ref={inputRef}
              name={isEmail ? "email_code" : "totp_code"}
              {...oneTimeCodeProps}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="123456"
              aria-invalid={shownError ? true : undefined}
              aria-describedby={[descId("cloud-code"), shownError && "cloud-code-error"].filter(Boolean).join(" ")}
              className="w-full font-mono tracking-widest tabular-nums"
            />
            <StatusMessage id="cloud-code-error" tone="error" className="max-w-72 text-right break-words">
              {shownError}
            </StatusMessage>
          </div>
        }
      />
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={busy} aria-busy={busy || undefined}>
          {busy ? t("settings.cloudSync.verifying") : t("settings.cloudSync.verify")}
        </Button>
        {onResend && (
          <Button
            type="button"
            variant="outline"
            disabled={busy || resending || wait > 0}
            aria-busy={resending || undefined}
            onClick={() => void resend()}
            className="tabular-nums"
          >
            {resending
              ? t("settings.cloudSync.resendingCode")
              : wait > 0
                ? t("settings.cloudSync.resendCodeIn", { seconds: wait })
                : t("settings.cloudSync.resendCode")}
          </Button>
        )}
        <Button type="button" variant="outline" disabled={busy} onClick={onBack}>
          {t("common.back")}
        </Button>
      </div>
      <StatusMessage tone={resendNote?.tone ?? "muted"} className="break-words">
        {resendNote?.text}
      </StatusMessage>
    </form>
  )
}
