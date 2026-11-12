package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"html"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// the view types are the json contract between the extractor and the theme.
// they carry only what a template needs: no ast handles, no file contents.
// doc fields hold already rendered html because the doc comments carry
// code blocks and links that the theme styles by class.

type symView struct {
	Name   string        `json:"name"`
	Sig    string        `json:"sig"`
	Doc    template.HTML `json:"doc,omitempty"`
	Anchor string        `json:"anchor"`
}

type typeView struct {
	symView
	Methods []symView `json:"methods"`
	Funcs   []symView `json:"funcs"`
}

type subpkgView struct {
	ImportPath string `json:"import_path"`
	Rel        string `json:"rel"`
	Synopsis   string `json:"synopsis"`
}

type pkgView struct {
	ImportPath string        `json:"import_path"`
	Name       string        `json:"name"`
	Repo       string        `json:"repo"`
	Branch     string        `json:"branch"`
	Rel        string        `json:"rel"`
	Synopsis   string        `json:"synopsis"`
	Doc        template.HTML `json:"doc"`
	GitHub     string        `json:"github"`
	Module     string        `json:"module"`
	Date       string        `json:"date"`
	IsCommand  bool          `json:"is_command"`
	HasExports bool          `json:"has_exports"`

	Consts  []symView    `json:"consts"`
	Vars    []symView    `json:"vars"`
	Funcs   []symView    `json:"funcs"`
	Types   []typeView   `json:"types"`
	Subpkgs []subpkgView `json:"subpkgs"`
}

// extractRepo documents every go package found in a cloned repo. the first
// returned view is the repo root package when the root contains go files;
// the others are the subpackages, sorted by path.
func extractRepo(dir, org, name, branch, vanityHost string) ([]*pkgView, error) {
	modPath := detectModPath(dir)
	if modPath == "" {
		return nil, fmt.Errorf("no go.mod in %s", dir)
	}

	type hit struct {
		rel  string
		view *pkgView
	}
	var hits []hit

	dirs := collectPackageDirs(dir)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no go packages in %s", dir)
	}
	for _, d := range dirs {
		rel, _ := filepath.Rel(dir, d)
		if rel == "." {
			rel = ""
		}
		view, err := extractPackage(d, rel, org, name, branch, modPath, vanityHost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gendoc: skip %s/%s: %v\n", name, rel, err)
			continue
		}
		hits = append(hits, hit{rel, view})
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rel < hits[j].rel })

	var subs []subpkgView
	for _, h := range hits {
		if h.rel != "" {
			subs = append(subs, subpkgView{
				ImportPath: h.view.ImportPath,
				Rel:        h.rel,
				Synopsis:   h.view.Synopsis,
			})
		}
	}
	var views []*pkgView
	for _, h := range hits {
		if h.rel == "" && len(subs) > 0 {
			h.view.Subpkgs = subs
		}
		views = append(views, h.view)
	}
	return views, nil
}

// collectPackageDirs walks the repo and keeps every directory that holds at
// least one non test go file.
func collectPackageDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		base := d.Name()
		if strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" {
			return filepath.SkipDir
		}
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				dirs = append(dirs, path)
				break
			}
		}
		return nil
	})
	return dirs
}

func detectModPath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func extractPackage(dir, rel, org, name, branch, modPath, vanityHost string) (*pkgView, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var astPkg *ast.Package
	for pname, p := range pkgs {
		if !strings.HasSuffix(pname, "_test") {
			astPkg = p
			break
		}
	}
	if astPkg == nil {
		return nil, fmt.Errorf("no package")
	}

	// doc.New works purely from the ast. NewFromFiles drops symbols when the
	// import path does not line up with what it expects, and the module paths
	// of these repos rarely agree with the vanity paths shown on the site.
	dpkg := doc.New(astPkg, modPath+joinRel(rel), 0)

	vanity := vanityHost + "/" + name
	githubURL := "https://github.com/" + org + "/" + name
	if rel != "" {
		vanity += "/" + rel
		githubURL += "/tree/" + branch + "/" + rel
	}

	view := &pkgView{
		ImportPath: vanity,
		Name:       dpkg.Name,
		Repo:       name,
		Branch:     branch,
		Rel:        rel,
		Synopsis:   doc.Synopsis(dpkg.Doc),
		Doc:        template.HTML(renderDoc(dpkg.Doc)),
		GitHub:     githubURL,
		Module:     modPath,
		Date:       time.Now().UTC().Format("2006-01-02"),
		IsCommand:  astPkg.Name == "main",
	}
	for _, c := range dpkg.Consts {
		view.Consts = append(view.Consts, mkSym(fset, strings.Join(c.Names, ", "), c.Decl, c.Doc))
	}
	for _, v := range dpkg.Vars {
		view.Vars = append(view.Vars, mkSym(fset, strings.Join(v.Names, ", "), v.Decl, v.Doc))
	}
	for _, f := range dpkg.Funcs {
		view.Funcs = append(view.Funcs, mkSym(fset, f.Name, f.Decl, f.Doc))
	}
	for _, t := range dpkg.Types {
		tv := typeView{symView: mkSym(fset, t.Name, t.Decl, t.Doc)}
		for _, m := range t.Methods {
			tv.Methods = append(tv.Methods, mkSym(fset, m.Name, m.Decl, m.Doc))
		}
		for _, f := range t.Funcs {
			tv.Funcs = append(tv.Funcs, mkSym(fset, f.Name, f.Decl, f.Doc))
		}
		view.Types = append(view.Types, tv)
	}
	view.HasExports = len(view.Consts)+len(view.Vars)+len(view.Funcs)+len(view.Types) > 0
	return view, nil
}

