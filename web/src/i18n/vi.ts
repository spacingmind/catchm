// Vietnamese messages. Glossary: cuộc họp = meeting, bản ghi =
// transcript/recording, chép lời = transcribe/transcription (never
// "phiên âm"), tóm tắt = summary, bản dịch = translation, cài đặt =
// settings. Tone: natural, concise UI Vietnamese.
import type { Messages } from "./en"

export const vi: Messages = {
  common: {
    tryAgain: "Thử lại",
    cancel: "Hủy",
  },
  labels: {
    source: {
      mic: "Micrô",
      system: "Âm thanh máy tính",
      both: "Cả hai",
    },
    sourceShort: {
      mic: "Mic",
      system: "Máy tính",
      both: "Mic + máy tính",
    },
    sourceHint:
      "Họp trực tuyến (Zoom, Meet, Teams)? Chọn Cả hai để nghe cả bạn và người khác. Ở trong phòng, Micrô là đủ.",
    engine: {
      whisper: "Trên máy này",
      soniox: "Đám mây (Soniox)",
    },
    engineHint: {
      whisper: "Miễn phí và riêng tư. Cần máy chủ Whisper miễn phí chạy trên máy này.",
      soniox: "Chính xác hơn và phân biệt được người nói. Cần API key Soniox trả phí.",
    },
  },
  settings: {
    nav: {
      recording: "Ghi âm",
      transcription: "Chép lời",
      translation: "Dịch",
      summaries: "Tóm tắt",
      cloudSync: "Đồng bộ đám mây",
      appearance: "Giao diện",
      shortcuts: "Phím tắt",
      system: "Hệ thống",
      title: "Cài đặt",
      sections: "Các mục cài đặt",
    },
    pieces: {
      saving: "Đang lưu…",
      saved: "Đã lưu",
      saveError: "Không lưu được. Thử lại, hoặc khởi động lại CatchM nếu vẫn lỗi.",
      opensInBrowser: " (mở trong trình duyệt của bạn)",
    },
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
