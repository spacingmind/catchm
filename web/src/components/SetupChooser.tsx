import { useState } from "react"
import { Boxes, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { ChoiceCard } from "@/components/settings/ChoiceCard"
import { SetupBuilder } from "@/components/settings/SetupBuilder"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { updateConfig, type AppConfig } from "@/lib/api"
import { presetPatch, type Preset } from "@/lib/setup"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

const CHOICES: readonly Preset[] = ["cloud", "selfhost", "custom"]

const DESC_KEYS = {
  cloud: "setup.chooser.cloudDesc",
  selfhost: "setup.chooser.selfhostDesc",
  custom: "setup.chooser.customDesc",
} as const

/**
 * First run (config.setup === ""): Cloud, Self-host or Custom, before
 * the normal app. Custom opens the builder right here; Continue records
 * the choice (Cloud and Self-host fill every module), and App moves on
 * to the meetings list, where Get started lists what's left to do.
 */
export function SetupChooser({ config, onDone }: { config: AppConfig; onDone: () => void }) {
  const { t } = useTranslation()
  const [choice, setChoice] = useState<Preset>("cloud")
  // The builder's own saves (Custom) keep this copy current.
  const [current, setCurrent] = useState(config)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const apply = async () => {
    if (saving) return
    setSaving(true)
    setError(null)
    try {
      await updateConfig(presetPatch(choice))
      onDone()
    } catch (err) {
      setError(t("setup.chooser.saveError", { error: errorMessage(err) }))
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-2 flex items-center gap-2 text-2xl font-semibold text-balance">
        <Boxes aria-hidden="true" className="size-6" />
        {t("setup.chooser.title")}
      </h1>
      <p id="setup-chooser-desc" className="mb-6 text-sm text-muted-foreground">
        {t("setup.chooser.desc")}
      </p>

      <div
        role="radiogroup"
        aria-label={t("setup.chooser.groupLabel")}
        aria-describedby="setup-chooser-desc"
        className="grid gap-3"
      >
        {CHOICES.map((p) => (
          <ChoiceCard
            key={p}
            name="setup-choice"
            value={p}
            checked={choice === p}
            onChange={(v) => setChoice(v as Preset)}
            title={t(`setup.presets.${p}`)}
            description={t(DESC_KEYS[p])}
            badges={
              p === "cloud" ? (
                <Badge variant="secondary" className="border-transparent bg-primary/15 text-primary">
                  {t("setup.chooser.recommended")}
                </Badge>
              ) : undefined
            }
            className="p-4"
          />
        ))}
      </div>

      {choice === "custom" && (
        <div className="mt-8">
          <SetupBuilder config={current} onSaved={setCurrent} inline />
        </div>
      )}

      <StatusMessage tone="error" className="mt-4 break-words">
        {error}
      </StatusMessage>
      <div className="mt-6 flex justify-end">
        <Button onClick={() => void apply()} disabled={saving} aria-busy={saving || undefined}>
          {saving && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
          {saving ? t("setup.chooser.saving") : t("setup.chooser.continue")}
        </Button>
      </div>
    </div>
  )
}
