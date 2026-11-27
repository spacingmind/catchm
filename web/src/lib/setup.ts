/**
 * The setup catalog (docs/plans/active/cloud-mode.md → "Modules and
 * options"): which provider options each module has, which side they're
 * on (cloud = bring a key, selfhost = runs on this computer), and the
 * presets that fill every module at once. Every screen that hides one
 * side under a setup asks here, so a new provider is one entry below.
 */
import i18next, { type ParseKeys, type TFunction } from "i18next"

import type {
  AppConfig,
  CloudSttEngine,
  Setup,
  SttEngine,
  SummaryProvider,
  TranslateProvider,
  TranslateStatus,
  UpdateConfigRequest,
} from "@/lib/api"

export type Side = "cloud" | "selfhost"

export type ModuleId = "transcription" | "translation" | "summaries"

/** A setup the user can pick ("" is only "not chosen yet"). */
export type Preset = Exclude<Setup, "">

export type OptionBadge = "speakers" | "translatesItself" | "free" | "freeTier" | "freeCredit"

/** Settings sections an option's own setup lives in. */
export type SetupSection = "transcription" | "translation" | "summaries"

interface OptionValue {
  transcription: SttEngine
  translation: TranslateProvider
  summaries: SummaryProvider
}

export interface SetupOption<M extends ModuleId = ModuleId> {
  /** The wire value written to the module's config field. */
  id: OptionValue[M]
  module: M
  side: Side
  labelKey: ParseKeys
  descKey: ParseKeys
  badges: OptionBadge[]
  /** Where the option's key/server/model is set up. */
  section: SetupSection
}

export const MODULES: readonly ModuleId[] = ["transcription", "translation", "summaries"]

export const OPTIONS: readonly SetupOption[] = [
  {
    id: "soniox",
    module: "transcription",
    side: "cloud",
    labelKey: "setup.options.soniox",
    descKey: "setup.options.sonioxDesc",
    badges: ["speakers", "translatesItself"],
    section: "transcription",
  },
  {
    id: "deepgram",
    module: "transcription",
    side: "cloud",
    labelKey: "setup.options.deepgram",
    descKey: "setup.options.deepgramDesc",
    badges: ["speakers", "freeCredit"],
    section: "transcription",
  },
  {
    id: "elevenlabs",
    module: "transcription",
    side: "cloud",
    labelKey: "setup.options.elevenlabs",
    descKey: "setup.options.elevenlabsDesc",
    badges: ["speakers", "freeTier"],
    section: "transcription",
  },
  {
    id: "speechmatics",
    module: "transcription",
    side: "cloud",
    labelKey: "setup.options.speechmatics",
    descKey: "setup.options.speechmaticsDesc",
    badges: ["speakers", "freeCredit"],
    section: "transcription",
  },
  {
    id: "gladia",
    module: "transcription",
    side: "cloud",
    labelKey: "setup.options.gladia",
    descKey: "setup.options.gladiaDesc",
    badges: ["freeTier"],
    section: "transcription",
  },
  {
    id: "whisper",
    module: "transcription",
    side: "selfhost",
    labelKey: "setup.options.whisper",
    descKey: "setup.options.whisperDesc",
    badges: ["free"],
    section: "transcription",
  },
  {
    id: "none",
    module: "translation",
    side: "cloud",
    labelKey: "setup.options.builtIn",
    descKey: "setup.options.builtInDesc",
    badges: [],
    section: "translation",
  },
  {
    id: "google",
    module: "translation",
    side: "cloud",
    labelKey: "setup.options.google",
    descKey: "setup.options.googleDesc",
    badges: ["freeTier"],
    section: "translation",
  },
  {
    id: "llm",
    module: "translation",
    side: "cloud",
    labelKey: "setup.options.llm",
    descKey: "setup.options.llmTranslateDesc",
    badges: [],
    section: "translation",
  },
  {
    id: "ollama",
    module: "translation",
    side: "selfhost",
    labelKey: "setup.options.ollama",
    descKey: "setup.options.ollamaTranslateDesc",
    badges: ["free"],
    section: "translation",
  },
  {
    id: "catchm-cloud",
    module: "summaries",
    side: "cloud",
    labelKey: "setup.options.catchmCloud",
    descKey: "setup.options.catchmCloudDesc",
    badges: ["free"],
    section: "summaries",
  },
  {
    id: "openai",
    module: "summaries",
    side: "cloud",
    labelKey: "setup.options.llm",
    descKey: "setup.options.llmSummaryDesc",
    badges: [],
    section: "summaries",
  },
  {
    id: "ollama",
    module: "summaries",
    side: "selfhost",
    labelKey: "setup.options.ollama",
    descKey: "setup.options.ollamaSummaryDesc",
    badges: ["free"],
    section: "summaries",
  },
]

