// update_testhook.go: CATCHM_UPDATE_AUTOAPPLY — an env-gated live-test
// hook that runs the exact production update sequence (check →
// DownloadAndVerify → ApplyUpdate's launch/quit path) without needing
// to click the webview UI (no SendKeys-style injection — banned after
// a past incident). Nothing runs unless the env var is set; production
// behavior is unchanged.
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"
)

// maybeAutoApplyUpdate is called from OnStartup in main.go. When
// CATCHM_UPDATE_AUTOAPPLY=1 it waits for the frontend to be up, then
// drives the same bound-method sequence the banner's "Update now"
// button drives, on the same updateManager.
func maybeAutoApplyUpdate(ctx context.Context, m *updateManager) {
	if v, _ := strconv.Atoi(os.Getenv("CATCHM_UPDATE_AUTOAPPLY")); v != 0 {
		go func() {
			// Give the window/webview time to mount (the production
			// flow is user-paced; this only approximates the delay).
			time.Sleep(15 * time.Second)
			bridge := DesktopBridge{updates: m}
			status := bridge.CheckForUpdate()
			log.Printf("AUTOAPPLY: check -> available=%v latest=%s err=%q",
				status.UpdateAvailable, status.LatestVersion, status.Error)
			if !status.UpdateAvailable {
				return
			}
			res, err := bridge.DownloadUpdate()
			if err != nil {
				log.Printf("AUTOAPPLY: download failed: %v", err)
				return
			}
			log.Printf("AUTOAPPLY: verified installer sha256=%s", res.SHA256[:12])
			log.Printf("AUTOAPPLY: applying (installer launches, app quits)")
			if err := bridge.ApplyUpdate(); err != nil {
				log.Printf("AUTOAPPLY: apply failed: %v", err)
			}
		}()
	}
}
