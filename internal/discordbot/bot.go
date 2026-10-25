package discordbot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Bot is the Discord remote-control process.
type Bot struct {
	session *discordgo.Session
	live    *LiveConfig
	advisor *AdvisorClient
	voice   *Voice
	speaker *Speaker
	cache   *AudioCache
}

// Run starts the bot until ctx is cancelled.
func Run(ctx context.Context, cfgPath string) error {
	live, err := NewLiveConfig(cfgPath)
	if err != nil {
		return err
	}
	cfg := live.Get()
	cache, err := NewAudioCache(cfg.NTTS.CacheDir)
	if err != nil {
		return err
	}
	voice := NewVoice()
	speaker := NewSpeaker(live, cache, voice)
	adv := NewAdvisorClient(cfg.AdvisorURL, cfg.AdvisorToken)

	dg, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return err
	}
	b := &Bot{
		session: dg,
		live:    live,
		advisor: adv,
		voice:   voice,
		speaker: speaker,
		cache:   cache,
	}
	dg.AddHandler(b.onReady)
	dg.AddHandler(b.onInteraction)
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	if err := dg.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	defer dg.Close()
	defer voice.Leave()

	go b.watchConfig(ctx)
	go b.watchAlerts(ctx)

	log.Printf("discordbot: connected; guild=%s", cfg.GuildID)
	<-ctx.Done()
	return nil
}

func (b *Bot) watchConfig(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := b.live.Poll()
			if err != nil {
				log.Printf("discordbot: config reload: %v", err)
				continue
			}
			if changed {
				b.speaker.OnConfigChange()
			}
		}
	}
}

func (b *Bot) watchAlerts(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := b.advisor.Events(ctx, func(st *State) {
			b.speaker.HandleState(st)
		})
		if ctx.Err() != nil {
			return
		}
		log.Printf("discordbot: events stream ended: %v; retry in 3s", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	cfg := b.live.Get()
	if err := SyncGuildCommands(s, r.User.ID, cfg.GuildID); err != nil {
		log.Printf("discordbot: sync slash commands: %v", err)
	}
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	cfg := b.live.Get()
	if i.GuildID != cfg.GuildID {
		return
	}
	if !b.hasRole(i, cfg.RoleID) {
		b.reply(s, i, "Нет роли для remote-control.", true)
		return
	}
	data := i.ApplicationCommandData()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch data.Name {
	case "scout":
		players := optionString(data, "players")
		st, err := b.advisor.Scout(ctx, players)
		b.replyResult(s, i, st, err)
	case "scout-clear":
		st, err := b.advisor.ScoutClear(ctx, false)
		b.replyResult(s, i, st, err)
	case "side":
		st, err := b.advisor.SetSide(ctx, optionString(data, "team"))
		b.replyResult(s, i, st, err)
	case "lineup":
		side := optionString(data, "side")
		pos := int(optionInt(data, "position"))
		acc := optionInt(data, "account")
		hero := optionInt(data, "hero")
		clear := optionBool(data, "clear")
		st, err := b.advisor.Lineup(ctx, side, pos, acc, hero, clear)
		b.replyResult(s, i, st, err)
	case "voice":
		b.handleVoice(s, i, optionString(data, "action"))
	case "status":
		st, err := b.advisor.GetState(ctx)
		if err != nil {
			b.reply(s, i, err.Error(), true)
			return
		}
		b.reply(s, i, formatStatus(st), true)
	case "tts-reload":
		if _, err := b.live.Poll(); err != nil {
			b.reply(s, i, "reload: "+err.Error(), true)
			return
		}
		b.speaker.OnConfigChange()
		b.reply(s, i, "Конфиг перечитан, кэш озвучки очищен.", true)
	default:
		b.reply(s, i, "unknown command", true)
	}
}

func (b *Bot) handleVoice(s *discordgo.Session, i *discordgo.InteractionCreate, action string) {
	cfg := b.live.Get()
	switch action {
	case "leave":
		b.voice.Leave()
		b.reply(s, i, "Вышел из войса.", true)
	case "join":
		vs := b.findVoiceState(s, cfg.GuildID, i.Member.User.ID)
		if vs == nil || vs.ChannelID == "" {
			b.reply(s, i, "Сначала зайди в голосовой канал.", true)
			return
		}
		if err := b.voice.Join(s, cfg.GuildID, vs.ChannelID); err != nil {
			b.reply(s, i, "Не удалось зайти в войс: "+err.Error(), true)
			return
		}
		b.reply(s, i, "В войсе. Алерты будут озвучиваться.", true)
	default:
		b.reply(s, i, "action: join|leave", true)
	}
}

func (b *Bot) findVoiceState(s *discordgo.Session, guildID, userID string) *discordgo.VoiceState {
	g, err := s.State.Guild(guildID)
	if err != nil || g == nil {
		return nil
	}
	for _, vs := range g.VoiceStates {
		if vs.UserID == userID {
			return vs
		}
	}
	return nil
}

func (b *Bot) hasRole(i *discordgo.InteractionCreate, roleID string) bool {
	if i.Member == nil {
		return false
	}
	for _, r := range i.Member.Roles {
		if r == roleID {
			return true
		}
	}
	return false
}

func (b *Bot) replyResult(s *discordgo.Session, i *discordgo.InteractionCreate, st *State, err error) {
	if err != nil {
		b.reply(s, i, err.Error(), true)
		return
	}
	msg := "ok"
	if st != nil && st.Status != "" {
		msg = st.Status
	}
	if st != nil && st.Meta.OurTeam != "" {
		msg += "\nсторона: " + st.Meta.OurTeam
	}
	b.reply(s, i, msg, true)
}

func (b *Bot) reply(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: flags},
	})
}

func formatStatus(st *State) string {
	var b strings.Builder
	b.WriteString("сторона: ")
	b.WriteString(st.Meta.OurTeam)
	if st.Status != "" {
		b.WriteString("\n")
		b.WriteString(st.Status)
	}
	for _, a := range st.Alerts {
		if !a.Due {
			continue
		}
		b.WriteString(fmt.Sprintf("\n⚠ %s через %ds", a.Label, a.NextIn))
	}
	return b.String()
}

func optionString(data discordgo.ApplicationCommandInteractionData, name string) string {
	for _, o := range data.Options {
		if o.Name == name {
			return o.StringValue()
		}
	}
	return ""
}

func optionInt(data discordgo.ApplicationCommandInteractionData, name string) int64 {
	for _, o := range data.Options {
		if o.Name == name {
			return o.IntValue()
		}
	}
	return 0
}

func optionBool(data discordgo.ApplicationCommandInteractionData, name string) bool {
	for _, o := range data.Options {
		if o.Name == name {
			return o.BoolValue()
		}
	}
	return false
}

