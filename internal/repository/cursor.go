package repository

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EncodeCursor encodes a timestamp and ID into an opaque URL-safe string
func EncodeCursor(t time.Time, id int64) string {
	if t.IsZero() {
		t = time.Now()
	}
	utcStr := t.UTC().Format("2006-01-02 15:04:05")
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", utcStr, id)))
}

// DecodeCursor decodes an opaque cursor string into (timeStr, id, ok)
func DecodeCursor(cursorStr string) (string, int64, bool) {
	if cursorStr == "" {
		return "", 0, false
	}
	raw, err := base64.URLEncoding.DecodeString(cursorStr)
	if err != nil {
		return "", 0, false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return parts[0], id, true
}
