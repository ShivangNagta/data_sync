package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPBackendPlanAndUpload(t *testing.T) {
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		switch r.URL.Path {
		case "/v2/sync/plan":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"actions":[{"path":"a.txt","action":"upload","hash":"abc","size":3}]}`))
		case "/v2/files/abc":
			var err error
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
		case "/v2/sync/commit":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accepted":true,"conflict":false,"current_hash":"abc"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	backend := &HTTPBackend{BaseURL: server.URL, Token: "secret"}
	actions, err := backend.GetSyncPlan(context.Background(), []*FileState{{Path: "a.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != ActionUpload {
		t.Fatalf("unexpected actions: %+v", actions)
	}
	_, err = backend.Upload(context.Background(), &UploadFileMeta{
		Path: "a.txt", Hash: "abc", Size: 3,
	}, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if string(uploaded) != "one" {
		t.Fatalf("uploaded %q", uploaded)
	}
}
