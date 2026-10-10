package scanner

import (
	"encoding/json"
	"testing"
)

const sampleProbe = `{"streams":[
 {"index":0,"codec_type":"video","codec_name":"h264"},
 {"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"jpn","title":"Stereo"},"disposition":{"default":1,"forced":0}},
 {"index":2,"codec_type":"audio","codec_name":"ac3","channels":6,"tags":{"language":"eng"},"disposition":{"default":0}},
 {"index":3,"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Full"},"disposition":{"default":0,"forced":0}},
 {"index":4,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"spa"},"disposition":{"forced":1}},
 {"index":5,"codec_type":"attachment","codec_name":"ttf"}
],"format":{}}`

func TestTracksFromStreams(t *testing.T) {
	var parsed probeOutput
	if err := json.Unmarshal([]byte(sampleProbe), &parsed); err != nil {
		t.Fatal(err)
	}
	tracks := tracksFromStreams(parsed.Streams)
	if len(tracks) != 4 {
		t.Fatalf("got %d tracks, want 4 (audio and subtitle only): %+v", len(tracks), tracks)
	}
	want := []Track{
		{Kind: "audio", KindIndex: 0, StreamIndex: 1, Codec: "aac", Language: "jpn", Title: "Stereo", Channels: 2, Default: true},
		{Kind: "audio", KindIndex: 1, StreamIndex: 2, Codec: "ac3", Language: "eng", Channels: 6},
		{Kind: "subtitle", KindIndex: 0, StreamIndex: 3, Codec: "subrip", Language: "eng", Title: "Full"},
		{Kind: "subtitle", KindIndex: 1, StreamIndex: 4, Codec: "hdmv_pgs_subtitle", Language: "spa", Forced: true},
	}
	for i := range want {
		if tracks[i] != want[i] {
			t.Errorf("track %d = %+v, want %+v", i, tracks[i], want[i])
		}
	}
}
