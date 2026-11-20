package gui

import (
	"testing"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestTabNamesAreUniquePerProject(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	layout := protocol.Layout{}
	for range 24 {
		name := suggestTabName(layout, project)
		if !tabNameAvailable(layout, project, uuid.Nil, name) {
			t.Fatalf("duplicate suggestion %q", name)
		}
		addTab(&layout, project, name)
	}
	name := tabName(layout.Tabs[0])
	if tabNameAvailable(layout, project, uuid.Nil, "  "+name+"  ") {
		t.Fatal("duplicate accepted")
	}
	if !tabNameAvailable(layout, other, uuid.Nil, name) {
		t.Fatal("name in another project rejected")
	}
	if !tabNameAvailable(layout, project, layout.Tabs[0].Id, name) {
		t.Fatal("existing tab cannot keep its name")
	}
}

func TestClosedProjectDoesNotReopenOnReload(t *testing.T) {
	project := uuid.New()
	a := &app{layout: protocol.Layout{Revision: 1}, catalog: protocol.Catalog{Projects: []protocol.Project{{Id: project.String()}}}}
	a.scopePrototypeTabs()
	if a.layout.ActiveProject != nil {
		t.Fatal("closed project reopened")
	}
}
