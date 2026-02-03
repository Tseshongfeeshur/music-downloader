package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
)

// ReloadHandler handles /reload command for dynamic plugins.
type ReloadHandler struct {
	Reload      func(ctx context.Context) error
	RateLimiter *telegram.RateLimiter
	Logger      *logpkg.Logger
	AdminIDs    map[int64]struct{}
}

func (h *ReloadHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	message := update.Message

	if !isBotAdmin(h.AdminIDs, message.From.ID) {
		return
	}

	if h.Reload == nil {
		params := &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "❌ 重载未启用",
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	if err := h.Reload(ctx); err != nil {
		if h.Logger != nil {
			h.Logger.Error("reload failed", "error", err)
		}
		params := &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "❌ 重载失败: " + err.Error(),
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
		return
	}

	params := &bot.SendMessageParams{
		ChatID: message.Chat.ID,
		Text:   "✅ 动态插件已重载",
	}
	if h.RateLimiter != nil {
		_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
	} else {
		_, _ = b.SendMessage(ctx, params)
	}
}

func isBotAdmin(adminIDs map[int64]struct{}, userID int64) bool {
	if len(adminIDs) == 0 {
		return false
	}
	_, ok := adminIDs[userID]
	return ok
}
