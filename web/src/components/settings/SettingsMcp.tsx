import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { Check, Copy, RefreshCw } from "lucide-react"
import { Trans, useTranslation } from "react-i18next"
import { cn } from "cn"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import { descId, SaveStatus, SegmentedControl, SettingRow, SettingsSection } from "@/components/settings/pieces"
import { useSaveStatus } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { getMcp, regenerateMcpToken, updateMcp, type McpSettings, type UpdateMcpRequest } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import { noAutofill } from "@/lib/inputProps"

type McpApp = "claude-desktop" | "claude-code" | "cursor"

const APPS: { value: McpApp; label: string }[] = [
  { value: "claude-desktop", label: "Claude Desktop" },
  { value: "claude-code", label: "Claude Code" },
  { value: "cursor", label: "Cursor" },
]

const PORT_ERROR_ID = "mcp-port-error"
const MASK = "•".repeat(24)

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** The `mcpServers` config Claude Desktop, Cursor and most URL clients
 *  read. JSON.stringify escapes Windows backslashes and quotes. */
function mcpServersJson(entry: Record<string, unknown>): string {
  // One-item arrays (args) stay on one line: ["mcp"].
  return JSON.stringify({ mcpServers: { catchm: entry } }, null, 2).replace(
    /\[\n\s+("(?:[^"\\]|\\.)*")\n\s+\]/g,
    "[$1]",
  )
}

/** Double-quotes a path for a terminal (it may contain spaces). POSIX
 *  shells still expand \ $ ` and " inside double quotes, so those are
 *  escaped; a Windows path can't contain a quote and keeps its
 *  backslashes as they are. */
function shellQuote(path: string): string {
  if (/^[A-Za-z]:[\\/]|^\\\\/.test(path)) return `"${path}"`
  return `"${path.replace(/(["\\$`])/g, "\\$1")}"`
}

function Code({ children }: { children?: ReactNode }) {
  return (
    <code translate="no" className="rounded bg-muted px-1 font-mono text-xs break-words text-foreground">
      {children}
    </code>
  )
}

const CODE = { code: <Code /> }

/** Copy-to-clipboard with a short-lived "Copied" (or a lasting error). */
function useCopy() {
  const [result, setResult] = useState<"copied" | "failed" | null>(null)
  const timer = useRef<number | null>(null)

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
    },
    [],
  )

  const copy = (text: string) => {
    if (timer.current !== null) window.clearTimeout(timer.current)
    setResult(null)
    copyText(Promise.resolve(text)).then(
      () => {
        setResult("copied")
        timer.current = window.setTimeout(() => setResult(null), 2000)
      },
      () => setResult("failed"),
    )
  }

  return { result, copy }
}

type CopyResult = ReturnType<typeof useCopy>["result"]

/** Copy button: reads "Copied" for a moment after a copy. */
function CopyButton({
  result,
  onClick,
  variant = "outline",
  name,
}: {
  result: CopyResult
  onClick: () => void
  variant?: "outline" | "ghost"
  /** What's copied, for the accessible name ("Copy …"). */
  name: string
}) {
  const { t } = useTranslation()
  return (
    <Button size="sm" variant={variant} onClick={onClick} aria-label={t("settings.mcp.copyNamed", { name })}>
      {result === "copied" ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      {result === "copied" ? t("settings.mcp.copied") : t("settings.mcp.copy")}
    </Button>
  )
}

/** Announces a copy; only a failure is visible (the button shows "Copied"). */
function CopyStatus({ result, className }: { result: CopyResult; className?: string }) {
  const { t } = useTranslation()
  return (
    <StatusMessage tone="error" className={result === "copied" ? "sr-only" : className}>
      {result === "copied" ? t("settings.mcp.copied") : result === "failed" ? t("settings.mcp.copyFailed") : null}
    </StatusMessage>
  )
}

/**
 * A copyable setup snippet: caption + Copy over a scrollable code block.
 * `display` is what's shown when it differs from what's copied (a
 * masked token).
 */
function CodeBlock({ caption, code, display }: { caption: string; code: string; display?: string }) {
  const { result, copy } = useCopy()
  return (
    <div className="grid gap-1.5">
      <div className="overflow-hidden rounded-lg border border-border">
        <div className="flex items-center gap-3 border-b border-border bg-muted/50 py-1 pr-1 pl-3">
          <span translate="no" className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
            {caption}
          </span>
          <CopyButton result={result} onClick={() => copy(code)} variant="ghost" name={caption} />
        </div>
        <pre translate="no" className="overflow-x-auto p-3 font-mono text-xs leading-relaxed">
          <code>{display ?? code}</code>
        </pre>
      </div>
      <CopyStatus result={result} />
    </div>
  )
}

/** A setting row whose control is an on/off switch. */
function SwitchRow({
  id,
  label,
  description,
  checked,
  disabled,
  onCheckedChange,
}: {
  id: string
  label: string
  description: ReactNode
  checked: boolean
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <SettingRow
      label={label}
      htmlFor={id}
      description={description}
      control={
        <Switch
          id={id}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onCheckedChange}
          aria-describedby={descId(id)}
        />
      }
    />
  )
}

