import { useState } from "react"
import type { ParseKeys } from "i18next"
import { useTranslation } from "react-i18next"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import { ApiKeyField } from "@/components/settings/ApiKeyField"
import { descId, ExternalLink, SaveStatus, SettingRow } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { verifyProvider, type AppConfig, type LlmPreset, type UpdateConfigRequest } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import { isAbsoluteUrl } from "@/lib/url"

interface PresetInfo {
  id: LlmPreset
  /** Brand name (never translated); Custom's comes from the dictionary. */
  name?: string
  /** Where to create a key. */
  keyUrl?: string
  /** One muted line worth knowing before choosing it. */
  noteKey?: ParseKeys
}

/** The presets in Go config.LLMPresets, in the order the picker lists them. */
const PRESETS: readonly PresetInfo[] = [
  { id: "openai", name: "OpenAI", keyUrl: "https://platform.openai.com/api-keys" },
  {
    id: "gemini",
    name: "Google Gemini",
    keyUrl: "https://aistudio.google.com/apikey",
    noteKey: "settings.llm.noteGemini",
  },
  { id: "openrouter", name: "OpenRouter", keyUrl: "https://openrouter.ai/keys" },
  {
    id: "anthropic",
    name: "Anthropic",
    keyUrl: "https://platform.claude.com",
    noteKey: "settings.llm.noteAnthropic",
  },
  { id: "groq", name: "Groq", keyUrl: "https://console.groq.com/keys" },
  { id: "deepseek", name: "DeepSeek", keyUrl: "https://platform.deepseek.com/api_keys" },
  {
    id: "zai",
    name: "Z.AI (GLM)",
    keyUrl: "https://z.ai/manage-apikey/apikey-list",
    noteKey: "settings.llm.noteZai",
  },
  {
    id: "cloudflare",
    name: "Cloudflare Workers AI",
    keyUrl: "https://dash.cloudflare.com/profile/api-tokens",
    noteKey: "settings.llm.noteCloudflare",
  },
  { id: "custom" },
]

type LlmPatch = NonNullable<UpdateConfigRequest["llm"]>

/** A change that moves the LLM to another server, waiting for the user
 *  to agree that the saved key (entered for the old one) goes. */
interface PendingMove {
  patch: LlmPatch
  title: string
  body: string
}

/**
 * The shared OpenAI-compatible LLM ("Your AI provider"): which service,
 * its server when it needs one (Custom: an address; Cloudflare: the
 * Account ID), the API key, and under Advanced the fast model (live
 * translation) and strong model (summaries, translation after a
 * meeting), with the preset's defaults as placeholders. Used by
 * Settings → Translation and Settings → Summaries: it's one config
 * block (llm), so a key added on either page serves both.
 *
 * The server drops the stored key when the LLM moves to another server
 * without a new key, so such a move asks first while a key is saved.
 */
