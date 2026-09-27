import { useCallback, useLayoutEffect, useRef, useState } from "react"

/**
 * Distance from the bottom (px) within which the view counts as "at the
 * bottom". atBottom is sampled on scroll events, i.e. before new content
 * renders, so this only needs to absorb sub-line jitter, not a new line.
 */
const DEFAULT_THRESHOLD_PX = 32

/** Whether contentKey means "nothing rendered yet" (0, "", [], null…). */
function isEmptyKey(key: unknown): boolean {
  if (Array.isArray(key)) return key.length === 0
  return key == null || key === 0 || key === "" || key === false
}

/** The element that actually scrolls for `node`: Radix ScrollArea's
 *  viewport if it's inside one, else the nearest overflow-y ancestor. */
function findScroller(node: HTMLElement): HTMLElement | null {
  const viewport = node.closest<HTMLElement>('[data-slot="scroll-area-viewport"]')
  if (viewport) return viewport
  for (let el = node.parentElement; el; el = el.parentElement) {
    const { overflowY } = getComputedStyle(el)
    if (overflowY === "auto" || overflowY === "scroll" || overflowY === "overlay") return el
  }
  return null
}

/**
 * Keeps a growing log (live transcript) pinned to its newest line while
 * the user is at the bottom, and leaves them alone once they scroll up to
 * re-read — flagging `hasNew` instead so the UI can offer a jump back.
 *
 * Put `ref` on an element inside the scroll container (typically an
 * empty sentinel after the last row). It's a callback ref, so it
 * re-attaches whenever that element remounts (e.g. leaving compact mode
 * swaps the whole view); a fresh element has no scroll position worth
 * keeping, so it lands on the bottom.
 *
 * `contentKey` changes whenever content is added (the list, or its
 * length). The first non-empty content always scrolls to the bottom;
 * after that it auto-scrolls only if the view was at the bottom. Going
 * back to empty (e.g. switching meetings) re-arms the first-content case.
 */
export function useStickToBottom(contentKey: unknown, { threshold = DEFAULT_THRESHOLD_PX }: { threshold?: number } = {}) {
  const sentinelRef = useRef<HTMLElement | null>(null)
  const scrollerRef = useRef<HTMLElement | null>(null)
  // Live flag read at the moment new content renders; the state copy
  // below only re-renders when it flips.
  const atBottomRef = useRef(true)
  const [atBottom, setAtBottom] = useState(true)
  const [hasNew, setHasNew] = useState(false)
  const hadContentRef = useRef(false)

  const markAtBottom = useCallback((next: boolean) => {
    atBottomRef.current = next
    setAtBottom(next)
    if (next) setHasNew(false)
  }, [])

  const scrollToBottom = useCallback(() => {
    const el = scrollerRef.current
    if (el) el.scrollTo({ top: el.scrollHeight })
    else sentinelRef.current?.scrollIntoView({ block: "end" })
    markAtBottom(true)
  }, [markAtBottom])

  const ref = useCallback(
    (node: HTMLElement | null) => {
      if (!node) return
      const el = findScroller(node)
      sentinelRef.current = node
      scrollerRef.current = el
      const onScroll = () => {
        if (!el) return
        const next = el.scrollHeight - el.scrollTop - el.clientHeight <= threshold
        if (next !== atBottomRef.current) markAtBottom(next)
      }
      el?.addEventListener("scroll", onScroll, { passive: true })
      scrollToBottom()
      return () => {
        el?.removeEventListener("scroll", onScroll)
        if (sentinelRef.current === node) sentinelRef.current = null
        if (scrollerRef.current === el) scrollerRef.current = null
      }
    },
    [threshold, markAtBottom, scrollToBottom],
  )

  // Layout effect: scroll before paint so a new line never flashes in
  // below the fold first.
  useLayoutEffect(() => {
    if (isEmptyKey(contentKey)) {
      hadContentRef.current = false
      return
    }
    const first = !hadContentRef.current
    hadContentRef.current = true
    if (first || atBottomRef.current) scrollToBottom()
    else setHasNew(true)
  }, [contentKey, scrollToBottom])

  // Derived rather than reset in the effect: a stale flag from the last
  // meeting is hidden while empty, and the next first-content scroll
  // clears it for real.
  return { ref, atBottom, hasNew: hasNew && !isEmptyKey(contentKey), scrollToBottom }
}
