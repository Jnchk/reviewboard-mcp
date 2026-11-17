package tools

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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"reviewboard-mcp/internal/reviewboard"
)

type fakeState struct {
	draftPuts []string
	reviewSeq []string
	lastPatch string
}

func fakeRB(t *testing.T) (*httptest.Server, *fakeState) {
	t.Helper()
	st := &fakeState{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}

	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"session":{"authenticated":true,"links":{"user":{"href":"x","method":"GET","title":"alice"}}},"stat":"ok"}`)
	})
	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"users":[{"id":9,"username":"alice","first_name":"Alice","last_name":"Zhang"}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/users/9/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"user":{"id":9,"username":"alice"},"stat":"ok"}`)
	})
	mux.HandleFunc("/api/repositories/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"repositories":[{"id":3,"name":"web-svn","path":"/trunk/web","tool":"Subversion"}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/groups/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"groups":[{"id":5,"name":"web-team","display_name":"Web Team"}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/groups/5/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"group":{"id":5,"name":"web-team","display_name":"Web Team"},"stat":"ok"}`)
	})

	mux.HandleFunc("/api/review-requests/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeJSON(w, `{"review_request":{"id":42,"absolute_url":"http://x/r/42/","summary":"new"},"stat":"ok"}`)
			return
		}
		base := "http://" + r.Host
		writeJSON(w, `{"review_requests":[{"id":42,"summary":"add export","status":"pending","submitter":{"title":"alice"},"repository":{"title":"web-svn"},"last_updated":"2026-11-16T09:40:12Z","ship_it_count":1,"issue_open_count":2,"issue_resolved_count":0,"issue_dropped_count":0,"absolute_url":"`+base+`/r/42/"}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, `{"review_request":{"id":42,"absolute_url":"http://x/r/42/","summary":"add export","status":"pending","description":"body text","testing_done":"go test ./...","repository":{"title":"web-svn"},"submitter":{"title":"alice"},"target_people":[{"title":"alice"}],"target_groups":[{"title":"web-team"}],"issue_open_count":2,"ship_it_count":1,"last_updated":"2026-11-16T09:40:12Z"},"stat":"ok"}`)
	})
	mux.HandleFunc("/api/review-requests/42/draft/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		st.draftPuts = append(st.draftPuts, string(b))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/review-requests/42/diffs/", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		writeJSON(w, `{"diffs":[{"id":5,"revision":1,"name":"change.diff","basedir":"trunk","timestamp":"2026-11-16T09:00:00Z","links":{"self":{"href":"`+base+`/api/review-requests/42/diffs/1/"},"files":{"href":"`+base+`/api/review-requests/42/diffs/1/files/"}}}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/diffs/1/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/x-patch") {
			w.Header().Set("Content-Type", "text/x-patch")
			io.WriteString(w, "--- a/src/app.go\n+++ b/src/app.go\n@@ -1 +1 @@\n-a\n+b\n")
			return
		}
		writeJSON(w, `{"diff":{"id":5,"revision":1,"links":{}},"stat":"ok"}`)
	})
	mux.HandleFunc("/api/review-requests/42/diffs/1/files/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"files":[{"id":41,"source_file":"src/app.go","dest_file":"src/app.go","status":"modified","extra_data":{"insert_count":12,"delete_count":3}}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/draft/diffs/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.FormValue("basedir"); got != "trunk" {
			t.Errorf("basedir = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("/api/review-requests/42/reviews/", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		if r.Method == http.MethodPost {
			st.reviewSeq = append(st.reviewSeq, "create")
			w.Header().Set("Location", base+"/api/review-requests/42/reviews/12/")
			w.WriteHeader(http.StatusCreated)
			return
		}
		writeJSON(w, `{"reviews":[{"id":12,"body_top":"looks good","ship_it":true,"timestamp":"2026-11-16T09:40:12Z","links":{"user":{"title":"alice"},"diff_comments":{"href":"`+base+`/api/review-requests/42/reviews/12/diff-comments/"},"general_comments":{"href":"`+base+`/api/review-requests/42/reviews/12/general-comments/"}}}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/", func(w http.ResponseWriter, r *http.Request) {
		st.reviewSeq = append(st.reviewSeq, "publish")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/diff-comments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			st.reviewSeq = append(st.reviewSeq, "diff-comment")
			w.WriteHeader(http.StatusCreated)
			return
		}
		writeJSON(w, `{"diff_comments":[{"id":32,"text":"why not reuse parse()","issue_opened":true,"issue_status":"open","first_line":120,"num_lines":5,"links":{"filediff":{"href":"http://x/api/review-requests/42/diffs/1/files/41/","title":"src/app.go"}}}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/general-comments/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"general_comments":[{"id":31,"text":"needs a test","issue_opened":false,"links":{}}],"total_results":1,"links":{}}`)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/replies/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://x/api/review-requests/42/reviews/12/replies/13/")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/replies/13/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/review-requests/42/reviews/12/replies/13/general-comments/", func(w http.ResponseWriter, r *http.Request) {
		st.reviewSeq = append(st.reviewSeq, "reply-comment")
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Errorf("%s %s Authorization = %q", r.Method, r.URL.Path, got)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, st
}

func connect(t *testing.T, d *Deps) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	Register(s, d)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "c"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s: no content", name)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content type %T", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

func TestReadTools(t *testing.T) {
	srv, _ := fakeRB(t)
	c, _ := reviewboard.New(srv.URL, "secret", time.Second, false, 1<<20)
	d := &Deps{C: c, M: reviewboard.NewMeta(c, time.Minute), DefaultLimit: 25, TextType: "markdown"}
	cs := connect(t, d)

	out, isErr := call(t, cs, "whoami", map[string]any{})
	if isErr || !strings.Contains(out, "alice") {
		t.Fatalf("whoami: %q (error=%t)", out, isErr)
	}

	out, isErr = call(t, cs, "list_review_requests", map[string]any{"repository": "web-svn", "to_user": "alice"})
	if isErr || !strings.Contains(out, "add export") || !strings.Contains(out, "returned 1 of 1") {
		t.Fatalf("list_review_requests: %q (error=%t)", out, isErr)
	}

	out, isErr = call(t, cs, "get_review_request", map[string]any{"id": 42})
	for _, want := range []string{"#42 add export", "web-svn", "revision 1", "r/42/", "go test"} {
		if isErr || !strings.Contains(out, want) {
			t.Fatalf("get_review_request missing %q: %q", want, out)
		}
	}

	out, isErr = call(t, cs, "get_diff", map[string]any{"id": 42})
	for _, want := range []string{"filediff", "41", "src/app.go", "@@ -1 +1 @@"} {
		if isErr || !strings.Contains(out, want) {
			t.Fatalf("get_diff missing %q: %q", want, out)
		}
	}

	out, isErr = call(t, cs, "list_reviews", map[string]any{"id": 42})
	for _, want := range []string{"review 12", "general comment 31", "diff comment 32", "src/app.go:120-124"} {
		if isErr || !strings.Contains(out, want) {
			t.Fatalf("list_reviews missing %q: %q", want, out)
		}
	}

	out, isErr = call(t, cs, "list_metadata", map[string]any{"kind": "groups"})
	if isErr || !strings.Contains(out, "web-team") {
		t.Fatalf("list_metadata: %q (error=%t)", out, isErr)
	}
}

func TestWriteTools(t *testing.T) {
	srv, st := fakeRB(t)
	c, _ := reviewboard.New(srv.URL, "secret", time.Second, false, 1<<20)
	d := &Deps{C: c, M: reviewboard.NewMeta(c, time.Minute), DefaultLimit: 25, TextType: "markdown"}
	cs := connect(t, d)

	out, isErr := call(t, cs, "create_review_request", map[string]any{
		"repository": "web-svn", "summary": "new", "description": "body",
		"target_people": "alice", "target_groups": "web-team",
	})
	if isErr || !strings.Contains(out, "#42") || !strings.Contains(out, "published") {
		t.Fatalf("create_review_request: %q (error=%t)", out, isErr)
	}
	if len(st.draftPuts) != 2 || !strings.Contains(st.draftPuts[1], `"public":true`) {
		t.Fatalf("draft puts = %#v", st.draftPuts)
	}

	out, isErr = call(t, cs, "update_review_request", map[string]any{
		"id": 42, "summary": "newer", "description": "new body", "confirm": true,
	})
	if isErr || !strings.Contains(out, "updated and published") {
		t.Fatalf("update_review_request: %q (error=%t)", out, isErr)
	}
	if !strings.Contains(st.draftPuts[len(st.draftPuts)-1], `"public":true`) {
		t.Fatalf("publish put missing: %#v", st.draftPuts)
	}

	path := filepath.Join(t.TempDir(), "x.diff")
	os.WriteFile(path, []byte("--- a\n+++ b\n"), 0o600)
	out, isErr = call(t, cs, "upload_diff", map[string]any{"id": 42, "path": path, "basedir": "trunk"})
	if isErr || !strings.Contains(out, "uploaded and published") {
		t.Fatalf("upload_diff: %q (error=%t)", out, isErr)
	}

	out, isErr = call(t, cs, "create_review", map[string]any{
		"id": 42, "ship_it": true,
		"diff_comments": []map[string]any{{"filediff_id": 41, "first_line": 120, "num_lines": 5, "text": "why"}},
	})
	if isErr || !strings.Contains(out, "review 12 posted") {
		t.Fatalf("create_review: %q (error=%t)", out, isErr)
	}
	if strings.Join(st.reviewSeq, ",") != "create,diff-comment,publish" {
		t.Fatalf("review order = %#v", st.reviewSeq)
	}

	out, isErr = call(t, cs, "reply_review", map[string]any{
		"id": 42, "review_id": 12,
		"comments": []map[string]any{{"reply_to_id": 31, "kind": "general", "text": "fixed"}},
	})
	if isErr || !strings.Contains(out, "reply 13") {
		t.Fatalf("reply_review: %q (error=%t)", out, isErr)
	}

	out, isErr = call(t, cs, "close_review_request", map[string]any{
		"id": 42, "status": "submitted", "description": "done", "confirm": true,
	})
	if isErr || !strings.Contains(out, "status -> submitted") {
		t.Fatalf("close_review_request: %q (error=%t)", out, isErr)
	}
}

func TestReadOnlyBlocksWrites(t *testing.T) {
	srv, _ := fakeRB(t)
	c, _ := reviewboard.New(srv.URL, "secret", time.Second, false, 1<<20)
	d := &Deps{C: c, M: reviewboard.NewMeta(c, time.Minute), DefaultLimit: 25, TextType: "markdown", ReadOnly: true}
	cs := connect(t, d)

	out, isErr := call(t, cs, "create_review_request", map[string]any{"repository": "web-svn", "summary": "x"})
	if !isErr || !strings.Contains(out, "read-only") {
		t.Fatalf("read-only: %q (error=%t)", out, isErr)
	}
}

func TestCloseNeedsConfirm(t *testing.T) {
	srv, _ := fakeRB(t)
	c, _ := reviewboard.New(srv.URL, "secret", time.Second, false, 1<<20)
	d := &Deps{C: c, M: reviewboard.NewMeta(c, time.Minute), DefaultLimit: 25, TextType: "markdown"}
	cs := connect(t, d)
	out, isErr := call(t, cs, "close_review_request", map[string]any{"id": 42, "status": "discarded"})
	if !isErr || !strings.Contains(out, "confirm: true") {
		t.Fatalf("close confirm: %q (error=%t)", out, isErr)
	}
}

func TestRawMode(t *testing.T) {
	srv, _ := fakeRB(t)
	c, _ := reviewboard.New(srv.URL, "secret", time.Second, false, 1<<20)
	d := &Deps{C: c, M: reviewboard.NewMeta(c, time.Minute), DefaultLimit: 25, TextType: "markdown"}
	cs := connect(t, d)
	out, isErr := call(t, cs, "get_review_request", map[string]any{"id": 42, "raw": true})
	if isErr || !strings.Contains(out, `"summary"`) {
		t.Fatalf("raw: %q (error=%t)", out, isErr)
	}
}