func joinRel(rel string) string {
	if rel == "" {
		return ""
	}
	return "/" + rel
}

func mkSym(fset *token.FileSet, name string, decl ast.Decl, docStr string) symView {
	return symView{
		Name:   name,
		Sig:    printDecl(fset, decl),
		Doc:    template.HTML(renderDoc(docStr)),
		Anchor: anchor(name),
	}
}

func printDecl(fset *token.FileSet, decl ast.Decl) string {
	var buf bytes.Buffer
	p := &printer{fset: fset}
	p.print(&buf, decl)
	return strings.TrimSpace(buf.String())
}

// printer renders one declaration back to compact go source. go/printer
// always emits full source, which would pull whole import blocks and
// unexported fields into a signature.
type printer struct{ fset *token.FileSet }

func (p *printer) print(w *bytes.Buffer, node ast.Node) {
	switch n := node.(type) {
	case *ast.FuncDecl:
		w.WriteString("func ")
		if n.Recv != nil && len(n.Recv.List) > 0 {
			w.WriteString("(")
			p.fieldList(w, n.Recv)
			w.WriteString(") ")
		}
		w.WriteString(n.Name.Name)
		p.funcType(w, n.Type)
	case *ast.GenDecl:
		w.WriteString(n.Tok.String())
		if len(n.Specs) == 1 {
			w.WriteString(" ")
			p.spec(w, n.Specs[0])
		} else {
			w.WriteString(" (\n")
			for _, s := range n.Specs {
				w.WriteString("\t")
				p.spec(w, s)
				w.WriteString("\n")
			}
			w.WriteString(")")
		}
	}
}

func (p *printer) spec(w *bytes.Buffer, spec ast.Spec) {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		names := make([]string, len(s.Names))
		for i, n := range s.Names {
			names[i] = n.Name
		}
		w.WriteString(strings.Join(names, ", "))
		if s.Type != nil {
			w.WriteString(" ")
			p.expr(w, s.Type)
		}
		if len(s.Values) > 0 {
			w.WriteString(" = ")
			for i, v := range s.Values {
				if i > 0 {
					w.WriteString(", ")
				}
				p.expr(w, v)
			}
		}
	case *ast.TypeSpec:
		w.WriteString(s.Name.Name)
		if s.TypeParams != nil {
			w.WriteString("[")
			p.fieldList(w, s.TypeParams)
			w.WriteString("]")
		}
		w.WriteString(" ")
		p.expr(w, s.Type)
	case *ast.ImportSpec:
		if s.Name != nil {
			w.WriteString(s.Name.Name + " ")
		}
		w.WriteString(s.Path.Value)
	}
}

func (p *printer) funcType(w *bytes.Buffer, ft *ast.FuncType) {
	if ft.TypeParams != nil {
		w.WriteString("[")
		p.fieldList(w, ft.TypeParams)
		w.WriteString("]")
	}
	w.WriteString("(")
	p.fieldList(w, ft.Params)
	w.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		if len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) == 0 {
			w.WriteString(" ")
			p.expr(w, ft.Results.List[0].Type)
		} else {
			w.WriteString(" (")
			p.fieldList(w, ft.Results)
			w.WriteString(")")
		}
	}
}

func (p *printer) fieldList(w *bytes.Buffer, fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for i, f := range fl.List {
		if i > 0 {
			w.WriteString(", ")
		}
		for j, n := range f.Names {
			if j > 0 {
				w.WriteString(", ")
			}
			w.WriteString(n.Name)
		}
		if len(f.Names) > 0 {
			w.WriteString(" ")
		}
		p.expr(w, f.Type)
	}
}

