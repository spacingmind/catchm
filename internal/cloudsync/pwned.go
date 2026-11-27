package cloudsync

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// PwnedRangeURL is Have I Been Pwned's k-anonymity range API.
const PwnedRangeURL = "https://api.pwnedpasswords.com/range/"

// PasswordBreachCount reports how many times password appears in known
// breaches, via HIBP's range API: only the first 5 hex characters of
// the password's SHA-1 leave this machine, and padding hides the
// response size (docs/plans/active/cloud-auth-hardening.md). It runs
// locally, before signup or a password reset — the server never sees the
// password.
func PasswordBreachCount(ctx context.Context, client *http.Client, baseURL, password string) (int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = PwnedRangeURL
	}
	sum := sha1.Sum([]byte(password))
	full := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := full[:5], full[5:]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+prefix, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "CatchM")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("pwned passwords: status %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		hashSuffix, count, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(hashSuffix, suffix) {
			continue
		}
		n, _ := strconv.Atoi(count)
		return n, nil // padding rows have count 0
	}
	return 0, sc.Err()
}
