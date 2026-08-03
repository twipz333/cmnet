package model

import "cmnet/internal/features"

// WinProb binds a trained binary model to a hero map for convenient inference.
//
// Calibration comes in two layers. Calib is the global Platt scaling fitted on
// full compositions. PhaseCalib, when present, holds one calibrator per number of
// picks on the board: a probability read after two picks is a fundamentally
// different quantity from one read on a full 5v5 — it should sit far closer to
// 0.5 — and a single calibrator fitted on finished drafts cannot express that.
type WinProb struct {
	Model      *BinaryLogReg
	HeroMap    *features.HeroMap
	Calib      *Platt
	PhaseCalib []Platt
}

// RawWinProb returns the UNcalibrated model output. The stacker consumes this: it
// fits its own intercept and slope, so feeding it a pre-calibrated probability
// would just make it undo a transformation it is about to redo.
func (w *WinProb) RawWinProb(radiantPicks, direPicks []int) float64 {
	x := w.HeroMap.EncodeWinProb(radiantPicks, direPicks)
	return w.Model.Predict(x)
}

// RadiantWinProb returns the (rounded, calibrated) probability that Radiant wins
// given the hero ids picked by each team. Rounding makes the score
// platform-stable (§6).
func (w *WinProb) RadiantWinProb(radiantPicks, direPicks []int) float64 {
	raw := w.RawWinProb(radiantPicks, direPicks)
	board := len(radiantPicks) + len(direPicks)
	if c := w.phaseCalibrator(board); c != nil {
		return RoundScore(c.Apply(raw))
	}
	if w.Calib != nil {
		raw = w.Calib.Apply(raw)
	}
	return RoundScore(raw)
}

// CalibrateAt applies the calibrator appropriate to a board size to an already
// computed raw probability. Callers that produce the raw value themselves (the
// rollout estimator) use this instead of RadiantWinProb.
func (w *WinProb) CalibrateAt(raw float64, board int) float64 {
	if c := w.phaseCalibrator(board); c != nil {
		return c.Apply(raw)
	}
	if w.Calib != nil {
		return w.Calib.Apply(raw)
	}
	return raw
}

// phaseCalibrator returns the calibrator for a board size, or nil when there is no
// phase-specific one (identity entries mean "no data at this board size").
func (w *WinProb) phaseCalibrator(board int) *Platt {
	if board < 0 || board >= len(w.PhaseCalib) {
		return nil
	}
	c := w.PhaseCalib[board]
	if c == (Platt{A: 1, B: 0}) {
		return nil
	}
	return &c
}
