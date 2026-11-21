package mermaid

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	pele "github.com/Broderick-Westrope/pele-go"
	_ "github.com/Broderick-Westrope/pele-go/all"
)

// These are feasibility tests, not a security boundary or a visual fidelity test.
func TestCorpus(t *testing.T) {
	for _, example := range Corpus {
		name, source := example.Name, example.Source
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := pele.Render(ctx, source, pele.Options{Links: pele.Ptr(false), Images: pele.Ptr(false)})
			if err != nil {
				t.Fatal(err)
			}
			if result.Width <= 0 || result.Height <= 0 || math.IsNaN(result.Width) || math.IsNaN(result.Height) || math.IsInf(result.Width, 0) || math.IsInf(result.Height, 0) {
				t.Fatalf("invalid dimensions: %v x %v", result.Width, result.Height)
			}
			dec := xml.NewDecoder(strings.NewReader(result.SVG))
			first := true
			for {
				tok, err := dec.Token()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("invalid XML: %v", err)
				}
				if start, ok := tok.(xml.StartElement); ok && first {
					if start.Name.Local != "svg" {
						t.Fatalf("root is %q", start.Name.Local)
					}
					first = false
				}
			}
			if first {
				t.Fatal("empty SVG")
			}
		})
	}
}

func TestFailuresAndCancellation(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		if _, err := pele.Render(context.Background(), "not a diagram", pele.Options{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("source limit", func(t *testing.T) {
		if _, err := pele.Render(context.Background(), "flowchart LR\n"+strings.Repeat("A", 128), pele.Options{Limit: pele.Ptr(32.0)}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("output limit", func(t *testing.T) {
		if _, err := pele.Render(context.Background(), "flowchart LR\n A --> B", pele.Options{OutputLimit: pele.Ptr(32.0)}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := pele.Render(ctx, "flowchart LR\n A --> B", pele.Options{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
}

func TestDisabledLink(t *testing.T) {
	source := "flowchart LR\n A[Example] --> B[Done]\n click A href \"https://example.invalid/secret\""
	enabled, err := pele.Render(context.Background(), source, pele.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enabled.SVG, "https://example.invalid/secret") {
		t.Fatal("fixture did not produce a link with default options")
	}
	disabled, err := pele.Render(context.Background(), source, pele.Options{Links: pele.Ptr(false), Images: pele.Ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(disabled.SVG, "https://example.invalid/secret") || len(disabled.Links) != 0 {
		t.Fatal("disabled link remained in output")
	}
}

func BenchmarkFlowchart(b *testing.B) {
	source := "flowchart LR\n A[Start] --> B[Done]"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := pele.Render(context.Background(), source, pele.Options{Links: pele.Ptr(false), Images: pele.Ptr(false)}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGalleryCoversRegisteredDiagramTypes(t *testing.T) {
	want := strings.Fields("agentflow architecture block c4 class cynefin er eventmodeling flowchart gantt gitGraph info ishikawa journey kanban mindmap packet pie quadrantChart radar railroad requirement sankey sequence state swimlane timeline treemap treeView usecase venn wardley xychart")
	seen := make(map[string]bool)
	for _, example := range Corpus {
		result, err := pele.Render(context.Background(), example.Source, pele.Options{Links: pele.Ptr(false), Images: pele.Ptr(false)})
		if err != nil {
			t.Fatalf("%s: %v", example.Name, err)
		}
		seen[string(result.Type)] = true
	}
	for _, typ := range want {
		if !seen[typ] {
			t.Errorf("missing diagram type %s", typ)
		}
	}
}

// The tool's examples must stay tied to the fixtures exercised above.
func TestCatalogFixtures(t *testing.T) {
	data, err := os.ReadFile("../../../server/tools/mermaid-catalog-data.ts")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "["), strings.LastIndex(text, "]")
	if start < 0 || end < start {
		t.Fatal("catalog data is not a JSON array")
	}
	var entries []struct{ Kind, Name, Example, Fixture string }
	if err := json.Unmarshal([]byte(text[start:end+1]), &entries); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{}
	for _, item := range Corpus {
		fixtures[item.Name] = item.Source
	}
	for _, entry := range entries {
		source, ok := fixtures[entry.Fixture]
		if !ok {
			t.Errorf("%s references missing fixture %q", entry.Name, entry.Fixture)
			continue
		}
		if entry.Kind != "icon" && entry.Example != source {
			t.Errorf("%s example differs from tested fixture", entry.Name)
		}
		if entry.Kind == "icon" && !strings.Contains(source, "("+entry.Name+")") {
			t.Errorf("%s missing from icon fixture", entry.Name)
		}
	}
}
