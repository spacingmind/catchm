package cloudsync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordBreachCount(t *testing.T) {
	// SHA-1("password") = 5BAA61E4C9B93F3F0682250B6CF8331B7EE68FD8
	var gotPath, gotPadding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotPadding = r.URL.Path, r.Header.Get("Add-Padding")
		fmt.Fprint(w, "0018A45C4D1DEF81644B54AB7F969B88D65:1\r\n1E4C9B93F3F0682250B6CF8331B7EE68FD8:3861493\r\nFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF:0\r\n")
	}))
	defer srv.Close()
	n, err := PasswordBreachCount(context.Background(), nil, srv.URL+"/range/", "password")
	if err != nil || n != 3861493 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if gotPath != "/range/5BAA6" || gotPadding != "true" {
		t.Fatalf("request %s padding=%q (only the 5-char prefix may be sent)", gotPath, gotPadding)
	}
	n, err = PasswordBreachCount(context.Background(), nil, srv.URL+"/range/", "a much better passphrase 7!")
	if err != nil || n != 0 {
		t.Fatalf("unbreached = %d, %v", n, err)
	}
	if strings.Contains(gotPath, "passphrase") {
		t.Fatal("password leaked into the URL")
	}
}
