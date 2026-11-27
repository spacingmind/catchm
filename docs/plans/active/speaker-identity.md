# Plan: speaker identity — who said it, who owns it

- Status: approved 2026-11-27; phases 1–2 done, phase 3 next
- Created: 2026-11-27
- Branch: `feat/speaker-identity`
- Comes before P2 (knowledge graph) of
  [meeting-insights.md](meeting-insights.md): the graph links people, so
  it needs the right people.

## Why

Engines that separate speakers (Soniox, Deepgram, ElevenLabs v3 Live,
Speechmatics, Gladia files) label them "1, 2, 3". Those labels mean
nothing outside one meeting. Whisper and Gladia live give no labels at
all.

Today the summary (P0) asks the LLM who "Speaker N" is
(`summarize.Person.Speaker`), and export uses that. But nothing lets the
user confirm or fix it, and without labels the LLM has nothing to work
with. Action-item owners are guesses.

## Decisions (owner, 2026-11-27)

Four layers. Each has a source and a confidence, and the user has the
last word:

1. **Me vs others from the audio source.** In "Both" mode the
   microphone is the user and computer audio is the other side of the
   call. Each utterance records which input carried it (`mic` /
   `system`), for every engine including Whisper.
2. **Manual speaker naming.** On the meeting page, "Speaker 2" → a name.
   A confirmed name is final: the transcript, summary, export, MCP and
   the next summary all use it.
3. **LLM name suggestions with evidence.** They come from
   self-introductions, people addressing each other by name, and the
   participants list. The user confirms them; they're never applied
   silently.
4. **Grounded action-item owners.** An owner comes from what was said
   ("An will send it") or from the known speaker of "I'll send it".
   When neither is clear, the owner stays empty and the user assigns
   it.
5. **Guest list pasted from the calendar** (added 2026-11-27). The user
   can paste a meeting's guests (from Google Calendar, Outlook, or an
   invite email) before, during or after the meeting. The guests become
   the candidates for naming speakers and for owners.

**Storage:**
- Confirmed names live in the **frontmatter** (`speakers:`). That's user
  state, like action-item status; it syncs and MCP reads it.
- Suggestions stay in `analysis.json`.
- Guests live in the frontmatter as `guests:`. That field is separate
  from `participants`, which the summary rewrites; the summary never
  touches guests.

## Design

### 1. Audio-source attribution

- **Utterance timing.** `stt.Result` gains `Start`/`End`: audio offsets
  from the stream start. Every engine already receives them:
  - Soniox tokens: `start_ms` / `end_ms`;
  - Deepgram words: `start` / `end`;
  - ElevenLabs: words with timestamps (`include_timestamps=true`);
  - Speechmatics: `start_time` / `end_time`;
  - Gladia: `utterance.start` / `end`;
  - Whisper: the pipeline knows each chunk's boundaries.

  The pipeline stores `At = streamStart + Start` (plus `End`) instead of
  the arrival time when offsets are present. Arrival time stays the
  fallback.
- **Levels timeline.** The Mixer already measures each input per 20 ms
  tick. It appends one row per 100 ms (`t`, mic RMS, system RMS) to a
  sidecar `levels.jsonl` in the meeting folder (about 36k rows, under
  1 MB an hour). That way re-transcribe and import can attribute
  utterances afterwards too.
- **Attribution:**
  - For an utterance's audio window, compare the mic and system energy.
  - Over a set ratio (2:1 to start, recorded in the plan once tuned):
    `source: "mic"` or `"system"`.
  - Otherwise no source: overlap, or echo without headphones.
  - It's stored on `meeting.Utterance.Source` (`omitempty`, old files
    are unaffected).
- **"Me":** `config.profile.name` ("Your name") is asked in onboarding
  step 1 and editable in Settings.
  - With diarization, a speaker label whose utterances are mostly
    `mic` becomes an auto suggestion of "Me", with high confidence.
    It's shown as auto and the user can change it.
  - Without diarization, `mic` utterances show as the user's name and
    `system` ones as "Others". The latter are left for LLM suggestions,
    or for the user to label after re-transcribing with a diarizing
    engine.

