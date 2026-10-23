package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	From       string
	Encryption string
}

type Reminder struct {
	Name          string
	Provider      string
	ExpiresAt     time.Time
	DaysRemaining int
	RenewalPrice  string
	AutoRenew     bool
	RenewalURL    string
	PublicURL     string
}

func (c SMTPConfig) Configured() bool {
	return strings.TrimSpace(c.Host) != "" && c.Port > 0 && strings.TrimSpace(c.From) != ""
}

func SendReminderEmail(ctx context.Context, cfg SMTPConfig, recipient string, reminder Reminder) error {
	if !cfg.Configured() {
		return fmt.Errorf("SMTP 尚未配置完整")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("发件人地址无效")
	}
	to, err := mail.ParseAddress(recipient)
	if err != nil {
		return fmt.Errorf("收件人地址无效")
	}
	if cfg.Encryption != "starttls" && cfg.Encryption != "tls" && cfg.Encryption != "none" {
		return fmt.Errorf("SMTP 加密方式无效")
	}
	if cfg.Username != "" && cfg.Encryption == "none" {
		return fmt.Errorf("SMTP 使用账号密码认证时必须启用 TLS")
	}

	htmlBody, err := renderEmail(reminder)
	if err != nil {
		return fmt.Errorf("生成邮件内容失败")
	}
	plain := fmt.Sprintf("%s 将于 %s 到期，剩余 %d 天。续费金额：%s\n", reminder.Name,
		reminder.ExpiresAt.Format("2006-01-02"), reminder.DaysRemaining, reminder.RenewalPrice)
	if reminder.RenewalURL != "" {
		plain += "续费链接：" + reminder.RenewalURL + "\n"
	}
	if reminder.PublicURL != "" {
		plain += "打开 SubTrack：" + reminder.PublicURL + "\n"
	}
	var body bytes.Buffer
	body.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"subtrack-alternative\"\r\n\r\n")
	body.WriteString("--subtrack-alternative\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	plainWriter := quotedprintable.NewWriter(&body)
	_, _ = plainWriter.Write([]byte(strings.ReplaceAll(plain, "\n", "\r\n")))
	_ = plainWriter.Close()
	body.WriteString("\r\n--subtrack-alternative\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	htmlWriter := quotedprintable.NewWriter(&body)
	_, _ = htmlWriter.Write([]byte(htmlBody))
	_ = htmlWriter.Close()
	body.WriteString("\r\n--subtrack-alternative--\r\n")

	subject := fmt.Sprintf("SubTrack 到期提醒：%s", reminder.Name)
	message := "From: " + from.String() + "\r\n" +
		"To: " + to.String() + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" + body.String()

	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败")
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetDeadline(deadline)
	if cfg.Encryption == "tls" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP TLS 握手失败")
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP 握手失败")
	}
	defer client.Close()
	if cfg.Encryption == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 服务器不支持 STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("SMTP STARTTLS 握手失败")
		}
	}
	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败")
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP 发件人被拒绝")
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP 收件人被拒绝")
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP 无法开始发送邮件")
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("SMTP 写入邮件失败")
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("SMTP 发送邮件失败")
	}
	return nil
}

func renderEmail(reminder Reminder) (string, error) {
	tmpl, err := template.New("reminder").Parse(`<!doctype html><html lang="zh-CN"><body style="font-family:Arial,sans-serif;color:#24324a;line-height:1.7"><h2>订阅到期提醒</h2><p><strong>{{.Name}}</strong>{{if .Provider}}（{{.Provider}}）{{end}} 将于 <strong>{{.Expiry}}</strong> 到期。</p><p>剩余 <strong>{{.Days}} 天</strong>；续费金额：{{.Price}}。</p>{{if .AutoRenew}}<p>此资产启用了自动续费，请确认扣款和续费是否成功。</p>{{end}}{{if .RenewalURL}}<p><a href="{{.RenewalURL}}">打开续费页面</a></p>{{end}}{{if .PublicURL}}<p><a href="{{.PublicURL}}">打开 SubTrack</a></p>{{end}}<hr><small>此消息由 SubTrack 发送。</small></body></html>`)
	if err != nil {
		return "", err
	}
	data := struct {
		Name, Provider, Expiry, Price string
		Days                          int
		AutoRenew                     bool
		RenewalURL, PublicURL         string
	}{reminder.Name, reminder.Provider, reminder.ExpiresAt.Format("2006-01-02"), reminder.RenewalPrice,
		reminder.DaysRemaining, reminder.AutoRenew, reminder.RenewalURL, reminder.PublicURL}
	var result bytes.Buffer
	err = tmpl.Execute(&result, data)
	return result.String(), err
}
