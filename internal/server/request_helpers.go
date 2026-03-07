package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	errs "github.com/batuhan/easymatrix/internal/errors"
)

func decodeOptionalJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	return nil
}

func readMessageID(r *http.Request, bodyMessageID string) string {
	if messageID := strings.TrimSpace(r.PathValue("messageID")); messageID != "" {
		return messageID
	}
	if messageID := strings.TrimSpace(bodyMessageID); messageID != "" {
		return messageID
	}
	if messageID := strings.TrimSpace(r.URL.Query().Get("messageID")); messageID != "" {
		return messageID
	}
	return ""
}

func parseCSVQueryValues(values []string) []string {
	parsed := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				parsed = append(parsed, part)
			}
		}
	}
	return parsed
}
