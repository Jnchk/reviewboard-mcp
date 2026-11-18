package tools

import (
	"fmt"
	"strings"
)

// resolveUpload returns inline text or the body of a staged upload, never both.
func (d *Deps) resolveUpload(field, inline, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return inline, nil
	}
	if inline != "" {
		return "", fmt.Errorf("reviewboard: give either %s or %s_upload_id, not both", field, field)
	}
	if d.Staging == nil {
		return "", fmt.Errorf("reviewboard: %s_upload_id is not available in this mode", field)
	}
	data, err := d.Staging.Read(id)
	if err != nil {
		return "", fmt.Errorf("reviewboard: %s_upload_id %s: %w", field, id, err)
	}
	return string(data), nil
}

// consumeUploads drops staged ids after the write they fed succeeded.
func (d *Deps) consumeUploads(ids ...string) {
	if d.Staging == nil {
		return
	}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			_ = d.Staging.Delete(id)
		}
	}
}
