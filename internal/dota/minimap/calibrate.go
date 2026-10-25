package minimap

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"cmnet/internal/dota/worldmap"
)

// CalibrationAnchor is one landmark projected onto the minimap image.
type CalibrationAnchor struct {
	ID       string                 `json:"id"`
	Kind     string                 `json:"kind,omitempty"`
	World    worldmap.WorldPosition `json:"world"`
	Pixel    PixelPosition          `json:"pixel"`
	InBounds bool                   `json:"in_bounds"`
}

// CalibrationReport is JSON-friendly geometry vs minimap fit diagnostics.
type CalibrationReport struct {
	GeometryVersion string              `json:"geometry_version"`
	Transform       MinimapTransform    `json:"transform"`
	ImageWidth      int                 `json:"image_width"`
	ImageHeight     int                 `json:"image_height"`
	Anchors         []CalibrationAnchor `json:"anchors"`
	AllInBounds     bool                `json:"all_in_bounds"`
	// Verdict is A (fit), B (minor offset), or C (likely stale geometry vs client map).
	Verdict string `json:"verdict"`
	Notes   string `json:"notes,omitempty"`
}

// Calibrate projects worldmap.LoadCurrent landmarks through tf.
func Calibrate(tf MinimapTransform) (CalibrationReport, error) {
	geo, err := worldmap.LoadCurrent()
	if err != nil {
		return CalibrationReport{}, err
	}
	return CalibrateGeometry(geo, tf)
}

// CalibrateGeometry projects geo landmarks through tf and grades fit.
func CalibrateGeometry(geo *worldmap.Geometry, tf MinimapTransform) (CalibrationReport, error) {
	if geo == nil {
		return CalibrationReport{}, fmt.Errorf("minimap: nil geometry")
	}
	rep := CalibrationReport{
		GeometryVersion: geo.Version(),
		Transform:       tf,
		ImageWidth:      tf.ImageWidth,
		ImageHeight:     tf.ImageHeight,
		AllInBounds:     true,
	}

	ids := []worldmap.LandmarkID{
		worldmap.LandmarkRadiantAncient,
		worldmap.LandmarkDireAncient,
		worldmap.LandmarkRoshanPitA,
		worldmap.LandmarkRoshanPitB,
		worldmap.LandmarkRadiantTormentor,
		worldmap.LandmarkDireTormentor,
	}
	for _, id := range ids {
		pos, ok := geo.LandmarkPosition(id)
		if !ok {
			continue
		}
		pix := tf.WorldToPixel(pos)
		in := pixInBounds(pix, tf.ImageWidth, tf.ImageHeight)
		if !in {
			rep.AllInBounds = false
		}
		kind := ""
		switch id {
		case worldmap.LandmarkRadiantAncient, worldmap.LandmarkDireAncient:
			kind = worldmap.KindAncient
		case worldmap.LandmarkRoshanPitA, worldmap.LandmarkRoshanPitB:
			kind = worldmap.KindRoshanPit
		case worldmap.LandmarkRadiantTormentor, worldmap.LandmarkDireTormentor:
			kind = worldmap.KindTormentor
		}
		rep.Anchors = append(rep.Anchors, CalibrationAnchor{
			ID: string(id), Kind: kind, World: pos, Pixel: pix, InBounds: in,
		})
	}
	rep.Verdict, rep.Notes = gradeCalibration(rep)
	return rep, nil
}

// WriteCalibrationJSON writes report to path (or stdout when path empty — caller prints).
func WriteCalibrationJSON(path string, rep CalibrationReport) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func pixInBounds(p PixelPosition, w, h int) bool {
	if w <= 0 || h <= 0 {
		return p.X >= 0 && p.Y >= 0
	}
	return p.X >= 0 && p.Y >= 0 && p.X <= float64(w) && p.Y <= float64(h)
}

// gradeCalibration compares Ancient/Roshan pixel placement to expected minimap quadrants.
// Does NOT rename geometry; only reports A/B/C for TZ diagnosis.
func gradeCalibration(rep CalibrationReport) (verdict, notes string) {
	if !rep.AllInBounds || len(rep.Anchors) == 0 {
		return "C", "one or more landmarks project outside the minimap image"
	}
	byID := map[string]CalibrationAnchor{}
	for _, a := range rep.Anchors {
		byID[a.ID] = a
	}
	rad, okR := byID[string(worldmap.LandmarkRadiantAncient)]
	dire, okD := byID[string(worldmap.LandmarkDireAncient)]
	if !okR || !okD {
		return "C", "missing Ancient landmarks"
	}
	w := float64(rep.ImageWidth)
	h := float64(rep.ImageHeight)
	if w <= 0 {
		w = 1024
	}
	if h <= 0 {
		h = 1024
	}

	// Expected: Radiant SW (left+bottom), Dire NE (right+top) on overview image.
	radOK := rad.Pixel.X < w*0.45 && rad.Pixel.Y > h*0.55
	direOK := dire.Pixel.X > w*0.55 && dire.Pixel.Y < h*0.45
	if !radOK || !direOK {
		return "C", fmt.Sprintf("Ancient orientation mismatch (Radiant pixel=(%.0f,%.0f) Dire=(%.0f,%.0f)); geometry may not match client overview",
			rad.Pixel.X, rad.Pixel.Y, dire.Pixel.X, dire.Pixel.Y)
	}

	// Midpoint of ancients should sit near image center; Roshan pits near mid diagonal.
	midX := (rad.Pixel.X + dire.Pixel.X) / 2
	midY := (rad.Pixel.Y + dire.Pixel.Y) / 2
	cx, cy := w/2, h/2
	midDist := math.Hypot(midX-cx, midY-cy)
	midTol := 0.12 * math.Hypot(w, h) // ~12% of diagonal

	ra, okA := byID[string(worldmap.LandmarkRoshanPitA)]
	rb, okB := byID[string(worldmap.LandmarkRoshanPitB)]
	roshOK := true
	if okA && okB {
		// Pits should be on opposite sides of center (A NW-ish, B SE-ish in current geometry).
		roshOK = ra.Pixel.X < cx && ra.Pixel.Y < cy && rb.Pixel.X > cx && rb.Pixel.Y > cy ||
			ra.Pixel.X > cx && ra.Pixel.Y > cy && rb.Pixel.X < cx && rb.Pixel.Y < cy
	}

	if midDist <= midTol && roshOK {
		return "A", "Ancients and Roshan pits align with expected minimap orientation; 7.39 geometry looks current"
	}
	if midDist <= midTol*1.8 {
		return "B", fmt.Sprintf("orientation OK but midpoint offset=%.0fpx (tol=%.0f); minor drift vs overview", midDist, midTol)
	}
	return "C", fmt.Sprintf("large midpoint offset=%.0fpx or Roshan layout unexpected", midDist)
}
