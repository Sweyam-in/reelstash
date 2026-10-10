package provider

import (
	"context"
	"testing"
)

type fake struct {
	id      string
	enabled bool
	caps    Caps
	hosts   []string
}

func (f fake) ID() string                        { return f.id }
func (f fake) Name() string                      { return f.id }
func (f fake) Capabilities() Caps                { return f.caps }
func (f fake) Enabled() bool                     { return f.enabled }
func (f fake) ConfigFields() []Field             { return nil }
func (f fake) Configure(map[string]string) error { return nil }
func (f fake) Test(context.Context) error        { return nil }
func (f fake) ImageHosts() []string              { return f.hosts }
func (f fake) SearchMedia(context.Context, MediaQuery) ([]MediaCandidate, error) {
	return nil, nil
}
func (f fake) SearchPerformers(context.Context, string) ([]PerformerResult, error) {
	return nil, ErrUnsupported
}
func (f fake) GetPerformer(context.Context, string) (PerformerResult, error) {
	return PerformerResult{}, ErrUnsupported
}
func (f fake) SearchStudios(context.Context, string) ([]StudioResult, error) {
	return nil, ErrUnsupported
}
func (f fake) GetStudio(context.Context, string) (StudioResult, error) {
	return StudioResult{}, ErrUnsupported
}
func (f fake) GetMedia(context.Context, string) (MediaResult, error) { return MediaResult{}, nil }

func TestRegistryPriorityAndFiltering(t *testing.T) {
	r := NewRegistry()
	r.Register(fake{id: "a", enabled: true, caps: Caps{Media: true}})
	r.Register(fake{id: "b", enabled: true, caps: Caps{Media: true, Performer: true}})
	r.Register(fake{id: "c", enabled: false, caps: Caps{Media: true}})

	ids := func(ps []Provider) (out []string) {
		for _, p := range ps {
			out = append(out, p.ID())
		}
		return
	}
	if got := ids(r.Enabled(KindMedia)); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("default order = %v", got)
	}
	r.SetOrder([]string{"b", "a"})
	if got := ids(r.Enabled(KindMedia)); got[0] != "b" || got[1] != "a" {
		t.Fatalf("custom order = %v", got)
	}
	if got := ids(r.Enabled(KindPerformer)); len(got) != 1 || got[0] != "b" {
		t.Fatalf("performer providers = %v", got)
	}
}

func TestImageHostAllowed(t *testing.T) {
	r := NewRegistry()
	r.Register(fake{id: "a", hosts: []string{"alpha.example"}})
	r.Register(fake{id: "t", hosts: []string{"image.tmdb.org"}})
	for host, want := range map[string]bool{
		"alpha.example": true, "cdn.alpha.example": true, "image.tmdb.org": true,
		"evil-alpha.example": false, "tmdb.org": false, "example.com": false,
	} {
		if got := r.ImageHostAllowed(host); got != want {
			t.Errorf("ImageHostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}
