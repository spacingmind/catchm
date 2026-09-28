# Plan: Meeting insights — evidence, mind map, flows, knowledge graph

- Status: approved 2026-09-28; P0 in progress (branch `feat/meeting-insights-p0`)
- Created: 2026-09-28
- Branch: `feat/meeting-insights`
- References:
  - `../../scopedocs/Understand-Anything` (MIT). Borrowed:
    - its schema and tolerant validator (`packages/core/src/schema.ts`)
    - the "deterministic base graph + LLM enrichment" split
    - the domain→flow→step view and the force-graph worker
  - The pplx research from 2026-09-28 on small summarization models.

## Why

A summary alone doesn't capture how a meeting fits together. Users want
four things:
1. A **mind map** of what was discussed.
2. **Use-case / business flows** when a meeting walks through a process.
3. A **knowledge graph** that links meetings through people, topics,
   decisions, projects and terms.
4. Output they can trust: every decision and action points back to the
   sentence it came from.

## Decisions (brainstormed 2026-09-28; the user delegated the choices)

1. **Where the data lives: a sidecar file, `analysis.json`, next to
   `meeting.md`.**
   - It holds the canonical, richer output: topics, evidence, canonical
     keys, mind map and flows.
   - The frontmatter keeps today's fields (summary, decisions,
     action_items), so the MCP tools, sync and old clients keep working.
   - Action-item `status` stays in the frontmatter, because it is user
     state.
2. **Evidence on everything.** Each topic, decision, action and flow step
   carries `evidence: [seq]` pointing at utterances. The validator drops
   seqs that don't exist. In the UI a click scrolls the transcript to the
   original line, so a Vietnamese meeting always shows Vietnamese
   evidence.
3. **Mind map is derived, not generated.** The tree is:
   - root: the meeting
   - one branch per topic
   - under each topic: its decisions, actions and notes

   The LLM only assigns items to topics. This costs no extra pass, the
   map always agrees with the summary, and it can be rebuilt from
   `analysis.json`.
4. **Flows are a separate pass, run on demand.** The user presses
   "Find flows" on the Flows tab.
   - Most meetings contain no process, and adding flows to the summary
     schema would hurt small (4–9B) models.
   - Rules borrowed from `domain-analyzer.md`: don't invent flows; 3–10
     steps; an explicit `order`; real `next` edges with an optional
     `condition` (branches). UA hid step order in edge weights; we don't.
5. **Language: read the original transcript by default; English keys for
   linking.**
   - The multilingual default model (qwen3.5) reads Vietnamese directly
     and writes display text in the meeting's language.
   - Every topic, entity and person also gets a canonical English `key`
     (e.g. `topic:pricing-page`). Vietnamese and English meetings then
     link in the graph without translating whole transcripts.
   - **Optional "Analyze in English" mode**, for English-only models such
     as LFM2-2.6B-Transcript:
     - Utterances that have no English text are translated once with the
       batch translate model.
     - The translations are cached in a sidecar, `transcript.en.jsonl`
       (`{src: "live"|"v2", seq, en}`). A sidecar is needed because
       `utterances.jsonl` is append-only.
     - Display text is translated back afterwards.
   - The mode turns on automatically for presets marked English-only. The
     eval harness (decision 10) decides whether it ever becomes a
     default.
6. **The graph is a derived cache; user decisions are the only new truth.**
   - Everything under `<store>/graph/`:
     - `graph.json`: derived and rebuildable. It is built from every
       meeting's `analysis.json` plus the files below.
     - `entities.json`: user-confirmed merges, aliases and renames. This
       is the only data that can't be derived.
     - `links.json`: a cache of LLM cross-meeting edges, keyed by the
       meeting pair and the input hash. It is derivable but expensive.
   - There is no SQLite graph. FTS5 stays for search.
7. **Entity resolution: auto-merge only on exact keys; suggest the rest.**
   - Automatic merges only for exact `key` matches, speaker labels, and
     aliases in `entities.json`.
   - Fuzzy candidates (normalized-token overlap, edit distance, and
     optionally Ollama `embeddinggemma` similarity) appear as
     **"Suggested merges"** for the user to confirm.
   - UA only does exact-name dedup, so this part is ours.
8. **Cross-meeting links: a conservative LLM pass on candidates only.**
   - Candidates for a new meeting's decisions and topics are the top-k
     earlier items that share a key or score well in FTS.
   - The LLM labels each candidate pair `follows_up`, `supersedes`,
     `contradicts` or `relates_to`, with evidence and a weight, or
     returns none.
   - It runs on demand ("Link to earlier meetings") or after a summary is
     created. It sends summaries, not transcripts. The CatchM Cloud
     consent copy covers it.
