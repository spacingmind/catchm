import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Check } from "lucide-react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { descId, SettingRow } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { VerifyResult } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Providers often answer with a JSON error body ("status 400: {…
 *  "message": "API key not valid" …}"): show just its message when
 *  there is one, else the text as it came. */
function providerMessage(error: string): string {
  const m = /"message"\s*:\s*"((?:[^"\\]|\\.)*)"/.exec(error)
  return m ? m[1] : error
}

type CheckState = { state: "idle" } | { state: "checking" } | { state: "ok" } | { state: "failed"; text: string }

/**
 * A write-only API key as a settings row: "Key added ✓" + Check key +
 * Replace key once one is stored, or Add key; editing shows a password
 * field with Cancel, Check key (the typed key, before saving) and Save
 * key. Check key answers inline: the key works, or the provider's own
 * error. Without `verify` (a provider with no key check) there's no
 * Check key.
 *
 * `blockedReason`: the key can't be added yet (e.g. the server isn't
 * named), shown as text next to the disabled Add key button.
 */
export function ApiKeyField({
  id,
  label,
  description,
  keySet,
  onSave,
  verify,
  blockedReason,
}: {
  id: string
  label: string
  description: ReactNode
  keySet: boolean
  /** Saves the typed key; resolves true on success. */
  onSave: (key: string) => Promise<boolean>
  /** Checks a typed key, or the stored one when called without. */
  verify?: (key?: string) => Promise<VerifyResult>
  blockedReason?: string
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [input, setInput] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [check, setCheck] = useState<CheckState>({ state: "idle" })
  const inputRef = useRef<HTMLInputElement | null>(null)
  // Add/Replace button: focus returns here when the editor closes, so
  // keyboard users aren't dropped at the top of the page.
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const restoreFocus = useRef(false)
  const checkSeq = useRef(0)

  // A result for a key that's since been replaced or dropped is stale.
  const [prevKeySet, setPrevKeySet] = useState(keySet)
  if (prevKeySet !== keySet) {
    setPrevKeySet(keySet)
    setCheck({ state: "idle" })
  }

  const errorId = `${id}-error`
  const checkId = `${id}-check`
  const reasonId = `${id}-blocked`

  useEffect(() => {
    if (!editing && restoreFocus.current) {
      restoreFocus.current = false
      buttonRef.current?.focus()
    }
  }, [editing])

  const resetCheck = () => {
    checkSeq.current++
    setCheck({ state: "idle" })
  }

  const startEditing = () => {
    setInput("")
    setError(null)
    resetCheck()
    setEditing(true)
  }

  const closeEditor = () => {
    restoreFocus.current = true
    setEditing(false)
    setInput("")
    setError(null)
    resetCheck()
  }

  const runCheck = async (key?: string) => {
    if (!verify) return
    if (editing && !key) {
      setError(t("settings.transcription.enterKey"))
      inputRef.current?.focus()
      return
    }
    setError(null)
    const seq = ++checkSeq.current
    setCheck({ state: "checking" })
    try {
      const res = await verify(key)
      if (seq !== checkSeq.current) return
      setCheck(
        res.ok
          ? { state: "ok" }
          : { state: "failed", text: t("settings.keys.checkFailed", {
                error: res.error ? providerMessage(res.error) : t("settings.keys.checkFailedFallback"),
              }) },
      )
    } catch (err) {
      if (seq === checkSeq.current) {
        setCheck({ state: "failed", text: t("settings.keys.checkError", { error: errorMessage(err) }) })
      }
    }
  }

  const save = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (saving) return
    const key = input.trim()
    if (!key) {
      setError(t("settings.transcription.enterKey"))
      inputRef.current?.focus()
      return
    }
    setSaving(true)
    setError(null)
    const ok = await onSave(key)
    setSaving(false)
    if (ok) {
      closeEditor()
    } else {
      setError(t("settings.transcription.keySaveError"))
      inputRef.current?.focus()
    }
  }

  const checking = check.state === "checking"
  const checkResult = (
    <StatusMessage id={checkId} tone={check.state === "failed" ? "error" : "success"} className="break-words text-right">
      {check.state === "ok" ? t("settings.keys.checkOk") : check.state === "failed" ? check.text : null}
    </StatusMessage>
  )

  return (
    <SettingRow
      label={label}
      htmlFor={editing ? id : undefined}
      id={id}
      description={description}
      control={
        editing ? (
          <form noValidate onSubmit={(e) => void save(e)} className="grid w-80 justify-items-end gap-1.5">
            <Input
              ref={inputRef}
              id={id}
              name={id.replace(/-/g, "_")}
              type="password"
              {...noAutofill}
              value={input}
              onChange={(e) => {
                setInput(e.target.value)
                if (check.state !== "idle") resetCheck()
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape" && !saving) closeEditor()
              }}
              placeholder={t("settings.transcription.keyPlaceholder")}
              // readOnly, not disabled, so focus can return here on failure.
              readOnly={saving}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? `${descId(id)} ${errorId}` : descId(id)}
              // The user just asked to add/replace the key: move focus into the field.
              autoFocus
            />
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="button" size="sm" variant="outline" disabled={saving} onClick={closeEditor}>
                {t("common.cancel")}
              </Button>
              {verify && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={saving || checking}
                  aria-busy={checking || undefined}
                  onClick={() => void runCheck(input.trim())}
                >
                  {checking ? t("settings.keys.checking") : t("settings.keys.check")}
                </Button>
              )}
              <Button type="submit" size="sm" disabled={saving} aria-busy={saving || undefined}>
                {saving ? t("settings.pieces.saving") : t("settings.transcription.saveKey")}
              </Button>
            </div>
            <StatusMessage id={errorId} tone="error">
              {error}
            </StatusMessage>
            {checkResult}
          </form>
        ) : (
          <div className="grid w-80 justify-items-end gap-1.5">
            <div className="flex flex-wrap items-center justify-end gap-2">
              {keySet && (
                <>
                  <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                    <Check aria-hidden="true" className="size-3.5 text-primary" />
                    {t("setup.badges.keyAdded")}
                  </span>
                  {verify && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={checking}
                      aria-busy={checking || undefined}
                      onClick={() => void runCheck()}
                    >
                      {checking ? t("settings.keys.checking") : t("settings.keys.check")}
                    </Button>
                  )}
                </>
              )}
              <Button
                ref={buttonRef}
                size="sm"
                variant="outline"
                onClick={startEditing}
                disabled={!keySet && blockedReason != null}
                aria-describedby={!keySet && blockedReason ? `${descId(id)} ${reasonId}` : descId(id)}
              >
                {keySet ? t("settings.transcription.replaceKey") : t("settings.transcription.addKey")}
              </Button>
            </div>
            {!keySet && blockedReason && (
              <p id={reasonId} className="text-xs text-muted-foreground">
                {blockedReason}
              </p>
            )}
            {checkResult}
          </div>
        )
      }
    />
  )
}
