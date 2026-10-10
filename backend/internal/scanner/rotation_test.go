package scanner

import (
	"encoding/json"
	"testing"
)

func TestStreamRotation(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		want         int
	}{
		{"display matrix", `{"side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]}`, 270},
		{"display matrix positive", `{"side_data_list":[{"side_data_type":"Display Matrix","rotation":90}]}`, 90},
		{"upside down", `{"side_data_list":[{"rotation":180}]}`, 180},
		{"rotate tag", `{"tags":{"rotate":"90"}}`, 90},
		{"none", `{}`, 0},
	} {
		var s probeStream
		if err := json.Unmarshal([]byte(tc.stream), &s); err != nil {
			t.Fatal(err)
		}
		if got := streamRotation(s); got != tc.want {
			t.Errorf("%s: rotation %d, want %d", tc.name, got, tc.want)
		}
	}
}