/** What the UI needs to know about a bring-your-own-key transcription
 *  provider beyond its option card. Its key flags follow one naming
 *  rule (AppConfig `<id>_api_key_set`, PUT `<id>_api_key`), so a new
 *  provider is an entry here, one in OPTIONS and its i18n strings. */
export interface CloudEngineInfo {
  /** Where the user creates an API key. */
  keyUrl: string
  /** POST /api/providers/{id}/verify can check its key ("Check key"). */
  verifiable: boolean
  /** List price for a file (import, re-transcribe), US$ per hour of
   *  audio: the cost estimates. */
  fileRatePerHour: number
  /** What happens to uploaded audio, when the provider promises more
   *  than "sent to be transcribed" (meetingDetail.cloudPrivacy). */
  privacyKey?: ParseKeys
}

export const CLOUD_ENGINES: Record<CloudSttEngine, CloudEngineInfo> = {
  soniox: {
    keyUrl: "https://soniox.com",
    verifiable: false,
    fileRatePerHour: 0.1,
    // engine_soniox_async deletes the file and transcription afterwards.
    privacyKey: "meetingDetail.sonioxPrivacy",
  },
  deepgram: {
    keyUrl: "https://console.deepgram.com",
    verifiable: true,
    fileRatePerHour: 0.26,
  },
  elevenlabs: {
    keyUrl: "https://elevenlabs.io/app/settings/api-keys",
    verifiable: true,
    fileRatePerHour: 0.2,
  },
  speechmatics: {
    keyUrl: "https://portal.speechmatics.com/settings/api-keys",
    verifiable: true,
    fileRatePerHour: 0.75,
  },
  gladia: {
    keyUrl: "https://app.gladia.io",
    verifiable: true,
    fileRatePerHour: 0.3,
  },
}

/** Whether `engine` (a config or meeting engine value) is a cloud engine. */
export function isCloudEngine(engine: string | undefined): engine is CloudSttEngine {
  return engine != null && Object.hasOwn(CLOUD_ENGINES, engine)
}

/** A config/meeting engine value as an SttEngine: anything that isn't a
 *  known cloud engine runs on Whisper (the server's default). */
export function toEngine(engine: string): SttEngine {
  return isCloudEngine(engine) ? engine : "whisper"
}

/** Whether the cloud engine's API key is stored. */
export function engineKeySet(config: AppConfig, engine: CloudSttEngine): boolean {
  return config[`${engine}_api_key_set` as const]
}

/** The PUT /api/config body that stores a cloud engine's key. */
export function engineKeyPatch(engine: CloudSttEngine, key: string): UpdateConfigRequest {
  return { [`${engine}_api_key` as const]: key }
}

/** Whether the engine can be picked without more setup: Whisper always
 *  (its server is checked separately), a cloud engine once its key is set. */
export function engineHasKey(config: AppConfig, engine: SttEngine): boolean {
  return !isCloudEngine(engine) || engineKeySet(config, engine)
}

/** "about $0.05" worth of dollars for `seconds` of audio sent to a cloud
 *  engine as a file, at its list price. */
export function fileCost(engine: CloudSttEngine, seconds: number): string {
  return ((seconds / 3600) * CLOUD_ENGINES[engine].fileRatePerHour).toFixed(2)
}

/** What happens to audio sent to a cloud engine as a file, in plain words. */
export function cloudPrivacy(engine: CloudSttEngine): string {
  const key = CLOUD_ENGINES[engine].privacyKey ?? "meetingDetail.cloudPrivacy"
  return i18next.t(key as "meetingDetail.cloudPrivacy", { provider: engineOptionName(engine) })
}

