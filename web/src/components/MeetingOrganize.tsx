import { useEffect, useId, useState } from "react"
import { useTranslation } from "react-i18next"

import { ProjectPicker } from "@/components/Projects"
import { StatusMessage } from "@/components/StatusMessage"
import { TagEditor } from "@/components/TagEditor"
import { listMeetings, plainTags, projectOf, updateMeeting, withProject, type Meeting } from "@/lib/api"
import { refreshProjects, useProjects } from "@/lib/projects"

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))
const sentence = (s: string) => `${s.replace(/[.\s]+$/, "")}.`

/**
 * The meeting page's project picker and tag editor. Both save the full
 * tag list through PATCH /api/meetings/{id}; the project is the one
 * `project:` tag in that list (docs/plans/active/projects.md).
 */
export function MeetingOrganize({ meeting, onSaved }: { meeting: Meeting; onSaved: (m: Meeting) => void }) {
  const { t } = useTranslation()
  const projects = useProjects()
  const project = projectOf(meeting.tags)
  const tags = plainTags(meeting.tags)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<{ tone: "success" | "error"; text: string } | null>(null)
  const [allTags, setAllTags] = useState<string[]>([])
  const projectLabelId = useId()
  const tagsLabelId = useId()

  // Suggestions: every plain tag used on any meeting.
  useEffect(() => {
    let cancelled = false
    listMeetings()
      .then((ms) => {
        if (cancelled) return
        const s = new Set<string>()
        for (const m of ms) for (const t of plainTags(m.tags)) s.add(t)
        setAllTags([...s].sort((a, b) => a.localeCompare(b)))
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  const save = async (next: string[], done: string) => {
    if (saving) return
    setSaving(true)
    setStatus(null)
    try {
      const m = await updateMeeting(meeting.id, { tags: next })
      onSaved(m)
      setStatus({ tone: "success", text: done })
      setAllTags((prev) => [...new Set([...prev, ...plainTags(next)])].sort((a, b) => a.localeCompare(b)))
      void refreshProjects()
    } catch (err) {
      setStatus({ tone: "error", text: t("meetingOrganize.saveError", { error: sentence(errorText(err)) }) })
    } finally {
      setSaving(false)
    }
  }

  const changeProject = (name: string) => {
    if (name === project) return
    void save(withProject(meeting.tags, name), name ? t("meetingOrganize.movedTo", { name }) : t("meetingOrganize.removedFromProject"))
  }

  const changeTags = (next: string[]) => {
    const added = next.length > tags.length
    void save(project ? withProject(next, project) : next, added ? t("meetingOrganize.tagAdded") : t("meetingOrganize.tagRemoved"))
  }

  return (
    <div className="mt-3 grid gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <span id={projectLabelId} className="w-14 shrink-0 text-xs font-medium text-muted-foreground">
          {t("meetingOrganize.project")}
        </span>
        <ProjectPicker
          value={project}
          projects={projects}
          onChange={changeProject}
          saving={saving}
          labelledBy={projectLabelId}
        />
      </div>
      <div className="flex flex-wrap items-start gap-2">
        <span id={tagsLabelId} className="w-14 shrink-0 pt-1.5 text-xs font-medium text-muted-foreground">
          {t("meetingOrganize.tags")}
        </span>
        <div className="min-w-0 flex-1">
          <TagEditor
            tags={tags}
            suggestions={allTags}
            onChange={changeTags}
            busy={saving}
            labelledBy={tagsLabelId}
          />
        </div>
      </div>
      <StatusMessage tone={status?.tone ?? "muted"}>
        {saving ? t("meetingOrganize.saving") : status?.text}
      </StatusMessage>
    </div>
  )
}
