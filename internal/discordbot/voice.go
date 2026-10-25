package discordbot

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Voice plays cached/fetched audio into a Discord voice channel.
type Voice struct {
	mu    sync.Mutex
	vc    *discordgo.VoiceConnection
	queue chan string // file paths
	stop  chan struct{}
}

func NewVoice() *Voice {
	v := &Voice{
		queue: make(chan string, 16),
		stop:  make(chan struct{}),
	}
	go v.loop()
	return v
}

// Join connects to channelID on guildID.
// Uses the DAVE-capable discordgo fork; ChannelVoiceJoin blocks until Ready or error.
func (v *Voice) Join(s *discordgo.Session, guildID, channelID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.vc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = v.vc.Disconnect(ctx)
		cancel()
		v.vc = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// mute=false, deaf=true: we only speak alerts, no need to receive.
	vc, err := s.ChannelVoiceJoin(ctx, guildID, channelID, false, true)
	if err != nil {
		return fmt.Errorf("voice join: %w", err)
	}
	if vc.Status != discordgo.VoiceConnectionStatusReady {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		_ = vc.Disconnect(ctx2)
		cancel2()
		return fmt.Errorf("voice join: status=%v err=%v", vc.Status, vc.Err)
	}
	v.vc = vc
	go v.watchDead(vc)
	return nil
}

func (v *Voice) watchDead(vc *discordgo.VoiceConnection) {
	<-vc.Dead
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.vc == vc {
		if vc.Err != nil {
			log.Printf("discordbot: voice connection died: %v", vc.Err)
		} else {
			log.Printf("discordbot: voice connection closed")
		}
		v.vc = nil
	}
}

// Leave disconnects from voice.
func (v *Voice) Leave() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.vc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = v.vc.Disconnect(ctx)
		cancel()
		v.vc = nil
	}
}

// Connected reports whether we are in a voice channel and ready to send.
func (v *Voice) Connected() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vc != nil && v.vc.Status == discordgo.VoiceConnectionStatusReady
}

// Enqueue plays path after previous clips (non-blocking; drops if full).
func (v *Voice) Enqueue(path string) {
	select {
	case v.queue <- path:
	default:
		log.Printf("discordbot: voice queue full, drop %s", path)
	}
}

func (v *Voice) loop() {
	for {
		select {
		case <-v.stop:
			return
		case path := <-v.queue:
			if err := v.playFile(path); err != nil {
				log.Printf("discordbot: play %s: %v", path, err)
			}
		}
	}
}

func (v *Voice) playFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	packets, err := OpusPacketsFromOgg(data)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	v.mu.Lock()
	vc := v.vc
	v.mu.Unlock()
	if vc == nil || vc.Status != discordgo.VoiceConnectionStatusReady {
		return fmt.Errorf("not in voice")
	}
	_ = vc.Speaking(true)
	defer vc.Speaking(false)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for _, pkt := range packets {
		if vc.Status != discordgo.VoiceConnectionStatusReady {
			return fmt.Errorf("voice died mid-play: %v", vc.Err)
		}
		<-ticker.C
		select {
		case vc.OpusSend <- pkt:
		case <-vc.Dead:
			return fmt.Errorf("voice died mid-play: %v", vc.Err)
		}
	}
	return nil
}