### 2 + 3. Speaker names

- **Frontmatter:** `speakers: {"1": "An Nguyen", "2": "Bao Tran"}`,
  confirmed only. The key is the engine label, or "me" / "others" for
  source-only meetings.
- **`analysis.json`:** `speaker_suggestions: [{speaker, name, source:
  "llm"|"audio", confidence, evidence: [seq]}]`. The summary prompt
  already yields `people[].speaker`; this adds evidence seqs and
  confidence, and drops what the validator can't ground.
- **API:**
  - `GET /api/meetings/{id}/speakers` returns the labels with their
    utterance counts, confirmed names and suggestions;
  - `PUT /api/meetings/{id}/speakers` writes the `speakers` map.
- **MCP:**
  - `get_meeting` and `read_transcript` show the confirmed names;
  - a write tool, `set_speaker_names`, is available when "Allow changes"
    is on.
- **UI:**
  - each speaker chip on the transcript opens the naming popover. It
    offers a suggestion with "why" (jump to the evidence lines), the
    participants, the known people, and free text;
  - an "Unnamed speakers" nudge appears on the meeting page;
  - a meeting without labels gets "Re-transcribe with a speaker-aware
    engine".

### Guests

- **Frontmatter:** `guests: [{name: "An Nguyen", email: "an@acme.com"}]`.
  Both parts are optional, but at least one is set.
- **Pasting:** one parser accepts the usual copy formats:
  - `Name <email>` separated by commas, semicolons or new lines (Google
    Calendar, Outlook, mail clients);
  - bare emails, one per line;
  - names only.

  Duplicates are merged by email. When there's no name, one is guessed
  from the address (`an.nguyen@` → "An Nguyen"), shown so the user can
  fix it.
- **API:** `PUT /api/meetings/{id}/guests`.
- **MCP:** `get_meeting` returns the guests, and a write tool,
  `set_meeting_guests`, works when "Allow changes" is on.
- **UI:**
  - a "Guests" field on the meeting page, where the user pastes the list
    and then reviews it;
  - the guests feed the speaker naming popover (listed first) and the
    owner "Assign" list.
- **Privacy:** emails stay in the meeting file (cloud sync encrypts it).
  The LLM only gets guest **names**, as candidates for speakers and
  owners, never the emails.
- **Recording:** guest names also go to the engine as context terms,
  next to `participants`, so they're spelled right.

### 4. Action-item owners

- **Summary prompt:**
  - the owner must come from the text or from a named speaker;
  - each owner carries `owner_evidence: [seq]`;
  - an owner without evidence is dropped and left empty;
  - confirmed speaker names go into the prompt.
- **UI:** an empty owner shows "Assign". Owner edits already exist for
  action items; reuse them.
- **Renaming a speaker** updates owners that were derived from that
  speaker label. Owners the user typed by hand are never touched.

## Phases

1. **Timing and source:**
   - `Result.Start`/`End` for the six engines;
   - pipeline `At`/`End`;
   - Mixer `levels.jsonl`;
   - attribution (live, plus batch through the levels sidecar);
   - `profile.name`.
2. **Names:**
   - frontmatter `speakers`;
   - the speakers API;
   - the transcript naming UI;
   - names used in the transcript, summary, export and MCP;
   - frontmatter `guests`, the paste parser, the Guests field, and the
     guests used as candidates and context terms.
3. **Suggestions and owners:**
   - LLM suggestions with evidence;
   - audio "Me" suggestions;
   - grounded owners and the Assign flow.

## Acceptance criteria

1. **"Both" mode with headphones:** the user's lines are tagged `mic`
   and the call's lines `system` (≥95% on a scripted test: say lines
   alternately with a video playing).
2. **Utterance times** come from the engine's audio offsets when it
   gives them. The levels sidecar lets a re-transcribed meeting get
   sources too.
3. **Naming:**
   - naming "Speaker 2" once updates the transcript, summary owners,
     export and MCP output;
   - the next summary uses the names;
   - a hand-typed owner is never overwritten.