function withPatch(m: McpSettings, p: UpdateMcpRequest): McpSettings {
  return {
    ...m,
    enabled: p.enabled ?? m.enabled,
    allow_writes: p.allow_writes ?? m.allow_writes,
    http: { ...m.http, enabled: p.http_enabled ?? m.http.enabled, port: p.port ?? m.http.port },
  }
}

/**
 * Settings → MCP: the local MCP server that lets AI apps search and
 * read meetings. stdio clients run `<stdio.command> mcp` themselves;
 * the optional local HTTP listener serves URL-only clients while the
 * app is open. Every change is saved at once (PUT /api/mcp is partial).
 */
export function SettingsMcp() {
  const { t } = useTranslation()
  const [mcp, setMcp] = useState<McpSettings | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [app, setApp] = useState<McpApp>("claude-desktop")
  const [portInput, setPortInput] = useState("")
  const [portError, setPortError] = useState<string | null>(null)
  const [showToken, setShowToken] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [regenerating, setRegenerating] = useState(false)
  const [regenError, setRegenError] = useState<string | null>(null)
  const save = useSaveStatus()
  const httpSave = useSaveStatus()
  const tokenCopy = useCopy()
  // A slower, older response must not overwrite a newer one.
  const seq = useRef(0)

  const load = useCallback(() => {
    getMcp()
      .then(setMcp)
      .catch((err: unknown) => setLoadError(errorText(err)))
  }, [])

  useEffect(load, [load])

  // The port field follows the saved port (first load, a reverted save).
  const savedPort = mcp?.http.port
  const [prevPort, setPrevPort] = useState<number | undefined>(undefined)
  if (savedPort !== prevPort) {
    setPrevPort(savedPort)
    if (savedPort !== undefined) setPortInput(String(savedPort))
  }

  const apply = (patch: UpdateMcpRequest, status: ReturnType<typeof useSaveStatus>) => {
    if (!mcp) return
    const before = mcp
    const id = ++seq.current
    setMcp(withPatch(mcp, patch))
    void status
      .run(async () => {
        const next = await updateMcp(patch)
        if (id === seq.current) setMcp(next)
      }, t("settings.mcp.saveError"))
      .then((ok) => {
        if (ok || id !== seq.current) return
        // Show what's actually saved; if even that fails, undo.
        getMcp().then(
          (m) => id === seq.current && setMcp(m),
          () => id === seq.current && setMcp(before),
        )
      })
  }

  const commitPort = () => {
    if (!mcp) return
    const v = portInput.trim()
    const n = Number(v)
    if (!/^\d+$/.test(v) || n < 1024 || n > 65535) {
      setPortError(t("settings.mcp.portError"))
      return
    }
    setPortError(null)
    if (n !== mcp.http.port) apply({ port: n }, httpSave)
  }

  const newToken = async () => {
    setRegenerating(true)
    setRegenError(null)
    seq.current++
    try {
      setMcp(await regenerateMcpToken())
      setConfirmOpen(false)
    } catch {
      setRegenError(t("settings.mcp.newTokenError"))
    } finally {
      setRegenerating(false)
    }
  }

  if (loadError) {
    return (
      <SettingsSection title={t("settings.nav.mcp")} description={t("settings.mcp.desc")}>
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">{t("settings.mcp.loadError", { error: loadError })}</StatusMessage>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setLoadError(null)
              load()
            }}
          >
            {t("common.tryAgain")}
          </Button>
        </div>
      </SettingsSection>
    )
  }

  if (!mcp) {
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">{t("settings.mcp.loading")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    )
  }

  const off = !mcp.enabled
  const command = mcp.stdio.command || "catchm"
  const args = mcp.stdio.args.length > 0 ? mcp.stdio.args : ["mcp"]
  const stdioJson = mcpServersJson({ command, args })
  const claudeCode = `claude mcp add catchm -- ${shellQuote(command)} ${args.join(" ")}`

  const { http } = mcp
  const url = http.url || `http://127.0.0.1:${http.port}/mcp`
  const urlJson = (token: string) => mcpServersJson({ url, headers: { Authorization: `Bearer ${token}` } })
  const httpStatus = http.running ? (
    <Trans i18nKey="settings.mcp.httpRunning" values={{ url: http.url }} components={CODE} />
  ) : http.error ? (
    <span className="text-destructive">{t("settings.mcp.httpError", { error: http.error })}</span>
  ) : http.enabled ? null : (
    t("settings.mcp.httpOff")
  )

  return (
    <div>
      <SettingsSection title={t("settings.nav.mcp")} description={t("settings.mcp.desc")}>
        <SwitchRow
          id="mcp-enabled"
          label={t("settings.mcp.enabledLabel")}
          description={t("settings.mcp.enabledDesc")}
          checked={mcp.enabled}
          onCheckedChange={(enabled) => apply({ enabled }, save)}
        />
        <div className={cn(off && "opacity-50")}>
          <SwitchRow
            id="mcp-writes"
            label={t("settings.mcp.writesLabel")}
            description={t("settings.mcp.writesDesc")}
            checked={mcp.allow_writes}
            disabled={off}
            onCheckedChange={(allow_writes) => apply({ allow_writes }, save)}
          />
        </div>
        <div className="flex items-center justify-end">
          <SaveStatus state={save.save.state} error={save.save.error} />
        </div>
      </SettingsSection>

      {/* Nothing below works while MCP is off: dimmed and inert (out of
          the tab order and the accessibility tree) until it's on. */}
      <div inert={off} className={cn(off && "opacity-50")}>
        <SettingsSection title={t("settings.mcp.connectTitle")} description={t("settings.mcp.connectDesc")}>
          <div className="grid gap-3 pt-2">
            <SegmentedControl id="mcp-app" label={t("settings.mcp.appLabel")} value={app} onChange={setApp} options={APPS} />
            {app === "claude-desktop" && (
              <>
                <p className="text-sm text-muted-foreground">
                  <Trans i18nKey="settings.mcp.claudeDesktopSteps" components={CODE} />
                </p>
                <CodeBlock caption="claude_desktop_config.json" code={stdioJson} />
                <p className="text-sm text-muted-foreground">
                  <Trans i18nKey="settings.mcp.claudeDesktopWhere" components={CODE} />
                </p>
              </>
            )}
            {app === "claude-code" && (
              <>
                <p className="text-sm text-muted-foreground">{t("settings.mcp.claudeCodeSteps")}</p>
                <CodeBlock caption={t("settings.mcp.terminal")} code={claudeCode} />
                <p className="text-sm text-muted-foreground">
                  <Trans i18nKey="settings.mcp.claudeCodeScope" components={CODE} />
                </p>
              </>
            )}
            {app === "cursor" && (
              <>
                <p className="text-sm text-muted-foreground">
                  <Trans i18nKey="settings.mcp.cursorSteps" components={CODE} />
                </p>
                <CodeBlock caption=".cursor/mcp.json" code={stdioJson} />
              </>
            )}
          </div>
        </SettingsSection>

        <section className="mb-8">
          <Disclosure label={t("settings.mcp.httpDisclosure")} defaultOpen={http.enabled}>
            <p className="text-sm text-muted-foreground">{t("settings.mcp.httpDesc")}</p>
            <SwitchRow
              id="mcp-http"
              label={t("settings.mcp.httpLabel")}
              description={httpStatus}
              checked={http.enabled}
              onCheckedChange={(http_enabled) => apply({ http_enabled }, httpSave)}
            />
            <SettingRow
              label={t("settings.mcp.portLabel")}
              htmlFor="mcp-port"
              description={t("settings.mcp.portDesc")}
              control={
                <div className="grid w-40 justify-items-end gap-1">
                  <Input
                    id="mcp-port"
                    name="mcp_port"
                    type="text"
                    inputMode="numeric"
                    {...noAutofill}
                    value={portInput}
                    onChange={(e) => setPortInput(e.target.value)}
                    onBlur={commitPort}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") commitPort()
                      if (e.key === "Escape") {
                        setPortInput(String(http.port))
                        setPortError(null)
                      }
                    }}
                    aria-invalid={portError ? true : undefined}
                    aria-describedby={portError ? `${descId("mcp-port")} ${PORT_ERROR_ID}` : descId("mcp-port")}
                    className="w-28 text-right tabular-nums"
                  />
                  <StatusMessage id={PORT_ERROR_ID} tone="error" className="text-right">
                    {portError}
                  </StatusMessage>
                </div>
              }
            />
            {http.enabled && http.token && (
              <>
                <SettingRow
                  label={t("settings.mcp.tokenLabel")}
                  htmlFor="mcp-token"
                  description={t("settings.mcp.tokenDesc")}
                  control={
                    <div className="grid w-96 justify-items-end gap-1.5">
                      <Input
                        id="mcp-token"
                        type={showToken ? "text" : "password"}
                        readOnly
                        {...noAutofill}
                        value={http.token}
                        aria-describedby={descId("mcp-token")}
                        className="w-full font-mono text-xs md:text-xs"
                      />
                      <div className="flex flex-wrap justify-end gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          aria-pressed={showToken}
                          aria-controls="mcp-token"
                          onClick={() => setShowToken((s) => !s)}
                        >
                          {showToken ? t("settings.mcp.hideToken") : t("settings.mcp.showToken")}
                        </Button>
                        <CopyButton result={tokenCopy.result} onClick={() => tokenCopy.copy(http.token)} name={t("settings.mcp.tokenLabel")} />
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setRegenError(null)
                            setConfirmOpen(true)
                          }}
                        >
                          <RefreshCw aria-hidden="true" />
                          {t("settings.mcp.newToken")}
                        </Button>
                      </div>
                      <CopyStatus result={tokenCopy.result} className="text-right" />
                    </div>
                  }
                />
                <div className="grid gap-2 py-4">
                  <div>
                    <p className="text-sm font-medium">
                      {t("settings.mcp.urlSnippetLabel")}
                    </p>
                    <p className="mt-0.5 text-sm text-muted-foreground">{t("settings.mcp.urlSnippetDesc")}</p>
                  </div>
                  <CodeBlock
                    caption="mcp.json"
                    code={urlJson(http.token)}
                    display={showToken ? undefined : urlJson(MASK)}
                  />
                </div>
              </>
            )}
            <div className="flex items-center justify-end">
              <SaveStatus state={httpSave.save.state} error={httpSave.save.error} />
            </div>
          </Disclosure>
        </section>
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("settings.mcp.newTokenTitle")}
        description={t("settings.mcp.newTokenDesc")}
        confirmLabel={t("settings.mcp.newTokenConfirm")}
        pending={regenerating}
        error={regenError}
        onConfirm={() => void newToken()}
      />
    </div>
  )
}
