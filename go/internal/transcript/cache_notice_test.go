package transcript

import (
	"pi-workspace.local/workspace/internal/protocol"
	"testing"
)

func TestCacheNoticesSuppressNoiseAndReset(t *testing.T) {
	p := func(v int) *int { return &v }
	entry := func(id string, input, read, write int) protocol.Entry {
		return protocol.Entry{Id: id, CacheInput: p(input), CacheRead: p(read), CacheWrite: p(write)}
	}
	cases := []struct {
		name    string
		entries []protocol.Entry
		want    int
	}{
		{"first request", []protocol.Entry{entry("1", 0, 0, 5000)}, 0},
		{"unreported", []protocol.Entry{entry("1", 5000, 0, 0), entry("2", 5000, 0, 0)}, 0},
		{"noise floor", []protocol.Entry{entry("1", 0, 0, 5000), entry("2", 1024, 3976, 0)}, 0},
		{"miss", []protocol.Entry{entry("1", 0, 0, 5000), entry("2", 2000, 3000, 0)}, 1},
		{"reset", []protocol.Entry{entry("1", 0, 0, 5000), {Id: "c", Kind: "pi.compaction"}, entry("2", 5000, 0, 0)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(cacheNotices(tc.entries)); got != tc.want {
				t.Fatalf("got %d notices, want %d", got, tc.want)
			}
		})
	}
}
