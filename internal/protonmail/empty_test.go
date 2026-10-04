package protonmail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProtonMail/go-proton-api"
)

// fakeProton answers the calls EmptyFolder makes; emptyOK decides whether
// the one-request empty works.
func fakeProton(t *testing.T, emptyOK bool) (*Account, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/mail/v4/messages/count":
			json.NewEncoder(w).Encode(map[string]any{"Code": 1000, "Counts": []map[string]any{{"LabelID": TrashID, "Total": 4000, "Unread": 0}}})
		case r.URL.Path == "/mail/v4/messages/empty":
			if !emptyOK {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]any{"Code": 2501, "Error": "not found"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Code": 1000})
		case r.URL.Path == "/mail/v4/messages" && strings.Contains(string(body), `"Page":0`):
			var msgs []map[string]any
			for i := 0; i < 3; i++ {
				msgs = append(msgs, map[string]any{"ID": "m" + string(rune('a'+i)), "Time": time.Now().Unix(), "LabelIDs": []string{TrashID}})
			}
			json.NewEncoder(w).Encode(map[string]any{"Code": 1000, "Messages": msgs})
		case r.URL.Path == "/mail/v4/messages":
			json.NewEncoder(w).Encode(map[string]any{"Code": 1000, "Messages": []any{}})
		case r.URL.Path == "/mail/v4/messages/delete":
			json.NewEncoder(w).Encode(map[string]any{"Code": 1000, "Responses": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	m := proton.New(proton.WithHostURL(srv.URL))
	return &Account{client: m.NewClient("uid", "acc", "ref")}, &calls
}

func TestEmptyFolderOneRequest(t *testing.T) {
	a, calls := fakeProton(t, true)
	var seen [][2]int
	n, err := a.EmptyFolder(context.Background(), TrashID, time.Time{}, func(d, t int) { seen = append(seen, [2]int{d, t}) })
	if err != nil || n != 4000 {
		t.Fatalf("EmptyFolder = %d, %v", n, err)
	}
	if len(seen) != 2 || seen[0] != [2]int{0, 4000} || seen[1] != [2]int{4000, 4000} {
		t.Errorf("progress %v", seen)
	}
	want := "DELETE /mail/v4/messages/empty?LabelID=" + TrashID
	found := false
	for _, c := range *calls {
		if c == want {
			found = true
		}
		if strings.HasSuffix(strings.Split(c, "?")[0], "/delete") {
			t.Errorf("deleted in batches although the one request worked: %v", *calls)
		}
	}
	if !found {
		t.Errorf("no %q in %v", want, *calls)
	}
}

func TestEmptyFolderFallsBackToBatches(t *testing.T) {
	a, calls := fakeProton(t, false)
	var last [2]int
	n, err := a.EmptyFolder(context.Background(), TrashID, time.Time{}, func(d, t int) { last = [2]int{d, t} })
	if err != nil || n != 3 || last != [2]int{3, 3} {
		t.Fatalf("EmptyFolder = %d, %v, progress %v (calls %v)", n, err, last, *calls)
	}
}
