// gendoc documents the go packages of the nein-ar org on go.nein.ar. it
// collects the public repos, extracts package docs with go/doc and renders
// the site through the dejot ssg.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/nein-ar/dejot/ssg"
)

// appConfig is the ssg configuration plus the settings the collector and the
// emitter need. everything lives in one config.toml; the extra keys land in
// ssg params as well, where the theme reads them.
type appConfig struct {
	*ssg.Config
	Org        string
	ClonesDir  string
	VanityHost string
}

func loadAppConfig(path string) (*appConfig, error) {
	base, err := ssg.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	cfg := &appConfig{
		Config:     base,
		Org:        strParam(base, "org", "nein-ar"),
		ClonesDir:  strParam(base, "clones_dir", filepath.Join(base.CacheDir, "clones")),
		VanityHost: strParam(base, "vanity_host", "go.nein.ar"),
	}
	return cfg, nil
}

func strParam(cfg *ssg.Config, key, def string) string {
	if v, ok := cfg.Param(key).(string); ok && v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(0)

	path := "config.toml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	cfg, err := loadAppConfig(path)
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}

	pkgs, err := document(cfg)
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}
	if err := emit(cfg, pkgs); err != nil {
		log.Fatalf("fatal: %v", err)
	}

	site, err := ssg.Load(cfg.Config)
	if err != nil {
		log.Fatalf("fatal: content load failed: %v", err)
	}
	for _, p := range pkgs {
		if p.Rel == "" {
			site.AddOutput(filepath.Join(p.Repo, "index.html"), metaPage(cfg, p))
		}
	}
	site.AddOutput("sitemap.xml", sitemap(cfg, pkgs))
	result, err := site.Build()
	if err != nil {
		log.Fatalf("fatal: site build failed: %v", err)
	}
	if result.Skipped {
		fmt.Println("site: up to date")
	} else {
		fmt.Printf("site: %d pages, %d written, %d removed\n", result.Pages, result.Written, result.Removed)
	}
}

// document runs the collection and extraction stages. a repo that fails to
// clone or holds no go code is skipped with a warning, never fatal.
func document(cfg *appConfig) ([]*pkgView, error) {
	repos, err := listRepos(cfg.Org)
	if err != nil {
		return nil, err
	}
	var pkgs []*pkgView
	for _, r := range repos {
		dir, err := clone(r, cfg.Org, cfg.ClonesDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gendoc: warn: %v\n", err)
			continue
		}
		views, err := extractRepo(dir, cfg.Org, r.Name, r.Branch, cfg.VanityHost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gendoc: warn: %s: %v\n", r.Name, err)
			continue
		}
		pkgs = append(pkgs, views...)
	}
	return pkgs, nil
}
