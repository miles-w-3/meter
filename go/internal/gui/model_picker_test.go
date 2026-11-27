package gui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestModelPickerStagesAndAppliesToEmptyPane(t *testing.T) {
	a := &app{modelsLoaded: true, modelsInventory: protocol.ModelInventory{
		Providers: []protocol.ProviderInfo{{Id: "test", Name: "Test", Status: "configured"}},
		Models: []protocol.ModelInfo{
			{Provider: "test", ModelId: "first", Name: "First", ThinkingLevels: []protocol.ThinkingLevel{"off", "high"}},
			{Provider: "test", ModelId: "second", Name: "Second", ThinkingLevels: []protocol.ThinkingLevel{"off", "high"}},
		},
	}}
	p := &pane{}
	driver := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(200).Children(func() { a.modelChip(c, protocol.LayoutNode{}, p, false) })
	}, 900, 700)
	for _, label := range []string{"Choose model and thinking", "Second · test", "Thinking high"} {
		if err := driver.Click(label); err != nil {
			t.Fatalf("click %q: %v; visible: %v", label, err, driver.Texts())
		}
	}
	if p.modelDraft != nil {
		t.Fatal("selection should not apply before Apply")
	}
	if err := driver.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	if p.modelDraft == nil || p.modelDraft.Model.ModelId != "second" || p.modelDraft.ThinkingLevel != "high" {
		t.Fatalf("wrong applied selection: %+v", p.modelDraft)
	}
	if p.modelPicker.open {
		t.Fatal("picker stayed open")
	}
	if err := driver.Click("Choose model and thinking"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click("First · test"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if p.modelDraft.Model.ModelId != "second" {
		t.Fatal("cancel changed the applied selection")
	}
}
