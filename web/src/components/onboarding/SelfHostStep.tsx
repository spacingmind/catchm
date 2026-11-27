import { TriangleAlert } from "lucide-react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import type { StatusCheck } from "@/hooks/useStatusCheck"
import type { MachineCheck } from "@/lib/api"
import { formatGb } from "@/lib/modelSizes"

const OS_NAMES: Record<string, string> = { darwin: "macOS", windows: "Windows", linux: "Linux" }

/**
 * Step 3 for Self-host: what GET /api/system says about this computer
 * (memory, free disk, cores, chip), a verdict, and for a slow or
 * unsuitable machine the reasons plus a way over to Cloud (BYOK).
 * Self-host stays allowed; the wizard's Finish button says "anyway"
 * then. Ends with what the user will install; the setup itself lives in
 * Settings (Get started links there).
 */
export function SelfHostStep({ check, onUseCloud }: { check: StatusCheck<MachineCheck>; onUseCloud: () => void }) {
  const { t } = useTranslation()
  const m = check.data
  const warn = m != null && m.selfhost !== "good"

  return (
    <div className="grid gap-6">
      {m ? (
        <div className="rounded-lg border border-border p-3">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">{t("onboarding.selfhost.verdictLabel")}</span>
            <VerdictBadge verdict={m.selfhost} />
          </div>
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Fact label={t("onboarding.selfhost.memory")} value={gb(m.total_ram_gb)} />
            <Fact label={t("onboarding.selfhost.freeDisk")} value={gb(m.free_disk_gb)} />
            <Fact label={t("onboarding.selfhost.cpuCores")} value={m.cpu_cores > 0 ? String(m.cpu_cores) : null} />
            <Fact
              label={t("onboarding.selfhost.platform")}
              value={m.apple_silicon ? t("onboarding.selfhost.appleSilicon") : `${OS_NAMES[m.os] ?? m.os} ${m.arch}`}
            />
          </dl>
          {!warn && m.selfhost_reasons.length > 0 && (
            <ul className="mt-3 grid gap-1 text-sm text-muted-foreground">
              {m.selfhost_reasons.map((r) => (
                <li key={r}>{t(`onboarding.selfhost.reasons.${r}`)}</li>
              ))}
            </ul>
          )}
        </div>
      ) : check.error ? (
        <div className="flex flex-wrap items-center gap-3">
          <StatusMessage tone="error" className="min-w-0 flex-1 break-words">
            {t("onboarding.selfhost.loadError", { error: check.error })}
          </StatusMessage>
          <Button size="sm" variant="outline" onClick={check.refresh}>
            {t("common.tryAgain")}
          </Button>
        </div>
      ) : (
        <div aria-busy="true">
          <Skeleton className="h-28 w-full rounded-lg" />
          <span className="sr-only" role="status">
            {t("onboarding.selfhost.checking")}
          </span>
        </div>
      )}

      {warn && (
        <div role="note" aria-labelledby="ob-selfhost-warning" className="rounded-lg border border-border p-3">
          <p id="ob-selfhost-warning" className="flex items-start gap-1.5 text-sm font-medium text-warning">
            <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
            {m.selfhost === "insufficient" ? t("onboarding.selfhost.warnInsufficient") : t("onboarding.selfhost.warnSlow")}
          </p>
          <ul className="mt-2 grid list-disc gap-1 pl-5 text-sm">
            {m.selfhost_reasons.map((r) => (
              <li key={r}>{t(`onboarding.selfhost.reasons.${r}`)}</li>
            ))}
          </ul>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <p className="min-w-0 flex-1 text-sm text-muted-foreground">{t("onboarding.selfhost.suggestCloud")}</p>
            <Button size="sm" variant="outline" onClick={onUseCloud}>
              {t("onboarding.selfhost.useCloud")}
            </Button>
          </div>
        </div>
      )}

      <section aria-labelledby="ob-selfhost-needs">
        <h3 id="ob-selfhost-needs" className="text-sm font-medium">
          {t("onboarding.selfhost.needsTitle")}
        </h3>
        <p className="mt-0.5 mb-2 text-sm text-muted-foreground">{t("onboarding.selfhost.needsDesc")}</p>
        <ul className="grid gap-2">
          <li className="rounded-lg border border-border p-3">
            <span className="block text-sm font-medium">{t("onboarding.selfhost.whisper")}</span>
            <span className="block text-sm text-muted-foreground">{t("onboarding.selfhost.whisperDesc")}</span>
          </li>
          <li className="rounded-lg border border-border p-3">
            <span className="block text-sm font-medium">{t("onboarding.selfhost.ollama")}</span>
            <span className="block text-sm text-muted-foreground">{t("onboarding.selfhost.ollamaDesc")}</span>
          </li>
        </ul>
      </section>
    </div>
  )
}

function gb(v: number | undefined): string | null {
  return v == null ? null : formatGb(v)
}

function Fact({ label, value }: { label: string; value: string | null }) {
  const { t } = useTranslation()
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium">{value ?? t("onboarding.selfhost.unknown")}</dd>
    </div>
  )
}

/** Ready (primary tint), Works but slow (warning outline), Not
 *  recommended (destructive), as the status badges elsewhere. */
function VerdictBadge({ verdict }: { verdict: MachineCheck["selfhost"] }) {
  const { t } = useTranslation()
  const label = t(`onboarding.selfhost.verdict.${verdict}`)
  if (verdict === "good") {
    return (
      <Badge variant="secondary" className="border-transparent bg-primary/15 text-primary">
        {label}
      </Badge>
    )
  }
  if (verdict === "slow") {
    return (
      <Badge variant="outline" className="border-warning/40 text-warning">
        {label}
      </Badge>
    )
  }
  return <Badge variant="destructive">{label}</Badge>
}
