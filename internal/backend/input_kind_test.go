package backend

import "testing"

func TestDetectInputRadioTech(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cols []string
		want string
	}{
		{"nr_arfcn", []string{"Latitude", "Longitude", "NR-ARFCN", "PCI", "MCC", "MNC", "SSS-RSRP"}, InputRadioTech5G},
		{"earfcn", []string{"Latitude", "Longitude", "EARFCN", "PCI", "MCC", "MNC", "RSRP"}, InputRadioTechLTE},
		{"earfcn_dl_suffix", []string{"EARFCN (DL)", "PCI"}, InputRadioTechLTE},
		{"nr_before_earfcn_token", []string{"NR-ARFCN", "EARFCN-Dummy"}, InputRadioTech5G},
		{"unknown_frequency_only", []string{"Latitude", "Frequency", "RSRP"}, InputRadioTechUnknown},
		{"empty", []string{}, InputRadioTechUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DetectInputRadioTech(tc.cols); got != tc.want {
				t.Fatalf("DetectInputRadioTech(%v) = %q, want %q", tc.cols, got, tc.want)
			}
		})
	}
}
