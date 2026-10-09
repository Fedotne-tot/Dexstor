package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type request struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type response struct {
	Response string `json:"response"`
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) Generate(model, prompt string) (string, error) {
	body, err := json.Marshal(request{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		c.BaseURL+"/api/generate",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorBody, readErr := io.ReadAll(resp.Body)

		if readErr != nil {
			return "", fmt.Errorf(
				"ollama HTTP status: %s; failed to read response body: %v",
				resp.Status,
				readErr,
			)
		}

		return "", fmt.Errorf(
			"ollama HTTP status: %s; body: %s",
			resp.Status,
			string(errorBody),
		)
	}

	var decoded response

	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}

	return decoded.Response, nil
}
