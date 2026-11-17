package reviewboard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, base string) *Client {
	t.Helper()
	c, err := New(base, "secret", time.Second, false, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListRequestShape(t *testing.T) {
	var gotPath, gotToken, gotStatus, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("Authorization")
		gotStatus = r.URL.Query().Get("status")
		gotLimit = r.URL.Query().Get("max-results")
		io.WriteString(w, `{"review_requests":[{"id":7,"summary":"hi","status":"pending"}],"total_results":1,"links":{}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/rb")
	items, total, err := c.ReviewRequests(context.Background(), RequestQuery{Status: "pending", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rb/api/review-requests/" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "token secret" {
		t.Fatalf("Authorization = %q", gotToken)
	}
	if gotStatus != "pending" || gotLimit != "50" {
		t.Fatalf("query status=%q limit=%q", gotStatus, gotLimit)
	}
	if len(items) != 1 || items[0].ID != 7 || total != 1 {
		t.Fatalf("items = %#v total = %d", items, total)
	}
}

func TestErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"stat":"fail","err":{"code":105,"msg":"Summary is required"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.ReviewRequest(context.Background(), 7)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"400", "validation failed", "Summary is required"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	if !IsStatus(err, 400) || !IsCode(err, 105) {
		t.Fatalf("status/code helpers failed for %v", err)
	}
}

func TestUpdateDraftPublishOrder(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.UpdateDraft(context.Background(), 3, map[string]any{"summary": "new"}, true); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("PUT calls = %d, want 2: %#v", len(bodies), bodies)
	}
	if !strings.Contains(bodies[0], `"summary":"new"`) || !strings.Contains(bodies[1], `"public":true`) {
		t.Fatalf("bodies = %#v", bodies)
	}
}

func TestUploadDiffMultipart(t *testing.T) {
	var gotBasedir, gotName, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		gotBasedir = r.FormValue("basedir")
		f, h, err := r.FormFile("path")
		if err != nil {
			t.Errorf("file: %v", err)
			return
		}
		defer f.Close()
		gotName = h.Filename
		b, _ := io.ReadAll(f)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "change.diff")
	if err := os.WriteFile(path, []byte("--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, srv.URL)
	if err := c.UploadDiff(context.Background(), 4, path, "trunk", "", ""); err != nil {
		t.Fatal(err)
	}
	if gotBasedir != "trunk" || gotName != "change.diff" || !strings.Contains(gotBody, "@@ -1 +1 @@") {
		t.Fatalf("basedir=%q name=%q body=%q", gotBasedir, gotName, gotBody)
	}
}

func TestCreateReviewWithCommentsPublishesLast(t *testing.T) {
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews/"):
			w.Header().Set("Location", "http://example/api/review-requests/4/reviews/9/")
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	rid, err := c.CreateReview(context.Background(), 4, ReviewInput{
		ShipIt:       true,
		DiffComments: []DiffCommentInput{{FilediffID: 31, FirstLine: 5, NumLines: 1, Text: "why"}},
	}, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if rid != 9 {
		t.Fatalf("review id = %d", rid)
	}
	want := []string{
		"POST /api/review-requests/4/reviews/",
		"POST /api/review-requests/4/reviews/9/diff-comments/",
		"PUT /api/review-requests/4/reviews/9/",
	}
	if strings.Join(order, "\n") != strings.Join(want, "\n") {
		t.Fatalf("order:\n%s", strings.Join(order, "\n"))
	}
}

func TestServerErrorRetriesGETOnly(t *testing.T) {
	var gets, posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
		} else {
			posts++
		}
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"stat":"fail","err":{"code":500,"msg":"down"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.ReviewRequest(context.Background(), 1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if gets != 2 {
		t.Fatalf("GET attempts = %d, want 2", gets)
	}
	_ = c.UpdateDraft(context.Background(), 1, map[string]any{"summary": "x"}, false)
	if posts != 1 {
		t.Fatalf("PUT attempts = %d, want 1", posts)
	}
}
