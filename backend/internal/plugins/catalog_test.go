package plugins

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogBrowseAndInstall(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	pub := newPublisher(t, "1.0.0", "1.2.0")

	for _, bad := range [][]string{{"ftp://x/index.json"}, {"not a url"}} {
		if _, err := r.m.SetCatalogs(ctx, bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	saved, err := r.m.SetCatalogs(ctx, []string{" " + pub.index() + " ", pub.index(), "", "http://127.0.0.1:1/missing.json"})
	if err != nil || len(saved) != 2 || saved[0] != pub.index() {
		t.Fatalf("saved = %v, %v", saved, err)
	}

	// The unreachable catalog reports its error; the good one lists its plugin.
	sources := r.m.BrowseCatalogs(ctx)
	if len(sources) != 2 || sources[1].Error == "" {
		t.Fatalf("sources = %+v", sources)
	}
	if items := sources[0].Items; len(items) != 1 || items[0].ID != "upd" || items[0].Latest != "1.2.0" || items[0].Installed != "" {
		t.Fatalf("items = %+v", items)
	}

	// Only saved catalogs can be installed from, and a bad checksum installs nothing.
	if _, err := r.m.InstallFromCatalog(ctx, "https://elsewhere.example/index.json", "upd"); err == nil {
		t.Error("an unsaved catalog must be refused")
	}
	pub.badSum = true
	if _, err := r.m.InstallFromCatalog(ctx, pub.index(), "upd"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("a corrupt download: %v", err)
	}
	if _, err := r.m.Get(ctx, "upd"); err == nil {
		t.Error("a refused install left the plugin installed")
	}
	pub.badSum = false

	info, err := r.m.InstallFromCatalog(ctx, pub.index(), "upd")
	if err != nil || info.Version != "1.2.0" {
		t.Fatalf("install = %+v, %v", info, err)
	}
	if src := r.m.UpdateSource(ctx, "upd"); src != pub.index() {
		t.Errorf("the catalog did not become the update source: %q", src)
	}
	if _, err := r.m.InstallFromCatalog(ctx, pub.index(), "nope"); err == nil {
		t.Error("a plugin the catalog does not list must be refused")
	}

	// Once installed, the catalog shows the installed version.
	pub.releases["1.3.0"] = versioned("1.3.0")
	pub.latest = "1.3.0"
	item := r.m.BrowseCatalogs(ctx)[0].Items[0]
	if item.Installed != "1.2.0" || !item.Update {
		t.Errorf("after install: %+v", item)
	}
}
