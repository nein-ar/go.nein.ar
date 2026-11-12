package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type repoView struct {
	Repo     string `json:"repo"`
	Synopsis string `json:"synopsis"`
	Date     string `json:"date"`
	Pkgs     int    `json:"pkgs"`
}

// emit materialises the extraction result as djot sources and json data.
// pages stay tiny and carry only an import path key; templates read
// everything else from the data layer, so the theme never sees a file path.
func emit(cfg *appConfig, pkgs []*pkgView) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.ContentDir, 0o755); err != nil {
		return err
	}

	byKey := make(map[string]*pkgView, len(pkgs))
	repos := make([]repoView, 0, len(pkgs))
	for _, p := range pkgs {
		byKey[p.ImportPath] = p
		if p.Rel == "" {
			repos = append(repos, repoView{
				Repo:     p.Repo,
				Synopsis: p.Synopsis,
				Date:     p.Date,
				Pkgs:     countRepos(pkgs, p.Repo),
			})
		}
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Repo < repos[j].Repo })

	if err := writeJSON(filepath.Join(cfg.DataDir, "pkgs.json"), map[string]any{"by_key": byKey}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cfg.DataDir, "repos.json"), repos); err != nil {
		return err
	}

	if err := writePage(cfg, "_index", homeSource()); err != nil {
		return err
	}
	// the host serves this for every missing path
	if err := writePage(cfg, "404", notFoundSource()); err != nil {
		return err
	}
	for _, p := range pkgs {
		rel := path.Join("pkg", p.Repo)
		if p.Rel != "" {
			rel = path.Join(rel, p.Rel)
		}
		if err := writePage(cfg, rel, pkgSource(p)); err != nil {
			return err
		}
	}
	return nil
}

func writePage(cfg *appConfig, rel, src string) error {
	dst := filepath.Join(cfg.ContentDir, rel+".djot")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(src), 0o644)
}

func countRepos(pkgs []*pkgView, repo string) int {
	n := 0
	for _, p := range pkgs {
		if p.Repo == repo {
			n++
		}
	}
	return n
}

func writeJSON(dst string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dst, append(data, '\n'), 0o644)
}

func homeSource() string {
	return source(
		"go packages hosted at **github.com/nein-ar**, importable as `go.nein.ar/<pkg>`.\n\n"+
			"```\ngo get go.nein.ar/<pkg>@latest\n```\n\n"+
			"browse a package below or visit <https://github.com/nein-ar> for source.\n",
		"title", "go.nein.ar",
		"layout", "home",
		"section_name", "home",
		"date", "2026-01-01",
	)
}

func notFoundSource() string {
	return source("", "title", "404", "layout", "404", "url", "404.html")
}

func pkgSource(p *pkgView) string {
	return source("", "title", p.Name, "layout", "pkg", "import", p.ImportPath)
}

// source renders front matter from key value pairs, then the body.
func source(body string, pairs ...string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for i := 0; i < len(pairs); i += 2 {
		b.WriteString(kv(pairs[i], pairs[i+1]) + "\n")
	}
	b.WriteString("---\n")
	if body != "" {
		b.WriteString("\n" + body)
	}
	return b.String()
}

func kv(key, value string) string {
	return key + " = " + fmt.Sprintf("%q", value)
}

// metaPage is the static stand in for the old vanity import function. the go
// tool reads the go-import tag from /<repo>/?go-get=1 and browsers are sent
// to the package page.
func metaPage(cfg *appConfig, p *pkgView) []byte {
	repoURL := "https://github.com/" + cfg.Org + "/" + p.Repo
	return fmt.Appendf(nil, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="go-import" content="%s git %s">
<meta name="go-source" content="%s %s %s/tree/%s{/dir} %s/blob/%s{/dir}/{file}#L{line}">
<meta http-equiv="refresh" content="0; url=%spkg/%s/">
<title>%s</title>
</head>
<body>
go.nein.ar/<a href="%spkg/%s/">%s</a>
</body>
</html>
`,
		p.ImportPath, repoURL,
		p.ImportPath, repoURL, repoURL, p.Branch, repoURL, p.Branch,
		cfg.BaseURL, p.Repo, p.ImportPath,
		cfg.BaseURL, p.Repo, p.Repo,
	)
}

// sitemap renders a minimal sitemap.xml listing every package page.
func sitemap(cfg *appConfig, pkgs []*pkgView) []byte {
	var xml strings.Builder
	xml.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)
	xml.WriteString(fmt.Sprintf("  <url><loc>%s</loc></url>\n", cfg.BaseURL))
	for _, p := range pkgs {
		u := cfg.BaseURL + "pkg/" + p.Repo + "/"
		if p.Rel != "" {
			u += p.Rel + "/"
		}
		xml.WriteString(fmt.Sprintf("  <url><loc>%s</loc></url>\n", u))
	}
	xml.WriteString("</urlset>\n")
	return []byte(xml.String())
}
