import { Trans, useTranslation } from "react-i18next"

import { ApiKeyField } from "@/components/settings/ApiKeyField"
import { ChoiceCard } from "@/components/settings/ChoiceCard"
import { ExternalLink } from "@/components/settings/pieces"
import { OptionBadges } from "@/components/settings/SetupBuilder"
import { updateConfig, verifyProvider, type AppConfig, type CloudSttEngine } from "@/lib/api"
import {
  CLOUD_ENGINES,
  engineKeyPatch,
  engineKeySet,
  engineOptionName,
  engineTranslates,
  isCloudEngine,
  keyAdded,
  optionsVisible,
  type SetupOption,
} from "@/lib/setup"

/** The translators a cloud setup can use when its engine doesn't
 *  translate on its own. */
export type CloudTranslator = "google" | "llm"

const GOOGLE_KEY_URL = "https://console.cloud.google.com/apis/credentials"

/** Saves a key, keeping the wizard's copy of the config current. */
async function saveKey(patch: Parameters<typeof updateConfig>[0], onConfig: (c: AppConfig) => void): Promise<boolean> {
  try {
    onConfig(await updateConfig(patch))
    return true
  } catch {
    return false
  }
}

/**
 * Step 3 for Cloud (BYOK): the transcription provider as cards, its key
 * pasted right here (Check key where the provider has one), and, when
 * the user wants translation and the engine can't translate itself, a
 * light choice of translator. Every key is optional: "Add a key later"
 * finishes without one, and Get started reminds the user.
 */
export function CloudStep({
  config,
  onConfig,
  headingId,
  engine,
  onEngine,
  wantsTranslation,
  translator,
  onTranslator,
}: {
  config: AppConfig
  onConfig: (c: AppConfig) => void
  /** The step's h2, naming the provider cards. */
  headingId: string
  engine: CloudSttEngine
  onEngine: (e: CloudSttEngine) => void
  wantsTranslation: boolean
  translator: CloudTranslator
  onTranslator: (p: CloudTranslator) => void
}) {
  const { t } = useTranslation()
  const engines = optionsVisible("cloud", "transcription")
  const translators = optionsVisible("cloud", "translation").filter(
    (o): o is SetupOption<"translation"> & { id: CloudTranslator } => o.id === "google" || o.id === "llm",
  )
  const provider = engineOptionName(engine)
  const info = CLOUD_ENGINES[engine]
  const showTranslation = wantsTranslation && !engineTranslates(engine)

  return (
    <div className="grid gap-6">
      <div>
        <div role="radiogroup" aria-labelledby={headingId} className="grid gap-3 sm:grid-cols-2">
          {engines.map((o) => (
            <ChoiceCard
              key={o.id}
              name="ob-engine"
              value={o.id}
              checked={engine === o.id}
              onChange={(id) => {
                if (isCloudEngine(id)) onEngine(id)
              }}
              title={t(o.labelKey)}
              description={t(o.descKey)}
              badges={o.badges.length > 0 || keyAdded(config, o) ? <OptionBadges config={config} option={o} /> : undefined}
            />
          ))}
        </div>
        <div className="mt-2">
          <ApiKeyField
            key={engine}
            id={`ob-${engine}-key`}
            label={t("settings.transcription.keyLabel", { provider })}
            description={
              <>
                {t("settings.transcription.keyDesc", { engine: provider })}{" "}
                <ExternalLink href={info.keyUrl}>{t("settings.transcription.getKeyLink", { provider })}</ExternalLink>
              </>
            }
            keySet={engineKeySet(config, engine)}
            onSave={(key) => saveKey(engineKeyPatch(engine, key), onConfig)}
            verify={info.verifiable ? (key) => verifyProvider(engine, key) : undefined}
          />
        </div>
        {!engineKeySet(config, engine) && (
          <p className="text-sm text-muted-foreground">{t("onboarding.cloud.keyLater")}</p>
        )}
      </div>

      {showTranslation && (
        <section aria-labelledby="ob-translation-title">
          <h3 id="ob-translation-title" className="text-sm font-medium">
            {t("onboarding.cloud.translationTitle")}
          </h3>
          <p className="mb-2 mt-0.5 text-sm text-muted-foreground">
            {t("onboarding.cloud.translationDesc", { engine: provider })}
          </p>
          <div role="radiogroup" aria-labelledby="ob-translation-title" className="grid gap-3 sm:grid-cols-2">
            {translators.map((o) => (
              <ChoiceCard
                key={o.id}
                name="ob-translator"
                value={o.id}
                checked={translator === o.id}
                onChange={(id) => onTranslator(id as CloudTranslator)}
                title={t(o.labelKey)}
                description={t(o.descKey)}
                badges={o.badges.length > 0 || keyAdded(config, o) ? <OptionBadges config={config} option={o} /> : undefined}
              />
            ))}
          </div>
          {translator === "google" ? (
            <div className="mt-2">
              <ApiKeyField
                id="ob-google-key"
                label={t("onboarding.cloud.googleKeyLabel")}
                description={
                  <Trans
                    i18nKey="onboarding.cloud.googleKeyDesc"
                    components={{ ext: <ExternalLink href={GOOGLE_KEY_URL}>{null}</ExternalLink> }}
                  />
                }
                keySet={config.translate.google_api_key_set}
                onSave={(key) => saveKey({ translate: { google_api_key: key } }, onConfig)}
                verify={(key) => verifyProvider("google", key)}
              />
            </div>
          ) : (
            <p className="mt-3 text-sm text-muted-foreground">{t("onboarding.cloud.llmLater")}</p>
          )}
        </section>
      )}

      <p className="text-sm text-muted-foreground">{t("onboarding.cloud.summariesNote")}</p>
    </div>
  )
}
