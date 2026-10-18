package minimap

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cmnet/internal/dota/worldmap"
)

// PixelPosition is an image-space coordinate (origin top-left, +Y down).
type PixelPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// MinimapTransform maps Dota world XY to overview image pixels.
// Valve overview: PosX/PosY are world coords of the UPPER-LEFT of the image;
// Scale is world units per pixel.
type MinimapTransform struct {
	PosX        float64 `json:"pos_x"`
	PosY        float64 `json:"pos_y"`
	Scale       float64 `json:"scale"`
	ImageWidth  int     `json:"width"`
	ImageHeight int     `json:"height"`
}

// WorldToPixel converts world coords to pixel coords.
//
//	pixelX = (worldX - PosX) / Scale
//	pixelY = (PosY - worldY) / Scale // Y inverted (world +Y north, image +Y down)
func (t MinimapTransform) WorldToPixel(p worldmap.WorldPosition) PixelPosition {
	if t.Scale == 0 {
		return PixelPosition{}
	}
	return PixelPosition{
		X: (p.X - t.PosX) / t.Scale,
		Y: (t.PosY - p.Y) / t.Scale,
	}
}

// WorldToNormalized returns pixel coords scaled to [0,1] by image size.
func (t MinimapTransform) WorldToNormalized(p worldmap.WorldPosition) (nx, ny float64) {
	pix := t.WorldToPixel(p)
	if t.ImageWidth > 0 {
		nx = pix.X / float64(t.ImageWidth)
	}
	if t.ImageHeight > 0 {
		ny = pix.Y / float64(t.ImageHeight)
	}
	return nx, ny
}

// ParseOverviewTXT reads a Valve overview KeyValues file (resource/overviews/dota.txt).
func ParseOverviewTXT(r io.Reader) (posX, posY, scale float64, err error) {
	var haveX, haveY, haveS bool
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		val := fields[1]
		switch key {
		case "pos_x":
			posX, err = strconv.ParseFloat(val, 64)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("minimap: parse pos_x: %w", err)
			}
			haveX = true
		case "pos_y":
			posY, err = strconv.ParseFloat(val, 64)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("minimap: parse pos_y: %w", err)
			}
			haveY = true
		case "scale":
			scale, err = strconv.ParseFloat(val, 64)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("minimap: parse scale: %w", err)
			}
			haveS = true
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, 0, err
	}
	if !haveX || !haveY || !haveS {
		return 0, 0, 0, fmt.Errorf("minimap: overview missing pos_x/pos_y/scale")
	}
	if scale == 0 {
		return 0, 0, 0, fmt.Errorf("minimap: scale must be non-zero")
	}
	return posX, posY, scale, nil
}
