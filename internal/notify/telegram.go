package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type TelegramBot struct {
	token  string
	client *http.Client
}

type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func NewTelegramBot(token string) *TelegramBot {
	return &TelegramBot{token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

func (b *TelegramBot) Configured() bool { return strings.TrimSpace(b.token) != "" }

func (b *TelegramBot) SendReminder(ctx context.Context, chatID string, reminder Reminder) error {
	lines := []string{
		"<b>订阅到期提醒</b>",
		"<b>" + html.EscapeString(reminder.Name) + "</b>",
	}
	if reminder.Provider != "" {
		lines = append(lines, "服务商："+html.EscapeString(reminder.Provider))
	}
	lines = append(lines, fmt.Sprintf("到期日期：%s", reminder.ExpiresAt.Format("2006-01-02")))
	if reminder.DaysRemaining < 0 {
		lines = append(lines, fmt.Sprintf("已过期 %d 天", -reminder.DaysRemaining))
	} else if reminder.DaysRemaining == 0 {
		lines = append(lines, "今天到期")
	} else {
		lines = append(lines, fmt.Sprintf("剩余 %d 天", reminder.DaysRemaining))
	}
	lines = append(lines, "续费金额："+html.EscapeString(reminder.RenewalPrice))
	if reminder.AutoRenew {
		lines = append(lines, "已启用自动续费，请确认扣款和续费是否成功。")
	}
	buttons := make([][]map[string]string, 0, 2)
	if reminder.RenewalURL != "" {
		buttons = append(buttons, []map[string]string{{"text": "续费", "url": reminder.RenewalURL}})
	}
	if reminder.PublicURL != "" {
		buttons = append(buttons, []map[string]string{{"text": "打开 SubTrack", "url": reminder.PublicURL}})
	}
	payload := map[string]any{"chat_id": chatID, "text": strings.Join(lines, "\n"), "parse_mode": "HTML", "disable_web_page_preview": true}
	if len(buttons) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": buttons}
	}
	return b.call(ctx, "sendMessage", payload, nil)
}

func (b *TelegramBot) SendTest(ctx context.Context, chatID string) error {
	return b.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID,
		"text":    "SubTrack 通知测试成功。此 Telegram 账号已连接。",
	}, nil)
}

func (b *TelegramBot) GetUpdates(ctx context.Context, offset int64) ([]TelegramUpdate, error) {
	query := url.Values{}
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("timeout", "20")
	query.Set("allowed_updates", `["message"]`)
	endpoint := "https://api.telegram.org/bot" + b.token + "/getUpdates?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Telegram 请求失败")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("连接 Telegram 失败")
	}
	defer response.Body.Close()
	var result struct {
		OK          bool             `json:"ok"`
		Description string           `json:"description"`
		Result      []TelegramUpdate `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || response.StatusCode/100 != 2 || !result.OK {
		if result.Description == "" {
			return nil, fmt.Errorf("Telegram getUpdates 请求失败")
		}
		return nil, fmt.Errorf("Telegram getUpdates 请求失败：%s", result.Description)
	}
	return result.Result, nil
}

func (b *TelegramBot) ReplyBinding(ctx context.Context, chatID string, success bool) error {
	message := "绑定链接无效或已过期。请回到 SubTrack 生成新的绑定链接。"
	if success {
		message = "Telegram 已绑定到 SubTrack。你可以返回网页发送测试消息。"
	}
	return b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": message}, nil)
}

func (b *TelegramBot) call(ctx context.Context, method string, payload any, result any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码 Telegram 请求失败")
	}
	endpoint := "https://api.telegram.org/bot" + b.token + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("创建 Telegram 请求失败")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.client.Do(request)
	if err != nil {
		return fmt.Errorf("连接 Telegram 失败")
	}
	defer response.Body.Close()
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil || response.StatusCode/100 != 2 || !envelope.OK {
		if envelope.Description == "" {
			return fmt.Errorf("Telegram %s 请求失败", method)
		}
		return fmt.Errorf("Telegram %s 请求失败：%s", method, envelope.Description)
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("读取 Telegram %s 响应失败", method)
		}
	}
	return nil
}

func (b *TelegramBot) GetMe(ctx context.Context) (string, error) {
	var user struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", map[string]any{}, &user); err != nil {
		return "", err
	}
	return user.Username, nil
}
