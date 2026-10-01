import i18next from "i18next"

/**
 * Copies text that may still be loading. WebKit (Safari, and the macOS
 * desktop's WKWebView) only allows a clipboard write inside the click
 * that asked for it, which an awaited fetch would end. So the desktop
 * copies through the Wails runtime (Go writes the clipboard, no gesture
 * needed), and browsers get a ClipboardItem built synchronously from the
 * still-pending text. Call it before any await in the click handler.
 */
export async function copyText(text: Promise<string>): Promise<void> {
  const setText = window.runtime?.ClipboardSetText
  if (typeof setText === "function") {
    if (!(await setText(await text))) throw new Error(i18next.t("errors.clipboard"))
    return
  }
  if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
    const blob = text.then((t) => new Blob([t], { type: "text/plain" }))
    await navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })])
    return
  }
  if (!navigator.clipboard) throw new Error(i18next.t("errors.clipboard"))
  await navigator.clipboard.writeText(await text)
}
