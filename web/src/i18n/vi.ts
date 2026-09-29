// Vietnamese messages. Glossary: cuộc họp = meeting, bản ghi =
// transcript/recording, tóm tắt = summary, bản dịch = translation,
// cài đặt = settings. Tone: natural, concise UI Vietnamese.
import type { Messages } from "./en"

export const vi: Messages = {
  common: {
    tryAgain: "Thử lại",
  },
  settings: {
    appearance: {
      title: "Giao diện",
      themeLabel: "Chế độ màu",
      themeDesc: "Chỉ lưu trên máy này. Chọn Hệ thống để theo chế độ sáng/tối của máy.",
      light: "Sáng",
      dark: "Tối",
      system: "Hệ thống",
      languageLabel: "Ngôn ngữ",
      languageDesc: "Lưu trong cài đặt CatchM. Chọn Hệ thống để dùng ngôn ngữ của máy.",
      languageLoadError: "Không tải được cài đặt ngôn ngữ: {{error}}",
    },
  },
}
