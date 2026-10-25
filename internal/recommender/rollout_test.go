package recommender

import (
	"reflect"
	"testing"
)

func TestTopKNormalized(t *testing.T) {
	got := topKNormalized([]float64{1, 4, 2, 3}, 2)
	if got[0] != 0 || got[2] != 0 {
		t.Fatalf("outside top-k not zero: %v", got)
	}
	var sum float64
	for _, v := range got {
		sum += v
	}
	if sum != 1 {
		t.Fatalf("top-k sum = %v, want 1", sum)
	}

	if got := topKNormalized([]float64{1, 2, 3}, 3); !reflect.DeepEqual(got, []float64{1.0 / 6, 2.0 / 6, 3.0 / 6}) {
		t.Fatalf("k >= len normalization = %v", got)
	}
	if got := topKNormalized([]float64{0, 0, 0}, 2); !reflect.DeepEqual(got, []float64{0, 0, 0}) {
		t.Fatalf("zero input = %v, want zeros", got)
	}
}

func TestTopKNormalizedTieBreaksByIndex(t *testing.T) {
	want := []float64{0.5, 0.5, 0, 0}
	for i := 0; i < 2; i++ {
		if got := topKNormalized([]float64{1, 1, 1, 1}, 2); !reflect.DeepEqual(got, want) {
			t.Fatalf("tie result = %v, want earliest indices %v", got, want)
		}
	}
}
