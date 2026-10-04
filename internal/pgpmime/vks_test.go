package pgpmime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublish(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/vks/v1/upload":
			if body["keytext"] != "KEY" {
				http.Error(w, `{"error":"bad key"}`, 400)
				return
			}
			_, _ = w.Write([]byte(`{"key_fpr":"AB","token":"tok","status":{"jan@example.cz":"unpublished","old@example.cz":"published"}}`))
		case "/vks/v1/request-verify":
			if body["token"] != "tok" {
				t.Errorf("token %v", body["token"])
			}
			for _, a := range body["addresses"].([]any) {
				asked = append(asked, a.(string))
			}
			_, _ = w.Write([]byte(`{"key_fpr":"AB","token":"tok","status":{"jan@example.cz":"pending"}}`))
		}
	}))
	defer srv.Close()
	vksURL = srv.URL + "/vks/v1"
	st, err := Publish(context.Background(), "KEY", []string{"jan@example.cz", "old@example.cz"}, "cs")
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "jan@example.cz" || st["jan@example.cz"] != "pending" || st["old@example.cz"] != "published" {
		t.Fatalf("asked=%v status=%v", asked, st)
	}
	if _, err := Publish(context.Background(), "BAD", nil, "cs"); err == nil || err.Error() != "keys.openpgp.org: bad key" {
		t.Errorf("error = %v", err)
	}
}
