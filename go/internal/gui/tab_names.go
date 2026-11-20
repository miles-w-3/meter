package gui

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

var treeAdjectives = []string{"sappy", "leafy", "shady", "mossy", "ancient", "gnarled", "towering", "blooming", "barky", "evergreen", "rooted", "stately"}
var treeNames = []string{"oak", "cedar", "maple", "willow", "birch", "pine", "ash", "elm", "redwood", "sequoia", "cypress", "spruce", "beech", "fir", "aspen", "sycamore"}

func tabNameAvailable(layout protocol.Layout, project uuid.UUID, except uuid.UUID, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, tab := range layout.Tabs {
		if tab.Id != except && tab.ProjectId != nil && *tab.ProjectId == project && strings.EqualFold(strings.TrimSpace(tabName(tab)), name) {
			return false
		}
	}
	return true
}

func suggestTabName(layout protocol.Layout, project uuid.UUID) string {
	total := len(treeAdjectives) * len(treeNames)
	start := rand.IntN(total)
	for i := 0; i < total; i++ {
		n := (start + i) % total
		name := treeAdjectives[n/len(treeNames)] + "-" + treeNames[n%len(treeNames)]
		if tabNameAvailable(layout, project, uuid.Nil, name) {
			return name
		}
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%s-%d", treeAdjectives[0], treeNames[0], i)
		if tabNameAvailable(layout, project, uuid.Nil, name) {
			return name
		}
	}
}
