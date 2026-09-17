# UI Brainstorm — Chipmunk Metaphor

- Status: reference (not a decision record)
- Date: 2026-09-17
- Source: brainstorm via Perplexity research (pplx)

## Core metaphor system

| Meeting feature | Chipmunk metaphor | UI interpretation |
|---|---|---|
| Live audio capture | Filling cheek pouches | Live buffer showing incoming audio/transcript/translation before permanent storage |
| Meeting archive | Burrow / winter stash | Structured library of past meetings, grouped by project, team, or season |
| Search and retrieval | Finding a buried nut | Results show the exact "nut": quote, decision, or timestamp |
| Action items | Nuts to crack later | Tasks extracted from meetings with owner, due date, status |
| Knowledge base | Knowledge stash | Durable, reusable facts and decisions accumulated across meetings |
| Markdown/frontmatter | Nut label / stash tag | Machine-readable metadata on every meeting record |
| MCP tools for agents | Burrow entrances / tunnels | Controlled pathways for agents to retrieve or update context |
| Translation | Acorn shell / language layer | Language toggle changing transcript while preserving original |

Product principle: **Catch the conversation, stash the context, retrieve
the useful part.** (Pairs with the tagline: "Catch the meeting. Keep the
mind.")

## The Pouch (live meeting view) — strongest concept

The cheek pouch maps naturally to temporary storage:

- Waveform shaped subtly like two rounded cheek pouches.
- Current speaker + real-time transcript; translation as secondary line.
- Buffer states: "Pouch open" → "Holding 18 seconds" → "Ready to stash" →
  "Saved to burrow". Avoid literal "% full" unless there is a real
  technical limit.
- "Stash this nut": click a transcript line to pin a key moment; hotkey
  saves the last 10–30s as a highlight.
- Loading animation: chipmunk runs across the bottom carrying a tiny
  acorn; each processing step (transcribed → translated → saved) moves
  the acorn toward the burrow entrance. Short and cancellable.

Microcopy: "Your pouch is empty. Start talking to fill it." / "Catch this
moment" / "Keep this nut" / "Sorting the conversation…" / "Saved safely
in the burrow".

## The Burrow (archive)

- Left rail: Live pouch / Freshly buried / My burrows (projects) / Loose
  nuts (uncategorized) / Knowledge stash.
- Meeting = "stash card": date, title, participants, duration, languages,
  counts of actions/decisions, a tiny acorn-density indicator.
- "Burrow" is the branded label; tooltip/secondary label says
  "Projects"/"Collections". Metaphor must not obscure the IA.

## Golden rule for terminology

**Use the metaphor for verbs and affordances; use conventional nouns for
records and governance.**

| Concept | Term | Notes |
|---|---|---|
| Recorded meeting | Meeting | "catch" only as secondary verb |
| Start recording | Record a meeting | not "Start a catch" |
| Speech-to-text output | Transcript | never "stash" |
| Important moment | Highlight | not "nibble" |
| Temporary recent audio | Live buffer | technical, explain once |
| Saved excerpt/insight | Stash | "Stash highlight" works |
| Long-term storage | Archive | familiar |
| Task from meeting | Action item | keep standard |
| Settled conclusion | Decision | keep standard |
| Reusable insight | Knowledge card | |
| Search results | Matches | "Search the stash" as copy only |
| AI workflow | Agent | standard term |
| Meeting collection | Workspace (label), Burrow (branding) | |

✅ "Stash this highlight", "Dig up". ❌ replacing Transcript/Decision/
Attendees with metaphor words.

## Empty states (warm, not childish)

- No meetings: "Your burrow is quiet." → *Start a meeting to capture the
  conversation, decisions, and follow-ups in one place.*
- No highlights: "No highlights yet." → *CatchM will surface important
  moments as your meeting develops.*
- No action items: "No loose ends."
- No search results: "No matches in the stash."
- First result: "Nice catch. We found 3 highlights, 2 action items, and 1
  decision. Review them before sharing." — AI output presented as
  reviewable suggestions, not facts.

## Icons and palette

- Lucide-style outline icons, 1.75–2px strokes in `#6C3524`, rounded
  caps. No filled cartoon icons. Mascot appears only in onboarding,
  empty states, occasional status moments.
- Mapping: Record=`Mic` (+cream-on-brown dot), Live buffer=`Radio`,
  Stash=`BookmarkPlus` (nut accent on selected state), Archive=`Archive`,
  Action=`CheckSquare`/`ArrowRightCircle`, Decision=`GitBranch`/
  `CheckCircle2`, Knowledge=`Network`/`BookOpen`, Agent=`Bot` (+`Plug`
  for MCP).
- Colors: primary `#6C3524`, canvas `#FBF7EA`, secondary muted brown
  `#A9826D`. Semantic states (green done, amber uncertain, red error)
  stay separate from the brand palette.

## Successful precedents

| Product | Metaphor | Why it works |
|---|---|---|
| Obsidian | Stone / "vault" | Quiet, intellectual; value from durable files and links, not mascots |
| Bear | Bear companion | Name gives warmth; UI stays minimalist, typography-led |
| Forest | Tree grows while focusing | Metaphor maps directly to behavior; reward optional |
| Habitica | RPG for habits | Explicit gamification for users who opt in; habit model stays legible |
| Pinterest | Pin/board | Metaphor explains the information architecture, not decoration |

Common thread: the metaphor explains structure; whimsy is a light layer.

## Pitfalls to avoid

1. **Tone mismatch** — playfulness reads as immaturity in
   enterprise/legal/compliance contexts.
2. **Metaphor leakage** — never obscure "Actions", "Decisions",
   "Transcript" labels; users must not decode the metaphor to understand
   function.
3. **Garnish syndrome** — mascot appearing inconsistently (onboarding
   only) feels like clip art; erodes brand trust.
4. **Over-anthropomorphism** — chatty/emotive mascot behavior feels
   manipulative; never let the character promise capabilities the system
   can't deliver.
5. **Motion fatigue** — continuous animations drain attention/battery;
   provide a calm mode that disables nonessential motion.
6. **Accessibility** — text-first explanations; allow hiding mascot
   animations; screen-reader users don't get anthropomorphic cues.
7. **Infantilization** — balance warmth with competence signals: clear
   typography, restrained motion, sober accents.

Rule of thumb: the mascot should serve clarity and motivation, not become
the product.

## Wireframe sketches (ASCII)

Kept in the original research output; to be referenced when building the
web UI (M1). Five layouts: (A) live pouch view, (B) burrow archive,
(C) search "dig up nuts", (D) meeting detail with highlights/actions/
decisions, (E) agents/MCP settings — all desktop web, left rail nav.
