package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var webhookHTTPClient = &http.Client{Timeout: 5 * time.Second}

const notifyCooldown = 15 * time.Minute

var (
	notifyMu       sync.Mutex
	lastNotifiedAt = map[string]time.Time{}
)

func notifyAlerts(store *Store, cfg ConfigDTO, alerts []ThreatAlert, now time.Time, ipTags *ipTagCache) {
	if store == nil {
		return
	}

	notifyMu.Lock()
	var toSend []ThreatAlert
	for _, a := range alerts {
		key := string(a.Kind) + ":" + a.IP
		if last, ok := lastNotifiedAt[key]; !ok || now.Sub(last) >= notifyCooldown {
			lastNotifiedAt[key] = now
			toSend = append(toSend, a)
		}
	}

	for key, last := range lastNotifiedAt {
		if now.Sub(last) > 2*notifyCooldown {
			delete(lastNotifiedAt, key)
		}
	}
	notifyMu.Unlock()

	if len(toSend) == 0 {
		return
	}

	webhooks, err := store.ListEnabledWebhooks()
	if err != nil {
		log.Printf("webhooks: list enabled webhooks failed: %v", err)
		return
	}
	for _, a := range toSend {
		alert := a
		alert.Label = resolveIPTag(ipTags, alert.IP)

		go func() {
			summary := ""
			if cfg.AIEnabled && cfg.AIBaseURL != "" && cfg.AIModel != "" {
				if s, err := summarizeAlertForNotify(cfg, alert, now); err != nil {
					log.Printf("webhooks: AI alert summary failed, falling back to raw fields only: %v", err)
				} else {
					summary = s
				}
			}
			for _, wh := range webhooks {
				wh := wh
				go func() {
					var sendErr error
					switch wh.Channel {
					case "wecom":
						sendErr = sendWeComAlert(wh.URL, alert, now, summary)
					case "dingtalk":
						sendErr = sendDingTalkAlert(wh.URL, wh.Secret, alert, now, summary)
					case "feishu":
						sendErr = sendFeishuAlert(wh.URL, alert, now, summary)
					default:
						log.Printf("webhooks: unknown channel %q (webhook id %d)", wh.Channel, wh.ID)
						return
					}
					if sendErr != nil {
						log.Printf("webhooks: %s send failed (webhook id %d): %v", wh.Channel, wh.ID, sendErr)
					}
				}()
			}
		}()
	}
}

func alertKindLabel(kind AlertKind) string {
	switch kind {
	case AlertKindDDoS:
		return "疑似DDoS/流量异常"
	case AlertKindVolume:
		return "单IP大流量"
	default:
		return "端口/主机扫描"
	}
}

func alertDetailLine(a ThreatAlert) string {
	if a.Kind == AlertKindVolume {
		return fmt.Sprintf("**流量总量**：%s", formatBytesCN(a.VolumeBytes))
	}
	label := "涉及目标数"
	if a.Kind == AlertKindDDoS {
		label = "涉及源IP数"
	}
	return fmt.Sprintf("**%s**：%d", label, a.DistinctPeers)
}

func formatBytesCN(n uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", v, units[i])
	}
	return fmt.Sprintf("%.2f%s", v, units[i])
}

func sendWeComAlert(webhookURL string, a ThreatAlert, ts time.Time, summary string) error {
	content := fmt.Sprintf(
		"## Netra 威胁感知告警\n**类型**：<font color=\"warning\">%s</font>\n**IP**：%s\n%s\n**时间**：%s",
		alertKindLabel(a.Kind), formatIPLabel(a.IP, a.Label), alertDetailLine(a), ts.Format("2006-01-02 15:04:05"),
	)
	if summary != "" {
		content += fmt.Sprintf("\n**AI解读**：%s", summary)
	}
	return postJSON(webhookURL, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	})
}

func sendFeishuAlert(webhookURL string, a ThreatAlert, ts time.Time, summary string) error {
	template := "orange"
	if a.Kind != AlertKindScan {
		template = "red"
	}
	text := fmt.Sprintf(
		"**类型**：%s\n**IP**：%s\n%s\n**时间**：%s",
		alertKindLabel(a.Kind), formatIPLabel(a.IP, a.Label), alertDetailLine(a), ts.Format("2006-01-02 15:04:05"),
	)
	if summary != "" {
		text += fmt.Sprintf("\n**AI解读**：%s", summary)
	}
	return postJSON(webhookURL, map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Netra 威胁感知告警"},
				"template": template,
			},
			"elements": []map[string]any{
				{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": text}},
			},
		},
	})
}

func sendDingTalkAlert(webhookURL, secret string, a ThreatAlert, ts time.Time, summary string) error {
	finalURL := webhookURL
	if secret != "" {
		timestamp := time.Now().UnixMilli()
		sign, err := dingTalkSign(timestamp, secret)
		if err != nil {
			return fmt.Errorf("sign: %w", err)
		}
		u, err := url.Parse(webhookURL)
		if err != nil {
			return fmt.Errorf("parse webhook url: %w", err)
		}
		q := u.Query()
		q.Set("timestamp", fmt.Sprintf("%d", timestamp))
		q.Set("sign", sign)
		u.RawQuery = q.Encode()
		finalURL = u.String()
	}
	text := fmt.Sprintf(
		"### Netra 威胁感知告警\n\n**类型**：%s\n\n**IP**：%s\n\n%s\n\n**时间**：%s",
		alertKindLabel(a.Kind), formatIPLabel(a.IP, a.Label), alertDetailLine(a), ts.Format("2006-01-02 15:04:05"),
	)
	if summary != "" {
		text += fmt.Sprintf("\n\n**AI解读**：%s", summary)
	}
	return postJSON(finalURL, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": "Netra 威胁感知告警",
			"text":  text,
		},
	})
}

func dingTalkSign(timestamp int64, secret string) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func postJSON(webhookURL string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
