# Plan: M13d — Cloud sync web UI

- Status: draft (chưa bắt đầu code — chờ duyệt)
- Created: 2026-09-21
- Repo: `catchm` (repo này). Dùng endpoints `/api/cloud/*` mà M13c
  đã ship; không đổi backend trừ khi spec này yêu cầu.

## Goal

Màn hình cloud-sync trong web UI: đăng ký / đăng nhập / đăng xuất,
hiển thị recovery code đúng một lần, nút Sync chạy orchestration và
  báo kết quả (pushed/pulled/conflicted), thanh trạng thái đăng nhập.
Không có gì trong UI biết đến crypto — mọi thứ crypto đã nằm trong
`internal/server/cloud.go` (M13c); UI chỉ gọi HTTP.

## Scope

### 1. `web/src/lib/api.ts` — typed client additions (không network logic mới)

Mirror 1:1 các route M13c, đúng quy ước file này đang theo (mỗi
export corresponds to a route):

- `cloudSignup(email, password): Promise<{ email: string;
  recovery_code: string }>`
- `cloudLogin(email, password): Promise<{ email: string }>`
- `cloudLogout(): Promise<{ status: string }>`
- `cloudStatus(): Promise<CloudStatus>` — `CloudStatus` là union:
  `{ logged_in: false }` hoặc `{ logged_in: true; email: string;
  logged_in_at: string; last_sync_at: string }` (đúng JSON handler
  trả về — `last_sync_at` là zero-time RFC3339 khi chưa sync).
- `cloudSync(): Promise<SyncResult>` — `SyncResult { pushed: string[]
  | null; pulled: string[] | null; conflicted: string[] | null }`
  (null khi rỗng — Go marshals nil slice thành null, không phải []).
- Error handling theo pattern sẵn có trong api.ts (non-2xx → throw
  với message từ `{"error": ...}` body).

### 2. `web/src/components/SettingsCloud.tsx` — tab Settings mới

Tab "Cloud" trong Settings (đặt sau Translation), tái dùng
`SettingsTranslate`'s layout pattern (card + form + status line):

- **Chưa đăng nhập**: form email + password, hai nút "Sign up" /
  "Log in". Signup thành công → dialog hiển thị recovery code +
  cảnh báo "lưu lại ngay, không bao giờ hiển thị lại" + nút copy.
  Đăng nhập nhầm (401) hiển thị lỗi inline, không reset form.
- **Đã đăng nhập**: hiện email, thời điểm đăng nhập, last sync; nút
  "Sync now" (disable khi đang chạy, spinner), nút "Log out".
- Sync result: "Pushed 2 · Pulled 0 · Conflicted 1" + nếu conflicted
  > 0, liệt kê meeting IDs kèm gợi ý mở thư mục meeting để xem file
  `conflict-*` (đúng mô hình conflict-copy của M13c — UI chỉ báo
  hiệu, không tự gộp).
- Poll `cloudStatus()` mỗi 30s khi tab mở (để bắt sync chạy từ nơi
  khác/CLI) — không poll khi component unmount.

### 3. Điểm ánh xạ nhỏ trong server (chỉ nếu cần)

Không dự kiến thay đổi backend. `last_sync_at` zero-time
(`0001-01-01T00:00:00Z`) hiển thị là "never". Nếu cần field mới
(ví dụ đếm meetings local) thì ghi vào Decisions kèm lý do.

## Out of scope

Device linking UI (M14), recovery flow UI (/recover, M15), thay đổi
crypto/protocol, deploy URL config UI (api_base_url vẫn là
config-file only như M13c Decisions), i18n (UI là tiếng Anh như các
tab khác).

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l .` sạch; `go test
   ./...` xanh (không test Go mới trừ khi thêm server-side logic).
2. `cd web && bun run build` (hoặc npm) build TypeScript sạch, không
   type error.
3. Thủ công với wrangler dev + catchm serve (giống M13c Validation):
   signup qua UI → thấy recovery code một lần → tạo meeting → Sync
   now → thấy "Pushed 1" → refresh → status đúng → logout → status
   về not-logged-in. Paste output/screenshot mô tả vào Validation.
4. Không secret nào (password, recovery code sau lần đầu, session
   token) được ghi vào localStorage hay bất kỳ storage nào của
   browser — recovery code chỉ render trong DOM lúc hiển thị.

## Decisions

(fill in khi duyệt: có tách CloudStatus union hay dùng optional
fields; đặt tab Cloud trước hay sau Translation trong TabsList;
recovery-code dialog là component riêng hay inline; refresh trạng
thái sau sync bằng cách nào.)

## Progress

- [ ] api.ts typed client cho /api/cloud/*
- [ ] SettingsCloud component + tab Settings
- [ ] Manual verification với wrangler dev (criterion 3)
- [ ] Decisions/Validation điền đầy đủ

## Validation

(to be filled)