9. **Rendering stack (new web dependencies, all permissive):**
   - `@xyflow/react` 12 (MIT) for nodes and edges
   - `d3-hierarchy` (ISC) for the mind-map tree layout
   - `@dagrejs/dagre` (MIT) for the left-to-right flow layout
   - `d3-force` (ISC), in a Web Worker, for the knowledge graph
   - `graphology` + `graphology-communities-louvain` (MIT) for community
     colours
   - `elkjs` is **not** used, because of its EPL-2.0 licence.
10. **Eval harness before any model change.**
    - Command: `catchm eval`, or a Go test behind a build tag.
    - Gold files: `{transcript, decisions[], actions[{text, owner, due}]}`.
    - Scores: JSON validity, decision recall/precision, owner accuracy,
      due accuracy, evidence validity, and unsupported items.
    - The committed gold set is synthetic, in English and Vietnamese.
      Real transcripts stay local in a gitignored folder.
11. **Port UA's tolerant validator to Go.** Sanitize (null→empty,
    lowercase enums), map aliases (`decision→claim`-style synonyms, plus
    Vietnamese labels), auto-fix defaults, drop dangling references, and
    keep an issues log in `analysis.json`.

## Contract: `analysis.json` (v1)

```json
{
  "version": 1,
  "generated": { "at": "RFC3339", "provider": "ollama", "model": "qwen3.5:9b",
                 "transcript": "v2|live", "analysis_lang": "vi|en" },
  "summary": "string (meeting language)",
  "topics":   [{ "id": "t1", "title": "…", "key": "topic:pricing-page", "evidence": [12, 40] }],
  "decisions":[{ "id": "d1", "text": "…", "topic": "t1", "scope": "…?", "evidence": [41] }],
  "actions":  [{ "id": "a1", "text": "…", "topic": "t1", "owner": "person:long?", "due": "YYYY-MM-DD?",
                 "due_text": "thứ Sáu tuần sau?", "evidence": [44] }],
  "people":   [{ "key": "person:long", "name": "Long", "aliases": ["anh Long"], "speaker": "S1?" }],
  "entities": [{ "key": "project:catchm-cloud", "name": "CatchM Cloud",
                 "kind": "project|product|org|tool|term|place", "evidence": [7] }],
  "flows":    [{ "id": "f1", "name": "…", "actor": "…?", "trigger": "…?", "evidence": [60],
                 "steps": [{ "id": "s1", "order": 1, "text": "…", "actor": "…?",
                             "next": [{ "to": "s2", "condition": "…?" }] }] }],
  "issues":   ["auto-corrected: …"]
}
```

- **`flows`:** empty until the user runs the flows pass.
- **Mind map:** computed from `topics`, `decisions` and `actions`.
- **Frontmatter:** `summary`, `decisions` and `action_items` are written
  from this file, in index order, keeping existing statuses where the
  text matches.

## Contract: graph (derived)

- **Node types:** `meeting | person | topic | decision | action | entity`.
- **Deterministic edges:**
  - `attended` (meeting→person)
  - `discussed` (meeting→topic)
  - `decided` (meeting→decision)
  - `owns` (person→action)
  - `mentions` (meeting→entity)
  - `in_topic` (decision/action→topic)
- **LLM edges:** `follows_up | supersedes | contradicts | relates_to`,
  each with `weight`, `evidence` and `from_meeting`/`to_meeting`.
- **API:**
  - `GET /api/graph`: nodes and edges, optionally filtered by
    type/person/date.
  - `GET /api/meetings/{id}/related`
  - `GET /api/graph/suggestions`, `POST /api/graph/merge`,
    `POST /api/graph/unmerge`
  - `POST /api/meetings/{id}/flows`, `POST /api/meetings/{id}/links`
- **MCP tool surface:** `get_related_meetings`, `query_graph`.

## P0 Go contract (fixed; both P0 workstreams code against these names)

**Package `internal/summarize`:**
- **`Extraction` stays** as the old four-field core: summary,
  participants, decisions, action_items. It is still what gets written
  to the frontmatter.
- **New file `analysis.go`** defines the types for `analysis.json` v1,
  exactly as in the "Contract: `analysis.json` (v1)" section above:
  - `Analysis{Version, Generated GeneratedInfo, Summary, Topics []Topic,
    Decisions []AnalysisDecision, Actions []AnalysisAction,
    People []Person, Entities []Entity, Flows []Flow, Issues []string}`
  - The field names and JSON tags are the snake_case names used in that
    section.
- **`Summarizer` becomes
  `Summarize(ctx, Input) (Analysis, error)`**, and every provider returns
  an Analysis:
  - **Ollama and OpenAI** use the richer prompt and schema: topics,
    canonical keys, people, entities, and `evidence: [seq]` on topics,
    decisions and actions.
  - **The CatchM Cloud Worker** still returns the old four-field shape.
    It is lifted into an Analysis with no topics and no evidence
    (follow-up: extend `/v1/summarize` in catchm-api).
