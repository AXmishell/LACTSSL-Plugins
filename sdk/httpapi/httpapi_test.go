package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoJSONAndError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") == "1" {
			http.Error(w, "boom", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(server.URL)
	resp, err := client.Do(context.Background(), http.MethodGet, "/", nil, nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := JSON(resp.Body, &payload); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok")
	}

	_, err = client.Do(context.Background(), http.MethodGet, "/?fail=1", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if IsNotFound(err) {
		t.Fatal("did not expect not found")
	}
}
