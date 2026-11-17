package reviewboard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
)

// UploadDiff posts a unified diff to the review request draft. Review Board
// creates draft revision 0; the caller publishes the draft afterwards.
func (c *Client) UploadDiff(ctx context.Context, id int, path, basedir, parentPath, baseCommitID string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reviewboard: diff file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("reviewboard: diff file %q is not a regular file", path)
	}
	if info.Size() > c.maxUpload {
		return fmt.Errorf("reviewboard: diff file is %d bytes, above REVIEWBOARD_MAX_UPLOAD (%d)", info.Size(), c.maxUpload)
	}
	if parentPath != "" {
		pinfo, err := os.Stat(parentPath)
		if err != nil {
			return fmt.Errorf("reviewboard: parent diff file: %w", err)
		}
		if !pinfo.Mode().IsRegular() {
			return fmt.Errorf("reviewboard: parent diff file %q is not a regular file", parentPath)
		}
		if pinfo.Size() > c.maxUpload {
			return fmt.Errorf("reviewboard: parent diff file is %d bytes, above REVIEWBOARD_MAX_UPLOAD (%d)", pinfo.Size(), c.maxUpload)
		}
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := addFile(mw, "path", path); err != nil {
		return err
	}
	if parentPath != "" {
		if err := addFile(mw, "parent_diff_path", parentPath); err != nil {
			return err
		}
	}
	if basedir != "" {
		if err := mw.WriteField("basedir", basedir); err != nil {
			return err
		}
	}
	if baseCommitID != "" {
		if err := mw.WriteField("base_commit_id", baseCommitID); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("reviewboard: building upload: %w", err)
	}
	_, err = c.do(ctx, "POST", c.url(fmt.Sprintf("api/review-requests/%d/draft/diffs/", id), nil),
		body.Bytes(), mw.FormDataContentType(), nil)
	return err
}

func addFile(mw *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reviewboard: diff file: %w", err)
	}
	defer f.Close()
	part, err := mw.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return fmt.Errorf("reviewboard: building upload: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("reviewboard: reading diff: %w", err)
	}
	return nil
}

// Diffs lists published revisions, or the single draft when draft is true.
func (c *Client) Diffs(ctx context.Context, id int, draft bool) ([]Diff, error) {
	path, key := fmt.Sprintf("api/review-requests/%d/diffs/", id), "diffs"
	if draft {
		path, key = fmt.Sprintf("api/review-requests/%d/draft/diffs/", id), "draft_diffs"
	}
	items, _, err := getPage[Diff](ctx, c, path, key, nil)
	return items, err
}

// LatestDiff returns the highest published revision, or the draft when draft is true.
func (c *Client) LatestDiff(ctx context.Context, id int, draft bool) (*Diff, error) {
	items, err := c.Diffs(ctx, id, draft)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Revision < items[j].Revision })
	return &items[len(items)-1], nil
}

// Files lists the file diffs inside one diff revision.
func (c *Client) Files(ctx context.Context, d *Diff) ([]FileDiff, error) {
	href := d.Links["files"].Href
	if href == "" {
		return nil, fmt.Errorf("reviewboard: diff %d has no files link", d.Revision)
	}
	return getAllAt[FileDiff](ctx, c, href, "files")
}

// RawDiff returns the unified diff text.
func (c *Client) RawDiff(ctx context.Context, d *Diff) ([]byte, error) {
	href := d.Links["self"].Href
	if href == "" {
		return nil, fmt.Errorf("reviewboard: diff %d has no self link", d.Revision)
	}
	return c.doRaw(ctx, href, "text/x-patch")
}
