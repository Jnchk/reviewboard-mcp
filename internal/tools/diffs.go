package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"reviewboard-mcp/internal/format"
	"reviewboard-mcp/internal/reviewboard"
)

type getDiffIn struct {
	ID        int  `json:"id" jsonschema:"review request id"`
	Revision  *int `json:"revision,omitempty" jsonschema:"diff revision; omit for the latest published, 0 for the current draft"`
	FilesOnly bool `json:"files_only,omitempty" jsonschema:"only the file table, no patch text"`
	MaxChars  int  `json:"max_chars,omitempty" jsonschema:"patch truncation limit (default 40000)"`
}

func (d *Deps) getDiff(ctx context.Context, in getDiffIn) (string, error) {
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	var (
		diff  *reviewboard.Diff
		draft bool
	)
	if in.Revision != nil && *in.Revision == 0 {
		draft = true
		var err error
		diff, err = d.C.LatestDiff(ctx, in.ID, true)
		if err != nil {
			return "", err
		}
		if diff == nil {
			return "", fmt.Errorf("reviewboard: review request %d has no draft diff", in.ID)
		}
	} else if in.Revision == nil {
		var err error
		diff, err = d.C.LatestDiff(ctx, in.ID, false)
		if err != nil {
			return "", err
		}
		if diff == nil {
			return "", fmt.Errorf("reviewboard: review request %d has no published diff", in.ID)
		}
	} else {
		items, err := d.C.Diffs(ctx, in.ID, false)
		if err != nil {
			return "", err
		}
		for i := range items {
			if items[i].Revision == *in.Revision {
				diff = &items[i]
				break
			}
		}
		if diff == nil {
			return "", fmt.Errorf("reviewboard: review request %d has no diff revision %d", in.ID, *in.Revision)
		}
	}

	files, err := d.C.Files(ctx, diff)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	state := "published"
	if draft {
		state = "draft"
	}
	fmt.Fprintf(&b, "# revision %d (%s) | %s | basedir: %s | %s\n",
		diff.Revision, state, dash(diff.Name), dash(diff.Basedir), format.When(diff.Timestamp))
	b.WriteString("# filediff | status | +/- | file\n")
	for _, f := range files {
		name := shortPath(f.DestFile)
		if f.SourceFile != "" && f.SourceFile != f.DestFile {
			name = shortPath(f.SourceFile) + " -> " + shortPath(f.DestFile)
		}
		if f.Binary {
			name += " (binary)"
		}
		b.WriteString(format.Row(
			strconv.Itoa(f.ID),
			f.Status,
			fmt.Sprintf("+%d/-%d", f.ExtraData.InsertCount, f.ExtraData.DeleteCount),
			name,
		) + "\n")
	}
	fmt.Fprintf(&b, "# %d files\n", len(files))

	if !in.FilesOnly {
		raw, err := d.C.RawDiff(ctx, diff)
		if err != nil {
			return "", err
		}
		limit := in.MaxChars
		if limit <= 0 {
			limit = 40000
		}
		fmt.Fprintf(&b, "\n[patch]\n%s\n", format.Clip(string(raw), limit))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type uploadDiffIn struct {
	ID             int    `json:"id" jsonschema:"review request id"`
	Path           string `json:"path" jsonschema:"unified diff file path on the machine running this server"`
	Basedir        string `json:"basedir,omitempty" jsonschema:"base directory prepended to diff paths; depends on the repository"`
	ParentDiffPath string `json:"parent_diff_path,omitempty" jsonschema:"optional parent diff file path"`
	BaseCommitID   string `json:"base_commit_id,omitempty" jsonschema:"commit the diff is built upon"`
	Publish        *bool  `json:"publish,omitempty" jsonschema:"publish the draft (default true)"`
}

func (d *Deps) uploadDiff(ctx context.Context, in uploadDiffIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("reviewboard: path is required")
	}
	if err := d.C.UploadDiff(ctx, in.ID, in.Path, in.Basedir, in.ParentDiffPath, in.BaseCommitID); err != nil {
		return "", err
	}
	publish := boolOr(in.Publish, true)
	if err := d.C.UpdateDraft(ctx, in.ID, nil, publish); err != nil {
		return "", fmt.Errorf("reviewboard: diff uploaded but publishing the draft failed: %w", err)
	}
	state := "uploaded and published"
	if !publish {
		state = "uploaded as a draft diff (not published)"
	}
	return fmt.Sprintf("#%d diff %s (basedir: %s). %s", in.ID, state, dash(in.Basedir), d.C.ReviewRequestURL(in.ID)), nil
}
