package doubao

import "testing"

// TestGetVideoInputRatio_BillsByResolution reproduces a real under-billing
// bug: doubao-seedance-2.0 was billed at a single flat video-input discount
// ratio regardless of output resolution, when the real upstream price
// varies by resolution (480p/720p vs 1080p) in addition to whether the
// request has video input at all.
func TestGetVideoInputRatio_BillsByResolution(t *testing.T) {
	cases := []struct {
		name       string
		model      string
		resolution string
		hasVideo   bool
		wantRatio  float64
		wantOK     bool
	}{
		{"480p no video is the baseline", "doubao-seedance-2-0-260128", "480p", false, 1.0, true},
		{"480p with video", "doubao-seedance-2-0-260128", "480p", true, 28.0 / 46.0, true},
		{"1080p no video costs more than the baseline", "doubao-seedance-2-0-260128", "1080p", false, 51.0 / 46.0, true},
		{"1080p with video", "doubao-seedance-2-0-260128", "1080p", true, 31.0 / 46.0, true},
		{"fast model 480p with video", "doubao-seedance-2-0-fast-260128", "480p", true, 22.0 / 37.0, true},
		{"fast model has no 1080p price, falls back to baseline", "doubao-seedance-2-0-fast-260128", "1080p", false, 1.0, true},
		{"4k no video", "doubao-seedance-2-0-260128", "4k", false, 26.0 / 46.0, true},
		{"4k with video", "doubao-seedance-2-0-260128", "4k", true, 16.0 / 46.0, true},
		{"4k resolution is case-insensitive", "doubao-seedance-2-0-260128", "4K", true, 16.0 / 46.0, true},
		{"unconfigured model", "doubao-seedance-1-0-pro-250528", "1080p", true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ratio, ok := GetVideoInputRatio(tc.model, tc.resolution, tc.hasVideo)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && ratio != tc.wantRatio {
				t.Fatalf("ratio = %v, want %v", ratio, tc.wantRatio)
			}
		})
	}
}
