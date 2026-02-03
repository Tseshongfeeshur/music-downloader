package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// RecognizeHandler handles voice recognition.
type RecognizeHandler struct {
	CacheDir         string
	Music            *MusicHandler
	RateLimiter      *telegram.RateLimiter
	RecognizeService recognize.Service
	Logger           *logpkg.Logger
}

func (h *RecognizeHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message
	chatID := message.Chat.ID
	replyID := message.MessageID

	if message.ReplyToMessage == nil || message.ReplyToMessage.Voice == nil {
		sendText(ctx, b, chatID, replyID, "请回复一条语音留言")
		return
	}
	replyID = message.ReplyToMessage.MessageID

	if h.CacheDir == "" {
		h.CacheDir = "./cache"
	}
	ensureDir(h.CacheDir)

	fileInfo, err := b.GetFile(ctx, &telego.GetFileParams{FileID: message.ReplyToMessage.Voice.FileID})
	if err != nil || fileInfo == nil || fileInfo.FilePath == "" {
		sendText(ctx, b, chatID, replyID, "获取语音失败，请稍后重试")
		return
	}
	if fileInfo.FileSize > 20*1024*1024 {
		sendText(ctx, b, chatID, replyID, "语音过大，无法识别")
		return
	}
	fileURL := b.FileDownloadURL(fileInfo.FilePath)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "下载语音失败，请稍后重试")
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "下载语音失败，请稍后重试")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		sendText(ctx, b, chatID, replyID, "下载语音失败，请稍后重试")
		return
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "读取语音失败，请稍后重试")
		return
	}

	mp3Data, err := convertToMP3(ctx, audioData, h.CacheDir)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("audio conversion failed", "error", err)
		}
		sendText(ctx, b, chatID, replyID, "音频格式转换失败，请稍后重试")
		return
	}

	if h.RecognizeService == nil {
		sendText(ctx, b, chatID, replyID, "识别服务未启动，请联系管理员")
		return
	}

	result, err := h.RecognizeService.Recognize(ctx, mp3Data)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Error("recognition service error", "error", err, "audio_size", len(mp3Data))
		}
		sendText(ctx, b, chatID, replyID, "识别失败，请稍后重试")
		return
	}

	if result == nil || result.TrackID == "" || result.Platform == "" {
		if h.Logger != nil {
			h.Logger.Info("recognition returned no results")
		}
		sendText(ctx, b, chatID, replyID, "识别失败，可能是录音时间太短")
		return
	}

	if h.Logger != nil {
		h.Logger.Debug("recognition result", "platform", result.Platform, "track_id", result.TrackID)
	}

	if result.URL != "" {
		params := &telego.SendMessageParams{
			ChatID:          telego.ChatID{ID: chatID},
			Text:            result.URL,
			ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
		}
		if h.RateLimiter != nil {
			_, _ = telegram.SendMessageWithRetry(ctx, h.RateLimiter, b, params)
		} else {
			_, _ = b.SendMessage(ctx, params)
		}
	}

	if h.Music != nil {
		h.Music.dispatch(ctx, b, message.ReplyToMessage, result.Platform, result.TrackID, "")
	}
}

func sendText(ctx context.Context, b *telego.Bot, chatID int64, replyID int, text string) {
	if b == nil {
		return
	}
	params := &telego.SendMessageParams{
		ChatID:          telego.ChatID{ID: chatID},
		Text:            text,
		ReplyParameters: &telego.ReplyParameters{MessageID: replyID},
	}
	_, _ = b.SendMessage(ctx, params)
}

func convertToMP3(ctx context.Context, audioData []byte, cacheDir string) ([]byte, error) {
	if cacheDir == "" {
		cacheDir = "./cache"
	}

	tmpFile := filepath.Join(cacheDir, fmt.Sprintf("recognize-%d.ogg", time.Now().UnixNano()))
	mp3File := tmpFile + ".mp3"

	defer os.Remove(tmpFile)
	defer os.Remove(mp3File)

	if err := os.WriteFile(tmpFile, audioData, 0644); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}

	ffmpegCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ffmpegCtx, "ffmpeg", "-i", tmpFile, "-f", "mp3", "-acodec", "libmp3lame", "-ar", "48000", mp3File)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}

	mp3Data, err := os.ReadFile(mp3File)
	if err != nil {
		return nil, fmt.Errorf("read converted file: %w", err)
	}

	return mp3Data, nil
}
