import { SegmentedControl, SettingRow, SettingsSection, descId, labelId } from "@/components/settings/pieces"
import { useTheme, type ThemePref } from "@/lib/theme"

const THEME_OPTIONS: { value: ThemePref; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
]

/** Theme choice. Device-local (localStorage), so no config save or
 *  SaveStatus: the change applies instantly and can't fail visibly. */
export function SettingsAppearance() {
  const { pref, setPref } = useTheme()
  const id = "appearance-theme"
  return (
    <SettingsSection title="Appearance">
      <SettingRow
        id={id}
        label="Theme"
        description="Stored on this device only. System follows your OS light or dark setting."
        control={
          <SegmentedControl
            id={id}
            value={pref}
            onChange={setPref}
            options={THEME_OPTIONS}
            labelledBy={labelId(id)}
            describedBy={descId(id)}
          />
        }
      />
    </SettingsSection>
  )
}
