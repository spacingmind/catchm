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
  app: {
    skipToContent: "Bỏ qua tới nội dung",
    dismiss: "Bỏ qua",
    missingMeeting:
      "Không mở được cuộc họp đó. Có thể nó đã bị xóa; hãy chọn một cuộc họp khác trong danh sách.",
  },
  sidebar: {
    mainNav: "Điều hướng chính",
    goHome: "CatchM — đến danh sách cuộc họp",
    expand: "Mở rộng thanh bên",
    collapse: "Thu gọn thanh bên",
    meetings: "Cuộc họp",
    live: "Trực tiếp",
    actionItems: "Việc cần làm",
    projects: "Dự án",
    settings: "Cài đặt",
    recordingSr: " (đang ghi âm)",
    startMeeting: "Bắt đầu cuộc họp",
    importRecording: "Nhập bản ghi",
  },
  viewSkeletons: {
    loadingMeeting: "Đang tải cuộc họp…",
    loadingActionItems: "Đang tải việc cần làm…",
    loadingSettings: "Đang tải cài đặt…",
  },
  updateBanner: {
    region: "Cập nhật ứng dụng",
    confirmTitle: "Cài đặt phiên bản {{version}}?",
    confirmBody:
      "CatchM sẽ thoát để cập nhật và Windows có thể hỏi quyền. Cuộc họp và cài đặt của bạn không bị ảnh hưởng.",
    downloading: "Đang tải phiên bản {{version}}…",
    cancelling: "Đang hủy tải xuống…",
    applying: "Đang cài đặt phiên bản {{version}} và khởi động lại CatchM…",
    available: "Đã có phiên bản {{version}}",
    released: " (phát hành {{date}})",
    releaseNotes: "Ghi chú phát hành",
    notNow: "Để sau",
    installRestart: "Cài đặt & khởi động lại",
    cancelDownload: "Hủy tải xuống",
    cancellingShort: "Đang hủy…",
    downloadUpdate: "Tải bản cập nhật",
    downloadCancelled: "Đã hủy tải xuống.",
    cancelFailed: "Không hủy được tải xuống. Hãy thử lại.",
  },
  meetingOrganize: {
    project: "Dự án",
    tags: "Thẻ",
    saving: "Đang lưu…",
    movedTo: "Đã chuyển đến “{{name}}”.",
    removedFromProject: "Đã xóa khỏi dự án.",
    tagAdded: "Đã thêm thẻ.",
    tagRemoved: "Đã xóa thẻ.",
    saveError: "Không lưu được thay đổi: {{error}} Hãy thử lại.",
  },
  tagEditor: {
    projectHint: "Để đưa cuộc họp này vào dự án, hãy dùng mục Dự án ở trên.",
    duplicateTag: "Cuộc họp này đã có thẻ “{{tag}}”.",
    removeTagAria: "Xóa thẻ “{{tag}}”",
    removeTag: "Xóa thẻ",
    addTag: "Thêm thẻ",
    addTagPlaceholder: "Thêm thẻ…",
    enterHint: "Nhấn Enter để thêm thẻ.",
  },
}
