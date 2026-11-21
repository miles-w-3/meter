// Package mermaid renders untrusted Mermaid in a bounded child process.
// A child process is not an OS sandbox: filesystem and network access are not
// prevented by this package.
package mermaid

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	pele "github.com/Broderick-Westrope/pele-go"
	_ "github.com/Broderick-Westrope/pele-go/all"
)

const MaxSource = 50_000
const MaxSVG = 4_000_000
const maxDimension = 8192
const maxResponse = 4*MaxSVG + 4096 // JSON escaping may expand SVG bytes
var slots = make(chan struct{}, 2)

type Result struct {
	SVG    string  `json:"svg"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type response struct {
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Render runs a private subcommand of the current executable. Limit concurrent
// helpers and kill and reap them on deadline or cancellation.
func Render(ctx context.Context, source string) (Result, error) {
	if len(source) == 0 || len(source) > MaxSource {
		return Result{}, errors.New("Mermaid source exceeds the allowed size or is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, exe, "__mermaid_render")
	cmd.Env = []string{}
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(source)
	var output limitedBuffer
	output.limit = maxResponse
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("Mermaid helper failed: %w", err)
	}
	var reply response
	if err := json.Unmarshal(output.Bytes(), &reply); err != nil {
		return Result{}, errors.New("invalid Mermaid helper response")
	}
	if reply.Error != "" {
		return Result{}, errors.New(reply.Error)
	}
	if reply.Result == nil {
		return Result{}, errors.New("missing Mermaid result")
	}
	if err := validate(*reply.Result); err != nil {
		return Result{}, err
	}
	return *reply.Result, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("Mermaid response too large")
	}
	return b.Buffer.Write(p)
}

// Helper is called only by the executable's private subcommand.
func Helper(in io.Reader, out io.Writer) error {
	source, err := io.ReadAll(io.LimitReader(in, MaxSource+1))
	if err != nil {
		return err
	}
	reply := response{}
	if len(source) == 0 || len(source) > MaxSource {
		reply.Error = "Mermaid source exceeds the allowed size or is empty"
	} else {
		result, renderErr := pele.Render(context.Background(), string(source), pele.Options{Links: pele.Ptr(false), Images: pele.Ptr(false), Limit: pele.Ptr(float64(MaxSource)), OutputLimit: pele.Ptr(float64(MaxSVG))})
		if renderErr != nil {
			reply.Error = "Unable to render Mermaid diagram"
		} else {
			// Resolve the preview's dark palette via Pele's public CSS tokens.
			const darkTokens = "--pele-bg:#17211b;--pele-fg:#edf3ee;--pele-muted:#b5c5b9;--pele-line:#c7d4c9;--pele-surface:#314137;--pele-surface-alt:#405347;--pele-border:#91a997;--pele-accent:#9abb91;"
			svg := strings.Replace(result.SVG, `style="`, `style="`+darkTokens, 1)
			r := Result{SVG: svg, Width: result.Width, Height: result.Height}
			if err := validate(r); err != nil {
				reply.Error = "Mermaid output rejected"
			} else {
				reply.Result = &r
			}
		}
	}
	return json.NewEncoder(out).Encode(reply)
}

func validate(r Result) error {
	if len(r.SVG) == 0 || len(r.SVG) > MaxSVG || r.Width <= 0 || r.Height <= 0 || r.Width > maxDimension || r.Height > maxDimension || math.IsNaN(r.Width) || math.IsNaN(r.Height) || math.IsInf(r.Width, 0) || math.IsInf(r.Height, 0) {
		return errors.New("invalid Mermaid output size")
	}
	decoder := xml.NewDecoder(strings.NewReader(r.SVG))
	depth := 0
	root := false
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid SVG XML")
		}
		switch t := tok.(type) {
		case xml.Directive, xml.ProcInst:
			return errors.New("SVG directives are not allowed")
		case xml.StartElement:
			depth++
			tag := strings.ToLower(t.Name.Local)
			if depth == 1 {
				if root || tag != "svg" || t.Name.Space != "http://www.w3.org/2000/svg" {
					return errors.New("invalid SVG root")
				}
				root = true
			}
			if tag == "script" || tag == "foreignobject" || tag == "image" || tag == "a" || tag == "iframe" || tag == "style" {
				return errors.New("unsafe SVG element")
			}
			for _, a := range t.Attr {
				name := strings.ToLower(a.Name.Local)
				value := strings.ToLower(a.Value)
				if strings.HasPrefix(name, "on") || name == "href" && !strings.HasPrefix(value, "#") || strings.Contains(value, "@import") || strings.Contains(value, "javascript:") || name == "style" && strings.Contains(value, `\`) || strings.Contains(value, "url(") && !onlyLocalURLs(value) {
					return errors.New("unsafe SVG attribute")
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if !root || depth != 0 {
		return errors.New("invalid SVG structure")
	}
	return nil
}

func onlyLocalURLs(s string) bool {
	for {
		i := strings.Index(s, "url(")
		if i < 0 {
			return true
		}
		s = s[i+4:]
		end := strings.IndexByte(s, ')')
		if end < 0 {
			return false
		}
		v := strings.Trim(strings.TrimSpace(s[:end]), "\"'")
		if !strings.HasPrefix(v, "#") || len(v) == 1 {
			return false
		}
		s = s[end+1:]
	}
}
