package scoring

import "testing"

func TestMinMaxWithFloor(t *testing.T) {
	cases := []struct {
		name               string
		v, min, max, floor float64
		want               float64
	}{
		{name: "range above floor matches MinMax", v: 0.75, min: 0.5, max: 1, floor: 0.1, want: 0.5},
		{name: "range below floor scales by floor", v: 0.501, min: 0.5, max: 0.502, floor: 0.01, want: 0.1},
		{name: "top of small range stays small", v: 0.502, min: 0.5, max: 0.502, floor: 0.01, want: 0.2},
		{name: "flat range is zero", v: 0.5, min: 0.5, max: 0.5, floor: 0.01, want: 0},
		{name: "zero floor is plain MinMax", v: 0.501, min: 0.5, max: 0.502, floor: 0, want: 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MinMaxWithFloor(tc.v, tc.min, tc.max, tc.floor)
			if diff := got - tc.want; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("MinMaxWithFloor(%v, %v, %v, %v) = %v, want %v", tc.v, tc.min, tc.max, tc.floor, got, tc.want)
			}
		})
	}
}