4. **Suggestions** always show their evidence lines, and nothing is
   applied without a click. The mic-dominant speaker is suggested as
   "Me".
5. **Guests:**
   - pasting a Google Calendar or Outlook guest list gives the right
     names and emails, with duplicates merged;
   - the guests are the first choices when naming a speaker or
     assigning an owner;
   - no email ever reaches the LLM prompt (a test checks this).
6. **Owners:** an action item with no grounded owner has none, and the
   UI offers to assign one.
7. **Compatibility:** old meetings (no sources, no `speakers`, no `guests`) render as
   before. Every new field is `omitempty`.
8. **Checks:** `task lint` and `task test` pass, with engine-offset tests
   against the fake servers.

## Progress

- **Phase 1** (timing and source), done on `feat/speaker-identity`:
  - `stt.Result.Start/End`:
    - Soniox live (token `start_ms`/`end_ms`), Deepgram (words),
      Speechmatics (results), Gladia live (`utterance.start/end`) and
      Whisper (the chunk's place in the stream);
    - ElevenLabs live, only for diarized segments: their words carry
      times. Plain commits stay untimed (`include_timestamps` is left
      off until it's tried against the real API).
  - `capture.Mixer.SetTickObserver` reports each delivered tick's
    mic/system RMS by its index in the stream. Dropped ticks aren't
    counted, so index × 20 ms is the audio offset the engine sees.
  - `internal/levels` writes `levels.jsonl` (100 ms rows, `t`/`m`/`s`)
    and attributes a span (ratio 2:1 RMS, both inputs required).
  - Utterance times:
    - live utterances get `at`/`end` from the engine offsets and
      `source` from the timeline;
    - re-transcription reads the timeline for `source`.
  - `profile.name` is in the config API, onboarding step 1 and
    Settings → Recording.
  - Not yet shown in the UI. Phase 2's speakers API resolves the
    labels ("me" → the profile name).
- **Phase 2** (names and guests), done:
  - `meeting.SpeakerKey`: engine labels reduce to their number ("Speaker
    2", "spk_2", "S2" → "2"); unlabelled lines become "me"/"others" from
    their source. `SpeakerNamer` gives names: confirmed, then the
    profile name for "me", then "Speaker N". The TS mirror is
    `lib/speakers.ts`.
  - The store owns the profile-name hook (`Store.SetMe`), so the
    summary, export, MCP and server name speakers the same way.
  - Frontmatter `speakers` and `guests`:
    - `PATCH /api/meetings/{id}` with `speakers` (merged, "" removes)
      and `guests` (replaced);
    - `GET /api/meetings/{id}/speakers` (stats, confirmed names, `me`,
      guests);
    - `POST /api/guests/parse` for the paste preview.
  - `meeting.ParseGuests` handles Google Calendar (commas, a name line
    followed by an email line), Outlook (semicolons, "Last, First"),
    bare emails (the name is guessed from the address) and plain names.
    Calendar noise ("Organizer", "optional") is dropped.
  - The summary input uses confirmed names, and the LLM prompt gets an
    "Invited:" line with guest names (never emails). CatchM Cloud gets the
    same: named lines plus `guests` (names only), which the Worker
    (catchm-api `feat/summary-guests`) turns into the same "Invited:"
    line. Older Workers ignore the field, so the two can ship in either
    order.
  - Export: confirmed name, then the analysis's guess, then a localized
    label ("Người nói 3", "Tôi").
  - MCP: `get_meeting` lists speakers and guests, and `read_transcript`
    uses names. New write tools: `set_speaker_names` and
    `set_meeting_guests` (it accepts pasted text).
  - Re-transcription passes guest names as context terms.
  - UI:
    - the speaker label over each run of lines opens a naming popover,
      with guests, the user and the summary's people as suggestions;
    - a Guests section on Overview with a "Paste guests" dialog: read
      the list, review it, then add it;
    - the live view shows "Speaker N" / "Me" labels.
  - Not yet: the "Unnamed speakers" nudge and the "Re-transcribe with a
    speaker-aware engine" hint (they belong with phase 3's suggestions).