- **`func (a Analysis) Extraction() Extraction`** projects an Analysis
  onto the frontmatter fields, keeping `ActionItem.Status` by matching
  existing text.
- **`func Validate(a *Analysis, validSeqs map[int64]bool) []string`**:
  the tolerant validator.
  - It sanitizes and maps aliases.
  - It drops evidence seqs that aren't in the transcript.
  - It drops references to unknown topic ids or people keys.
  - It returns the issues it logged, which are also appended to
    `a.Issues`.
- **The transcript fed to the model** gets a seq prefix on every line:
  `[12] Speaker 1: text`. That lets the model cite evidence.
- **`func BuildMindMap(a Analysis, title string) MindNode`**, where
  `MindNode{Label, Ref string, Children []MindNode}` and `Ref` is
  `"topic:t1"`, `"decision:d1"` or `"action:a1"`. The root is the
  meeting, then one branch per topic, then that topic's decisions and
  actions. Items with no topic go under an "Other" branch.

**Package `internal/meeting`:**
- `Store.WriteAnalysis(id, []byte)` and `Store.ReadAnalysis(id) ([]byte, error)`
  read and write `analysis.json`. Writes are atomic (tmp file, then
  rename). A missing file returns `ErrNoAnalysis`.

**HTTP:**
- `GET /api/meetings/{id}/analysis` returns `{analysis, mindmap}`, or
  404 `{error}` when there is no sidecar yet.
- Refine writes the sidecar, then syncs the frontmatter from it.

**Eval** (package `internal/summarize/eval`, command `catchm eval`):
- Gold file (JSON): `{title, lang, transcript: [{seq, speaker, text}],
  decisions: [text], actions: [{text, owner?, due?}]}`.
- `eval.Score(g Gold, a summarize.Analysis) Report` returns:
  - `json_valid`
  - `decision_recall` and `decision_precision`
  - `action_recall`
  - `owner_accuracy` and `due_accuracy`
  - `evidence_valid`: the share of items whose evidence seqs exist and
    whose text shares keywords with the item
  - `unsupported`: items with no evidence
- Matching is fuzzy: normalized token-set overlap ≥ 0.5.
- Command: `catchm eval [--dir testdata/eval] [--provider ollama|openai|catchm-cloud]`,
  reading the provider from the user's config unless flags override it.
  It prints a per-file and aggregate table.
- The synthetic gold set lives in `internal/summarize/eval/testdata/`:
  4–6 short meetings, English and Vietnamese, including one with a
  process flow and one with no decisions. Real transcripts go in a
  gitignored `eval-local/` directory.

## Phases

- **P0 — foundation** (backend, one workstream):
  - the `analysis.json` schema and writer
  - the Go port of the tolerant validator
  - the summary prompt extended with topics, keys, people, entities and
    evidence
  - frontmatter sync
  - the derived mind-map builder
  - the eval harness with the synthetic EN/VI gold set
- **P1 — per-meeting views.** Two parallel workstreams after P0:
  - **P1a, UI:** Mind map tab, transcript evidence jump, Flows tab.
  - **P1b, backend:** the flows pass, its endpoint and validation.
- **P2 — knowledge graph.** Two parallel workstreams:
  - **P2a, backend:** graph builder, entity suggestions, merges, the
    links pass, and the API and MCP tools.
  - **P2b, UI:**
    - a "Knowledge" sidebar view with a force graph, communities,
      filters and focus mode
    - a "Related meetings" panel on the meeting page
    - a "Suggested merges" review
- **P3 — optional:**
  - "Analyze in English" mode with the `transcript.en.jsonl` sidecar
  - an LFM2-2.6B-Transcript preset, only if the eval shows a gain

## Acceptance criteria

1. **Summary:** `Create summary & action items` writes a valid
   `analysis.json`. Every decision and action has ≥1 evidence seq that
   exists. The frontmatter still matches the old shape, and old meetings
   without the sidecar still render.
2. **Mind map:** the tab renders a meeting's mind map from the sidecar
   with no extra LLM call. Clicking a leaf jumps to its evidence line in
   the Transcript tab.
3. **Flows:** "Find flows" returns zero flows for a meeting with no
   process (no invented flows). For a process meeting it returns ordered
   steps with branches, rendered left→right.
4. **Graph linking:** two meetings, one Vietnamese and one English, that
   discuss the same topic or project share one graph node through its
   English `key`.
5. **Suggested merges:** suggestions appear for near-duplicate people
   and entities. Confirming one updates `entities.json`, and a rebuild
   keeps it.
6. **Related meetings:** the panel lists earlier meetings through shared
   keys and LLM links, each with its evidence.
7. **Eval:** `catchm eval` prints per-metric scores for a configured
   provider and model on the gold set.
8. **Checks:** `task test`, `task lint`, `bunx tsc -b`, `bun run build`
   and oxlint are clean. The new dependencies are the permissive ones
   listed in decision 9.
