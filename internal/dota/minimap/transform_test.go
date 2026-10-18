package minimap

import (
	"math"
	"strings"
	"testing"

	"cmnet/internal/dota/worldmap"
)

func currentTransform() MinimapTransform {
	return MinimapTransform{
		PosX: -9472, PosY: 9472, Scale: 18.5,
		ImageWidth: 1024, ImageHeight: 1024,
	}
}

func TestParseOverviewTXT(t *testing.T) {
	const sample = `dota
{
	material materials/overviews/dota.vmat	
	simple_material materials/overviews/dota_minimal.vmat
	pos_x -9472
	pos_y 9472
	scale 18.500
}
`
	x, y, s, err := ParseOverviewTXT(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if x != -9472 || y != 9472 || s != 18.5 {
		t.Fatalf("got pos=(%v,%v) scale=%v", x, y, s)
	}
}

func TestWorldToPixelYInversion(t *testing.T) {
	tf := currentTransform()
	south := tf.WorldToPixel(worldmap.WorldPosition{X: 0, Y: -5000})
	north := tf.WorldToPixel(worldmap.WorldPosition{X: 0, Y: 5000})
	if !(north.Y < south.Y) {
		t.Fatalf("Y not inverted: north.Y=%v south.Y=%v", north.Y, south.Y)
	}
}

func TestRadiantSWDireNE(t *testing.T) {
	tf := currentTransform()
	rad := tf.WorldToPixel(worldmap.WorldPosition{X: -5920, Y: -5352})
	dire := tf.WorldToPixel(worldmap.WorldPosition{X: 5528, Y: 5000})
	if !(rad.X < float64(tf.ImageWidth)/2 && rad.Y > float64(tf.ImageHeight)/2) {
		t.Fatalf("Radiant not SW: %+v", rad)
	}
	if !(dire.X > float64(tf.ImageWidth)/2 && dire.Y < float64(tf.ImageHeight)/2) {
		t.Fatalf("Dire not NE: %+v", dire)
	}
}

func TestEdgeCoords(t *testing.T) {
	tf := currentTransform()
	ul := tf.WorldToPixel(worldmap.WorldPosition{X: tf.PosX, Y: tf.PosY})
	if math.Abs(ul.X) > 1e-9 || math.Abs(ul.Y) > 1e-9 {
		t.Fatalf("upper-left world must map to (0,0), got %+v", ul)
	}
	brWorld := worldmap.WorldPosition{
		X: tf.PosX + float64(tf.ImageWidth)*tf.Scale,
		Y: tf.PosY - float64(tf.ImageHeight)*tf.Scale,
	}
	br := tf.WorldToPixel(brWorld)
	if math.Abs(br.X-float64(tf.ImageWidth)) > 1e-6 || math.Abs(br.Y-float64(tf.ImageHeight)) > 1e-6 {
		t.Fatalf("bottom-right expected (%d,%d) got %+v", tf.ImageWidth, tf.ImageHeight, br)
	}
}

func TestKnownLandmarksApproxPixels(t *testing.T) {
	tf := currentTransform()
	cases := []struct {
		name   string
		w      worldmap.WorldPosition
		px, py float64
		tol    float64
	}{
		{"RADIANT_ANCIENT", worldmap.WorldPosition{X: -5920, Y: -5352}, 192.0, 801.3, 1.0},
		{"DIRE_ANCIENT", worldmap.WorldPosition{X: 5528, Y: 5000}, 810.8, 241.7, 1.0},
		{"ROSHAN_PIT_A", worldmap.WorldPosition{X: -2600, Y: 2700}, 371.5, 366.1, 1.0},
		{"ROSHAN_PIT_B", worldmap.WorldPosition{X: 2859, Y: -2764}, 666.5, 661.4, 1.0},
	}
	for _, tc := range cases {
		got := tf.WorldToPixel(tc.w)
		if math.Abs(got.X-tc.px) > tc.tol || math.Abs(got.Y-tc.py) > tc.tol {
			t.Errorf("%s: got (%.2f,%.2f) want ~(%.1f,%.1f)", tc.name, got.X, got.Y, tc.px, tc.py)
		}
	}
}

func TestWorldToNormalized(t *testing.T) {
	tf := currentTransform()
	nx, ny := tf.WorldToNormalized(worldmap.WorldPosition{X: tf.PosX, Y: tf.PosY})
	if nx != 0 || ny != 0 {
		t.Fatalf("got %v,%v", nx, ny)
	}
}

func TestCalibrateInBounds(t *testing.T) {
	tf := currentTransform()
	rep, err := Calibrate(tf)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.AllInBounds {
		t.Fatalf("expected all anchors in bounds: %+v", rep.Anchors)
	}
	if rep.Verdict != "A" && rep.Verdict != "B" {
		t.Fatalf("unexpected verdict %s notes=%s", rep.Verdict, rep.Notes)
	}
	t.Logf("verdict=%s notes=%s", rep.Verdict, rep.Notes)
	for _, a := range rep.Anchors {
		t.Logf("%s pixel=(%.1f,%.1f) world=(%.0f,%.0f)", a.ID, a.Pixel.X, a.Pixel.Y, a.World.X, a.World.Y)
	}
}
