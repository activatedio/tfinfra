package tf

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// fieldComments reads the doc comments protoc-gen-go copies from a proto
// field onto its Go struct field: the proto's own documentation, which the
// compiled descriptors do not carry. They are read from the Go source of
// the message's package, located through the build context.
var fieldComments = &commentIndex{byPackage: map[string]map[string]map[string]string{}}

type commentIndex struct {
	mu sync.Mutex
	// byPackage is package path -> struct name -> Go field name -> comment.
	byPackage map[string]map[string]map[string]string
}

// For returns the comment on t's Go field goName, as one paragraph per
// blank-line-separated block, or "" when it has none.
func (c *commentIndex) For(t reflect.Type, goName string) string {

	c.mu.Lock()
	defer c.mu.Unlock()

	pkg, ok := c.byPackage[t.PkgPath()]
	if !ok {
		pkg = loadComments(t.PkgPath())
		c.byPackage[t.PkgPath()] = pkg
	}

	return pkg[t.Name()][goName]
}

// loadComments parses the package's non-test Go files for struct field
// comments. A package it cannot find or parse fails generation: a silent
// miss would drop every description without a trace.
func loadComments(pkgPath string) map[string]map[string]string {

	wd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("field comments for %s: %v", pkgPath, err))
	}

	bp, err := build.Import(pkgPath, wd, build.FindOnly)
	if err != nil {
		panic(fmt.Sprintf("field comments for %s: %v", pkgPath, err))
	}

	files, err := filepath.Glob(filepath.Join(bp.Dir, "*.go"))
	if err != nil {
		panic(fmt.Sprintf("field comments for %s: %v", pkgPath, err))
	}

	out := map[string]map[string]string{}
	fset := token.NewFileSet()

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			panic(fmt.Sprintf("field comments for %s: %v", pkgPath, err))
		}
		collectStructComments(f, out)
	}

	return out
}

func collectStructComments(f *ast.File, out map[string]map[string]string) {

	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		fields := map[string]string{}
		for _, fl := range st.Fields.List {
			text := commentText(fl.Doc)
			if text == "" {
				text = commentText(fl.Comment)
			}
			if text == "" {
				continue
			}
			for _, name := range fl.Names {
				fields[name.Name] = text
			}
		}
		out[ts.Name.Name] = fields
		return false
	})
}

// commentText joins a comment's lines into paragraphs: a proto comment is
// wrapped prose, and a description renders as markdown.
func commentText(cg *ast.CommentGroup) string {

	if cg == nil {
		return ""
	}

	var paragraphs []string
	for _, p := range strings.Split(strings.TrimSpace(cg.Text()), "\n\n") {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			paragraphs = append(paragraphs, p)
		}
	}

	return strings.Join(paragraphs, "\n\n")
}
