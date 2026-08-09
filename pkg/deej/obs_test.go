package deej

import (
	"math"
	"testing"
)

func TestOBSVolumeMulFromPercent(t *testing.T) {
	cases := []struct {
		percent float32
		convert bool
		want    float64
	}{
		{0, true, 0},
		{0.5, true, 0.125},
		{1, true, 1},
		{0.5, false, 0.5},
		{-1, false, 0},
		{2, false, 1},
	}

	for _, testCase := range cases {
		got := obsVolumeMulFromPercent(testCase.percent, testCase.convert)
		if math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("obsVolumeMulFromPercent(%v, %v) = %v, want %v", testCase.percent, testCase.convert, got, testCase.want)
		}
	}
}

func TestOBSVolumeConversionDefaultsOn(t *testing.T) {
	if !defaultSettings().OBS.VolumeConversion {
		t.Fatal("OBS volume conversion should default on for configs without the key")
	}
}
