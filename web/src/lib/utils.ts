export { cn } from "cn"

/** "1 decision" / "2 decisions" — pluralizes by appending "s" unless given an explicit plural form. */
export function pluralize(count: number, singular: string, plural: string = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`
}
