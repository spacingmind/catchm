import type { InputHTMLAttributes } from "react"

/**
 * Spread on inputs that hold config values, not personal data (URLs,
 * hosts, model names, API keys, shortcuts): stops browser autofill,
 * spellcheck/autocapitalize, and 1Password/LastPass/Bitwarden from
 * offering to fill or save them. `{...noAutofill}` on <Input> or <input>.
 */
export const noAutofill = {
  autoComplete: "off",
  spellCheck: false,
  autoCapitalize: "off",
  "data-1p-ignore": true,
  "data-lpignore": "true",
  "data-bwignore": true,
} as const satisfies InputHTMLAttributes<HTMLInputElement> & Record<`data-${string}`, string | boolean>