export function LlmProviderForm({
  config,
  onSaved,
  onChanged,
  idPrefix,
}: {
  config: AppConfig
  onSaved: (config: AppConfig) => void
  /** After any successful save, e.g. to re-check readiness. */
  onChanged?: () => void
  /** Keeps ids unique per page ("translate-llm", "summary-llm"). */
  idPrefix: string
}) {
  const { t } = useTranslation()
  const llm = config.llm
  const keySet = config.llm_api_key_set
  const preset = llm.preset
  const info = PRESETS.find((p) => p.id === preset)
  const presetName = (p: PresetInfo | undefined) => (p ? (p.name ?? t("settings.llm.presetCustom")) : "")

  const [baseUrl, setBaseUrl] = useState(llm.base_url)
  const [baseUrlError, setBaseUrlError] = useState<string | null>(null)
  const [accountId, setAccountId] = useState(llm.account_id)
  const [accountError, setAccountError] = useState<string | null>(null)
  const [fastModel, setFastModel] = useState(llm.fast_model)
  const [strongModel, setStrongModel] = useState(llm.strong_model)
  const [pendingMove, setPendingMove] = useState<PendingMove | null>(null)
  const [moving, setMoving] = useState(false)
  const [moveError, setMoveError] = useState<string | null>(null)

  // A save (here, or the preset change resetting the models) changes the
  // stored values: the fields follow. Compared by value, so saving
  // something else (the key) doesn't wipe a field being typed in.
  const llmKey = JSON.stringify([llm.preset, llm.base_url, llm.account_id, llm.fast_model, llm.strong_model])
  const [syncedKey, setSyncedKey] = useState(llmKey)
  if (syncedKey !== llmKey) {
    setSyncedKey(llmKey)
    setBaseUrl(llm.base_url)
    setAccountId(llm.account_id)
    setFastModel(llm.fast_model)
    setStrongModel(llm.strong_model)
    setBaseUrlError(null)
    setAccountError(null)
  }

  // One save state per area, so each status shows next to its control.
  const serverSave = useConfigSave(config, onSaved)
  const modelSave = useConfigSave(config, onSaved)
  const keySave = useConfigSave(config, onSaved)

  const put = async (save: typeof serverSave, patch: LlmPatch) => {
    const ok = await save.put({ llm: patch })
    if (ok) onChanged?.()
    return ok
  }

  /** Saves a server change, asking first when it would drop a saved key. */
  const moveServer = (patch: LlmPatch, title: string, body: string) => {
    // No saved key, or no server yet that a key could belong to.
    if (!keySet || !llm.resolved.base_url) {
      void put(serverSave, patch)
      return
    }
    setMoveError(null)
    setPendingMove({ patch, title, body })
  }

  const confirmMove = async () => {
    if (!pendingMove || moving) return
    setMoving(true)
    const ok = await put(serverSave, pendingMove.patch)
    setMoving(false)
    if (ok) setPendingMove(null)
    else setMoveError(t("settings.pieces.saveError"))
  }

  const choosePreset = (value: string) => {
    const next = value as LlmPreset
    if (next === preset) return
    const nextInfo = PRESETS.find((p) => p.id === next)
    // The models belong to the old service: back to the new one's defaults.
    moveServer(
      { preset: next, fast_model: "", strong_model: "" },
      t("settings.llm.switchTitle", { provider: presetName(nextInfo) }),
      t("settings.llm.switchBody", { old: presetName(info) || t("settings.llm.presetNone") }),
    )
  }

  const commitBaseUrl = () => {
    const v = baseUrl.trim()
    if (!isAbsoluteUrl(v, ["http", "https"])) {
      setBaseUrlError(t("settings.llm.baseUrlError"))
      return
    }
    setBaseUrlError(null)
    if (v.replace(/\/+$/, "") === llm.base_url) return
    moveServer({ base_url: v }, t("settings.llm.moveTitle"), t("settings.llm.moveBody"))
  }

  const commitAccount = () => {
    const v = accountId.trim()
    if (!v) {
      setAccountError(t("settings.llm.accountRequired"))
      return
    }
    setAccountError(null)
    if (v === llm.account_id) return
    moveServer({ account_id: v }, t("settings.llm.moveTitle"), t("settings.llm.moveBody"))
  }

  const commitModel = (which: "fast_model" | "strong_model") => {
    const v = (which === "fast_model" ? fastModel : strongModel).trim()
    if (v === llm[which]) return
    // "" is sent on purpose: back to the preset's default.
    void put(modelSave, { [which]: v })
  }

  const custom = preset === "custom"
  const cloudflare = preset === "cloudflare"
  const serverKnown = llm.resolved.base_url !== ""
  const id = (s: string) => `${idPrefix}-${s}`
  const modelPlaceholder = (resolved: string) => resolved || t("settings.llm.modelPlaceholder")

  return (
    <div>
      <SettingRow
        label={t("settings.llm.presetLabel")}
        htmlFor={id("preset")}
        description={
          <>
            {t("settings.llm.presetDesc")}
            {info?.noteKey && <span className="mt-1 block">{t(info.noteKey)}</span>}
          </>
        }
        control={
          <div className="grid justify-items-end gap-1">
            <Select value={preset || undefined} onValueChange={choosePreset}>
              <SelectTrigger id={id("preset")} className="w-56" aria-describedby={descId(id("preset"))}>
                <SelectValue placeholder={t("settings.translation.choosePlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {PRESETS.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {presetName(p)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <SaveStatus state={serverSave.save.state} error={serverSave.save.error} />
          </div>
        }
      />

      {custom && (
        <SettingRow
          label={t("settings.llm.baseUrlLabel")}
          htmlFor={id("base-url")}
          description={t("settings.llm.baseUrlDesc")}
          control={
            <div className="grid w-80 justify-items-end gap-1">
              <Input
                id={id("base-url")}
                name="llm_base_url"
                type="url"
                {...noAutofill}
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                onBlur={commitBaseUrl}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitBaseUrl()
                }}
                placeholder="https://api.example.com/v1…"
                aria-invalid={baseUrlError ? true : undefined}
                aria-describedby={
                  baseUrlError ? `${descId(id("base-url"))} ${id("base-url-error")}` : descId(id("base-url"))
                }
                className="w-full"
              />
              <StatusMessage id={id("base-url-error")} tone="error" className="break-words">
                {baseUrlError}
              </StatusMessage>
            </div>
          }
        />
      )}

      {cloudflare && (
        <SettingRow
          label={t("settings.llm.accountLabel")}
          htmlFor={id("account")}
          description={t("settings.llm.accountDesc")}
          control={
            <div className="grid w-80 justify-items-end gap-1">
              <Input
                id={id("account")}
                name="llm_account_id"
                translate="no"
                {...noAutofill}
                value={accountId}
                onChange={(e) => setAccountId(e.target.value)}
                onBlur={commitAccount}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitAccount()
                }}
                placeholder={t("settings.llm.accountPlaceholder")}
                aria-invalid={accountError ? true : undefined}
                aria-describedby={
                  accountError ? `${descId(id("account"))} ${id("account-error")}` : descId(id("account"))
                }
                className="w-full"
              />
              <StatusMessage id={id("account-error")} tone="error">
                {accountError}
              </StatusMessage>
            </div>
          }
        />
      )}

      {preset && (
        <ApiKeyField
          // A new service starts with a clean key check.
          key={`key-${preset}`}
          id={id("key")}
          label={cloudflare ? t("settings.llm.tokenLabel") : t("settings.llm.keyLabel")}
          description={
            <>
              {t("settings.llm.keyDesc")}
              {info?.keyUrl && (
                <>
                  {" "}
                  <ExternalLink href={info.keyUrl}>
                    {cloudflare ? t("settings.llm.getToken") : t("settings.llm.getKey", { provider: presetName(info) })}
                  </ExternalLink>
                </>
              )}
            </>
          }
          keySet={keySet}
          onSave={(key) => put(keySave, { api_key: key })}
          verify={(key) => verifyProvider("llm", key)}
          blockedReason={
            serverKnown ? undefined : cloudflare ? t("settings.llm.accountFirst") : t("settings.llm.baseUrlFirst")
          }
        />
      )}

      {preset && (
        // Custom has no defaults, so its models aren't optional: open.
        <Disclosure key={`models-${preset}`} defaultOpen={custom} label={t("settings.llm.advanced")} className="py-3">
          {custom && <p className="text-sm text-muted-foreground">{t("settings.llm.customModelsNeeded")}</p>}
          <SettingRow
            label={t("settings.llm.fastModelLabel")}
            htmlFor={id("fast-model")}
            description={t("settings.llm.fastModelDesc")}
            control={
              <Input
                id={id("fast-model")}
                name="llm_fast_model"
                translate="no"
                {...noAutofill}
                value={fastModel}
                onChange={(e) => setFastModel(e.target.value)}
                onBlur={() => commitModel("fast_model")}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitModel("fast_model")
                }}
                placeholder={modelPlaceholder(llm.fast_model ? "" : llm.resolved.fast_model)}
                aria-describedby={descId(id("fast-model"))}
                className="w-72"
              />
            }
          />
          <SettingRow
            label={t("settings.llm.strongModelLabel")}
            htmlFor={id("strong-model")}
            description={t("settings.llm.strongModelDesc")}
            control={
              <Input
                id={id("strong-model")}
                name="llm_strong_model"
                translate="no"
                {...noAutofill}
                value={strongModel}
                onChange={(e) => setStrongModel(e.target.value)}
                onBlur={() => commitModel("strong_model")}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitModel("strong_model")
                }}
                placeholder={modelPlaceholder(llm.strong_model ? "" : llm.resolved.strong_model)}
                aria-describedby={descId(id("strong-model"))}
                className="w-72"
              />
            }
          />
          <div className="flex items-center justify-end">
            <SaveStatus state={modelSave.save.state} error={modelSave.save.error} />
          </div>
        </Disclosure>
      )}

      <ConfirmDialog
        open={pendingMove != null}
        onOpenChange={(open) => {
          if (open || moving) return
          // Kept the old server: the fields show it again.
          setPendingMove(null)
          setBaseUrl(llm.base_url)
          setAccountId(llm.account_id)
        }}
        title={pendingMove?.title ?? ""}
        description={pendingMove?.body ?? ""}
        confirmLabel={moving ? t("settings.pieces.saving") : t("settings.llm.switchConfirm")}
        pending={moving}
        error={moveError}
        onConfirm={() => void confirmMove()}
      />
    </div>
  )
}