/**
 * The cloud engine "Improve with …" uses: the configured engine
 * when it's a cloud engine with a key, else the first one with a key
 * that the setup shows; null when there's none.
 */
export function refineCloudEngine(config: AppConfig): CloudSttEngine | null {
  const keyed = visibleEngines(config.setup).filter(
    (e): e is CloudSttEngine => isCloudEngine(e) && engineKeySet(config, e),
  )
  const configured = toEngine(config.engine)
  return keyed.find((e) => e === configured) ?? keyed[0] ?? null
}

/** What a preset puts in each module. Custom keeps what's there. Cloud
 *  leaves translation to Soniox (which translates on its own), so a new
 *  user needs one key, not two. */
export const PRESETS: Record<Exclude<Preset, "custom">, OptionValue> = {
  cloud: { transcription: "soniox", translation: "none", summaries: "catchm-cloud" },
  selfhost: { transcription: "whisper", translation: "ollama", summaries: "ollama" },
}

/** Under Cloud only cloud options show, under Self-host only self-host
 *  ones; Custom (and "", before the first-run chooser) shows all. */
function sideVisible(setup: Setup, side: Side): boolean {
  if (setup === "cloud") return side === "cloud"
  if (setup === "selfhost") return side === "selfhost"
  return true
}

/** Whether self-hosted parts (Whisper, Ollama) may appear at all. */
export function isSelfHostVisible(setup: Setup): boolean {
  return sideVisible(setup, "selfhost")
}

/** Whether cloud providers (Soniox, Google, your AI provider, CatchM
 *  Cloud) may appear. */
export function isCloudVisible(setup: Setup): boolean {
  return sideVisible(setup, "cloud")
}

export function optionsVisible<M extends ModuleId>(setup: Setup, module: M): SetupOption<M>[] {
  return OPTIONS.filter((o): o is SetupOption<M> => o.module === module && sideVisible(setup, o.side))
}

/** Whether one option (e.g. the "whisper" engine) is shown under setup. */
export function isOptionVisible<M extends ModuleId>(setup: Setup, module: M, id: OptionValue[M]): boolean {
  return optionsVisible(setup, module).some((o) => o.id === id)
}

export function findOption<M extends ModuleId>(module: M, id: OptionValue[M]): SetupOption<M> | undefined {
  return OPTIONS.find((o): o is SetupOption<M> => o.module === module && o.id === id)
}

/** The option each module currently uses, normalized from the config. */
export function currentChoices(config: AppConfig): OptionValue {
  return {
    transcription: toEngine(config.engine),
    translation: effectiveTranslateProvider(config),
    summaries: config.summary.provider,
  }
}

/** Whether the option's API key is already stored ("Key added"). */
export function keyAdded(config: AppConfig, o: SetupOption): boolean {
  if (o.module === "transcription") return isCloudEngine(o.id) && engineKeySet(config, o.id)
  if (o.module === "translation") {
    if (o.id === "google") return config.translate.google_api_key_set
    return o.id === "llm" && config.llm_api_key_set
  }
  return o.id === "openai" && config.llm_api_key_set
}

/** The PUT /api/config body that sets one module's option. */
function moduleFieldPatch<M extends ModuleId>(module: M, id: OptionValue[M]): UpdateConfigRequest {
  switch (module) {
    case "transcription":
      return { engine: id as SttEngine }
    case "translation":
      return { translate: { provider: id as TranslateProvider } }
    default:
      return { summary: { provider: id as SummaryProvider } }
  }
}

/**
 * The PUT body for choosing `id` in `module`: the field itself, plus
 * setup "custom" when `id` comes from the other side than the Cloud or
 * Self-host setup the user is on.
 */
