package transcript

import (
	"fmt"
	"pi-workspace.local/workspace/internal/protocol"
	"strings"
)

// cacheNotices estimates re-billed prompt tokens, following Pi's noise floor.
// Entries outside the loaded history are unknown, so the first request is not compared.
func cacheNotices(entries []protocol.Entry) map[string]string {
	notices := make(map[string]string)
	previous, reported := 0, false
	model := ""
	for _, entry := range entries {
		if strings.Contains(entry.Kind, "compaction") || strings.Contains(entry.Kind, "branch_summary") {
			previous, reported, model = 0, false, ""
		}
		if entry.CacheInput == nil || entry.CacheRead == nil || entry.CacheWrite == nil {
			continue
		}
		prompt := *entry.CacheInput + *entry.CacheRead + *entry.CacheWrite
		if prompt <= 0 {
			continue
		}
		activity := *entry.CacheRead+*entry.CacheWrite > 0
		missed := min(previous, prompt) - *entry.CacheRead
		if previous > 0 && (reported || activity) && missed > 1024 {
			notice := fmt.Sprintf("Possible cache miss: ~%s tokens reprocessed", formatCacheTokens(missed))
			if model != "" && entry.CacheModel != nil && model != *entry.CacheModel {
				notice += " · model changed"
			}
			notices[entry.Id] = notice
		}
		previous, reported = prompt, reported || activity
		if entry.CacheModel != nil {
			model = *entry.CacheModel
		}
	}
	return notices
}

func formatCacheTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