func (p *printer) expr(w *bytes.Buffer, e ast.Expr) {
	switch n := e.(type) {
	case *ast.Ident:
		w.WriteString(n.Name)
	case *ast.SelectorExpr:
		p.expr(w, n.X)
		w.WriteString(".")
		w.WriteString(n.Sel.Name)
	case *ast.StarExpr:
		w.WriteString("*")
		p.expr(w, n.X)
	case *ast.ArrayType:
		w.WriteString("[")
		if n.Len != nil {
			p.expr(w, n.Len)
		}
		w.WriteString("]")
		p.expr(w, n.Elt)
	case *ast.MapType:
		w.WriteString("map[")
		p.expr(w, n.Key)
		w.WriteString("]")
		p.expr(w, n.Value)
	case *ast.ChanType:
		switch n.Dir {
		case ast.SEND:
			w.WriteString("chan<- ")
		case ast.RECV:
			w.WriteString("<-chan ")
		default:
			w.WriteString("chan ")
		}
		p.expr(w, n.Value)
	case *ast.FuncType:
		w.WriteString("func")
		p.funcType(w, n)
	case *ast.InterfaceType:
		if n.Methods != nil && len(n.Methods.List) > 0 {
			w.WriteString("interface{ ... }")
		} else {
			w.WriteString("interface{}")
		}
	case *ast.StructType:
		if n.Fields == nil || len(n.Fields.List) == 0 {
			w.WriteString("struct{}")
		} else if len(n.Fields.List) > 12 {
			w.WriteString("struct{ ... }")
		} else {
			w.WriteString("struct {\n")
			for _, f := range n.Fields.List {
				w.WriteString("\t")
				for j, nm := range f.Names {
					if j > 0 {
						w.WriteString(", ")
					}
					w.WriteString(nm.Name)
				}
				if len(f.Names) > 0 {
					w.WriteString(" ")
				}
				p.expr(w, f.Type)
				if f.Tag != nil {
					w.WriteString(" " + f.Tag.Value)
				}
				w.WriteString("\n")
			}
			w.WriteString("}")
		}
	case *ast.Ellipsis:
		w.WriteString("...")
		if n.Elt != nil {
			p.expr(w, n.Elt)
		}
	case *ast.BasicLit:
		w.WriteString(n.Value)
	case *ast.UnaryExpr:
		w.WriteString(n.Op.String())
		p.expr(w, n.X)
	case *ast.BinaryExpr:
		p.expr(w, n.X)
		w.WriteString(" " + n.Op.String() + " ")
		p.expr(w, n.Y)
	case *ast.ParenExpr:
		w.WriteString("(")
		p.expr(w, n.X)
		w.WriteString(")")
	case *ast.IndexExpr:
		p.expr(w, n.X)
		w.WriteString("[")
		p.expr(w, n.Index)
		w.WriteString("]")
	case *ast.IndexListExpr:
		p.expr(w, n.X)
		w.WriteString("[")
		for i, idx := range n.Indices {
			if i > 0 {
				w.WriteString(", ")
			}
			p.expr(w, idx)
		}
		w.WriteString("]")
	default:
		w.WriteString("_")
	}
}

// renderDoc turns a go doc comment into themed html. indented runs become
// code blocks and words that look like urls become links.
func renderDoc(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var out strings.Builder
	var buf strings.Builder
	inCode := false

	flush := func() {
		s := strings.TrimSpace(buf.String())
		buf.Reset()
		if s == "" {
			return
		}
		if inCode {
			out.WriteString(`<pre class="godoc-pre"><code class="language-go">`)
			out.WriteString(html.EscapeString(s))
			out.WriteString("</code></pre>\n")
		} else {
			out.WriteString("<p>")
			out.WriteString(linkify(html.EscapeString(s)))
			out.WriteString("</p>\n")
		}
	}

	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			inCode = false
			continue
		}
		isCode := len(line) > 0 && (line[0] == '\t' || (len(line) > 1 && line[0] == ' ' && line[1] == ' '))
		if isCode {
			if !inCode {
				flush()
				inCode = true
			}
			buf.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "\t"), "  "))
			buf.WriteString("\n")
		} else {
			if inCode {
				flush()
				inCode = false
			}
			if buf.Len() > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(strings.TrimSpace(line))
		}
	}
	flush()
	return out.String()
}

func linkify(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		clean := strings.TrimRight(w, ".,;:)")
		if strings.HasPrefix(clean, "https://") || strings.HasPrefix(clean, "http://") {
			words[i] = `<a href="` + clean + `" class="godoc-link">` + clean + `</a>` + w[len(clean):]
		}
	}
	return strings.Join(words, " ")
}

func anchor(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.ToLower(b.String())
}
