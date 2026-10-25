package discordbot

import "math"

func float32FromBitsImpl(u uint32) float32 {
	return math.Float32frombits(u)
}