export function modulePatch<M extends ModuleId>(config: AppConfig, module: M, id: OptionValue[M]): UpdateConfigRequest {
  const patch = moduleFieldPatch(module, id)
  // An engine that translates on its own (Soniox) always uses its own
  // translation: picking it locks translation to "built in".
  if (module === "transcription" && engineTranslates(id as string)) {
    patch.translate = { provider: "none" }
  }
  const setup = config.setup
  // Cloud / Self-host mean "only that side", not "exactly the preset's
  // picks": switching between two cloud options keeps Cloud (otherwise
  // picking a different summary provider would suddenly show Whisper and
  // Ollama everywhere). Only an option from the other side makes it Custom.
  if ((setup === "cloud" || setup === "selfhost") && findOption(module, id)?.side !== setup) {
    patch.setup = "custom"
  }
  return patch
}

/** The PUT body for choosing a preset: Cloud and Self-host fill every
 *  module; Custom only records the choice and keeps what's there. */
export function presetPatch(preset: Preset): UpdateConfigRequest {
  if (preset === "custom") return { setup: "custom" }
  const p = PRESETS[preset]
  return {
    setup: preset,
    engine: p.transcription,
    translate: { provider: p.translation },
    summary: { provider: p.summaries },
  }
}

/** Engines the user may pick under setup, in display order. */
export function visibleEngines(setup: Setup): SttEngine[] {
  return optionsVisible(setup, "transcription").map((o) => o.id)
}

/** Whether the engine translates on its own (internal/stt
 *  SupportsTranslation): its option has the "translatesItself" badge.
 *  Then no translation provider is needed. */
export function engineTranslates(engine: string): boolean {
  return OPTIONS.some((o) => o.module === "transcription" && o.id === engine && o.badges.includes("translatesItself"))
}

/** The engines that translate on their own, by name, as one phrase
 *  ("Soniox", later "Soniox or Gladia"), for "use … instead" hints. */
export function translatingEngineNames(): string {
  const names = OPTIONS.filter((o) => o.module === "transcription" && o.badges.includes("translatesItself")).map((o) =>
    i18next.t(o.labelKey),
  )
  return new Intl.ListFormat(i18next.language || "en", { type: "disjunction" }).format(names)
}

/**
 * Why an option can't be picked right now (an i18n key), or null.
 * With Soniox transcribing, translation is locked to Soniox's own
 * ("none"): the user chose that rule (2026-11-27) so a meeting never
 * mixes two translators.
 */
export function optionLockedKey(config: AppConfig, module: ModuleId, id: string): string | null {
  if (module === "translation" && id !== "none" && engineTranslates(config.engine)) {
    return "setup.lockedByEngineTranslation"
  }
  return null
}

/** optionLockedKey as display text (undefined when unlocked). */
export function lockedReason(t: TFunction, config: AppConfig, module: ModuleId, id: string): string | undefined {
  const key = optionLockedKey(config, module, id)
  return key ? t(key as ParseKeys, { engine: engineOptionName(config.engine) }) : undefined
}

/** The engine's provider name from its option card ("Deepgram",
 *  "Whisper"); the raw value for an engine the catalog doesn't know. */
export function engineOptionName(engine: string): string {
  const o = OPTIONS.find((x) => x.module === "transcription" && x.id === engine)
  return o ? i18next.t(o.labelKey) : engine
}

/** Translation provider "none" with an engine that doesn't translate:
 *  meetings aren't translated (plain words, naming the engine). */
export function needsTranslatorText(engine: string): string {
  return i18next.t("setup.status.needsTranslator", {
    engine: engineOptionName(toEngine(engine)),
    translators: translatingEngineNames(),
  })
}

/** The translation choice in effect: "none" whenever the engine
 *  translates on its own, whatever is stored. */
export function effectiveTranslateProvider(config: AppConfig): TranslateProvider {
  return engineTranslates(config.engine) ? "none" : config.translate.provider
}

/** What a translation provider that isn't ready still needs, in plain
 *  words (the server's reason is English-only). */
export function translateNeed(st: TranslateStatus): string {
  switch (st.provider) {
    case "ollama":
      return i18next.t(st.ollama_reachable ? "getStarted.modelMissing" : "getStarted.needsOllama")
    case "google":
      return i18next.t("setup.status.addGoogleKey")
    case "llm":
      return i18next.t("setup.status.setUpLlm")
    default:
      return i18next.t("setup.status.notReadyFallback")
  }
}
