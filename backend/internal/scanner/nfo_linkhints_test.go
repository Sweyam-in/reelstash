package scanner

import (
	"encoding/xml"
	"testing"
)

func TestLinkHintsJellyfinPornDBIDs(t *testing.T) {
	const id = "0d9b1a3e-6f1f-4f7b-8d52-3f6a1c2b9e10"
	for name, doc := range map[string]string{
		"tpdb type":    `<movie><uniqueid type="tpdb">` + id + `</uniqueid></movie>`,
		"plugin key":   `<movie><uniqueid type="ThePornDBScene">` + id + `</uniqueid></movie>`,
		"tpdbid tag":   `<movie><tpdbid>` + id + `</tpdbid></movie>`,
		"theporndbid":  `<movie><theporndbid>` + id + `</theporndbid></movie>`,
		"reelstash id": `<movie><uniqueid type="theporndb">` + id + `</uniqueid><uniqueid type="reelstash">` + id + `</uniqueid></movie>`,
	} {
		var m nfoMovie
		if err := xml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		hints := m.LinkHints()
		if len(hints) != 1 || hints[0].Provider != "x-porndb" || hints[0].ExternalID != id {
			t.Errorf("%s: hints = %+v", name, hints)
		}
	}
	var m nfoMovie
	_ = xml.Unmarshal([]byte(`<movie><uniqueid type="reelstash">`+id+`</uniqueid></movie>`), &m)
	if h := m.LinkHints(); len(h) != 0 {
		t.Errorf("reelstash uid must not become a provider link: %+v", h)
	}
}

func TestLinkHintsKeepsScenePrefix(t *testing.T) {
	var m nfoMovie
	doc := `<movie><theporndbid>scenes/89f59eef-00a6-4fdf-93df-b5d5ef127374</theporndbid><uniqueid type="reelstash">95560d77-fad0-4b8d-badc-a8fb41309cf3</uniqueid></movie>`
	if err := xml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	h := m.LinkHints()
	if len(h) != 1 || h[0].Provider != "x-porndb" || h[0].ExternalID != "scenes/89f59eef-00a6-4fdf-93df-b5d5ef127374" {
		t.Errorf("hints = %+v", h)
	}
}
