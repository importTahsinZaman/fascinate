package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ResendClient struct {
	baseURL    string
	apiKey     string
	from       string
	httpClient *http.Client
}

func NewResendClient(baseURL, apiKey, from string) *ResendClient {
	return &ResendClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		from:    strings.TrimSpace(from),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *ResendClient) Enabled() bool {
	return c != nil && c.apiKey != "" && c.from != "" && c.baseURL != ""
}

func (c *ResendClient) SendSignupCode(ctx context.Context, to, code string) error {
	if !c.Enabled() {
		return fmt.Errorf("email delivery is not configured")
	}

	body := map[string]any{
		"from":    c.from,
		"to":      []string{strings.TrimSpace(to)},
		"subject": "Your Fascinate verification code",
		"text":    fmt.Sprintf("Your Fascinate verification code is %s. It expires soon.", strings.TrimSpace(code)),
		"html":    fmt.Sprintf("<p>Your Fascinate verification code is <strong>%s</strong>.</p><p>It expires soon.</p>", strings.TrimSpace(code)),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend returned %s", resp.Status)
	}

	return nil
}
