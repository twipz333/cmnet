package discordbot

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

func floatPtr(v float64) *float64 { return &v }

// SlashCommands is the canonical guild slash-command set.
func SlashCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "scout", Description: "Скаут врагов (URL/id через пробел или запятую)",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "players", Description: "Профили врагов", Required: true},
			}},
		{Name: "scout-clear", Description: "Очистить список скаута врагов"},
		{Name: "side", Description: "Сменить нашу сторону",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "team", Description: "radiant или dire", Required: true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "radiant", Value: "radiant"},
						{Name: "dire", Value: "dire"},
					}},
			}},
		{Name: "lineup", Description: "Изменить слот лайнапа",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "side", Description: "our или enemy", Required: true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "our", Value: "our"},
						{Name: "enemy", Value: "enemy"},
					}},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "position", Description: "1..5", Required: true, MinValue: floatPtr(1), MaxValue: 5},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "account", Description: "account id игрока", Required: false},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "hero", Description: "hero id", Required: false},
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "clear", Description: "снять игрока со слота", Required: false},
			}},
		{Name: "voice", Description: "Войс-канал для озвучки алертов",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: "join или leave", Required: true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "join", Value: "join"},
						{Name: "leave", Value: "leave"},
					}},
			}},
		{Name: "status", Description: "Краткий статус advisor"},
		{Name: "tts-reload", Description: "Перечитать конфиг и сбросить кэш озвучки"},
	}
}

// SyncGuildCommands clears leftover global commands and bulk-overwrites the
// guild command list (atomic replace — no stale leftovers).
func SyncGuildCommands(s *discordgo.Session, appID, guildID string) error {
	if appID == "" {
		return fmt.Errorf("empty application id")
	}
	if guildID == "" {
		return fmt.Errorf("empty guild id")
	}
	if _, err := s.ApplicationCommandBulkOverwrite(appID, "", []*discordgo.ApplicationCommand{}); err != nil {
		return fmt.Errorf("clear global commands: %w", err)
	}
	cmds := SlashCommands()
	out, err := s.ApplicationCommandBulkOverwrite(appID, guildID, cmds)
	if err != nil {
		return fmt.Errorf("guild bulk overwrite: %w", err)
	}
	names := make([]string, 0, len(out))
	for _, c := range out {
		names = append(names, c.Name)
	}
	log.Printf("discordbot: slash commands synced on guild %s: %v", guildID, names)
	return nil
}

// SyncCommandsOnce opens Discord with the bot token from cfgPath, syncs
// slash commands, and closes. Does not start the interactive bot.
func SyncCommandsOnce(cfgPath string) error {
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	dg, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return err
	}
	if err := dg.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	defer dg.Close()
	appID := ""
	if dg.State != nil && dg.State.User != nil {
		appID = dg.State.User.ID
	}
	if appID == "" {
		u, err := dg.User("@me")
		if err != nil {
			return fmt.Errorf("resolve bot user: %w", err)
		}
		appID = u.ID
	}
	return SyncGuildCommands(dg, appID, cfg.GuildID)
}
