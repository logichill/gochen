//go:build ignore

// Run from the repository root: GOWORK=off go run scripts/dependency_graph.go -out /tmp/gochen-dependencies
// This is a documentation generator, not an architecture gate.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type listedPackage struct {
	ImportPath, Dir                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
}
type location struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Role string `json:"role"`
}
type dependency struct {
	Source    string     `json:"source"`
	Target    string     `json:"target"`
	Locations []location `json:"locations"`
}
type graph struct {
	Direction    string       `json:"direction"`
	Packages     []string     `json:"packages"`
	Dependencies []dependency `json:"dependencies"`
}

func main() {
	out := flag.String("out", "", "required output directory (generated files are replaced)")
	flag.Parse()
	if *out == "" {
		fail(fmt.Errorf("-out is required"))
	}
	absoluteOut, err := filepath.Abs(*out)
	fail(err)
	*out = absoluteOut
	root, err := os.Getwd()
	fail(err)
	cmd := exec.Command("go", "list", "-json", "./...")
	// Force module isolation even if the caller is in a workspace.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOWORK=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off")
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	fail(err)
	dec := json.NewDecoder(bytes.NewReader(data))
	g := graph{Direction: "importer -> imported package"}
	edges := map[string]*dependency{}
	fs := token.NewFileSet()
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		fail(err)
		g.Packages = append(g.Packages, p.ImportPath)
		groups := []struct {
			role  string
			files []string
		}{
			{"production", append(append([]string{}, p.GoFiles...), p.CgoFiles...)},
			{"test", append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...)},
		}
		for _, group := range groups {
			role := group.role
			if role == "production" && strings.HasPrefix(p.ImportPath, "gochen/examples/") {
				role = "example"
			}
			for _, name := range group.files {
				path := filepath.Join(p.Dir, name)
				f, err := parser.ParseFile(fs, path, nil, parser.ImportsOnly)
				fail(err)
				rel, err := filepath.Rel(root, path)
				fail(err)
				for _, imp := range f.Imports {
					target, err := strconv.Unquote(imp.Path.Value)
					fail(err)
					key := p.ImportPath + "\x00" + target
					if edges[key] == nil {
						edges[key] = &dependency{Source: p.ImportPath, Target: target}
					}
					edges[key].Locations = append(edges[key].Locations, location{filepath.ToSlash(rel), fs.Position(imp.Pos()).Line, role})
				}
			}
		}
	}
	sort.Strings(g.Packages)
	keys := make([]string, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		e := edges[key]
		sort.Slice(e.Locations, func(i, j int) bool {
			a, b := e.Locations[i], e.Locations[j]
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Line < b.Line
		})
		g.Dependencies = append(g.Dependencies, *e)
	}
	fail(os.MkdirAll(*out, 0755))
	data, err = json.MarshalIndent(g, "", "  ")
	fail(err)
	fail(os.WriteFile(filepath.Join(*out, "dependency-graph.json"), append(data, '\n'), 0644))
	var md, mermaid strings.Builder
	md.WriteString("# Gochen 依赖来源索引\n\n箭头：引用方 → 被引用方。按当前构建环境的 go list 解析；忽略未启用的构建标签文件。测试与示例单独标记，标准库导入一并保留。行号随代码变化，请重新生成。\n\n")
	mermaid.WriteString("flowchart LR\n")
	ids := map[string]string{}
	for i, p := range g.Packages {
		ids[p] = fmt.Sprintf("p%d", i)
	}
	used := map[string]bool{}
	for _, e := range g.Dependencies {
		for _, l := range e.Locations {
			if l.Role == "production" && strings.HasPrefix(e.Target, "gochen/") {
				used[e.Source] = true
				used[e.Target] = true
				break
			}
		}
	}
	for _, p := range g.Packages {
		if used[p] {
			fmt.Fprintf(&mermaid, "  %s[%q]\n", ids[p], p)
		}
	}
	for _, e := range g.Dependencies {
		fmt.Fprintf(&md, "## %s → %s\n\n", e.Source, e.Target)
		production := false
		for _, l := range e.Locations {
			relative, err := filepath.Rel(*out, filepath.Join(root, filepath.FromSlash(l.File)))
			fail(err)
			fmt.Fprintf(&md, "- %s: [%s:%d](%s#L%d)\n", l.Role, l.File, l.Line, filepath.ToSlash(relative), l.Line)
			production = production || l.Role == "production"
		}
		md.WriteString("\n")
		if production && strings.HasPrefix(e.Target, "gochen/") {
			fmt.Fprintf(&mermaid, "  %s --> %s\n", ids[e.Source], ids[e.Target])
		}
	}
	fail(os.WriteFile(filepath.Join(*out, "DEPENDENCIES.md"), []byte(md.String()), 0644))
	fail(os.WriteFile(filepath.Join(*out, "dependencies.mmd"), []byte(mermaid.String()), 0644))
	fmt.Printf("Generated %d listed packages and %d import pairs in %s\n", len(g.Packages), len(g.Dependencies), *out)
}
func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
