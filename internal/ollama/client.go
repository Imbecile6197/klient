package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the Ollama HTTP API.
type Client struct {
	base string
	http *http.Client
}

func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return apiError(res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func apiError(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return fmt.Errorf("Ollama: %s", e.Error)
	}
	return fmt.Errorf("Ollama: %s", res.Status)
}

func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var v struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

// Model is a downloaded model.
type Model struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Digest   string    `json:"digest"`
	Modified time.Time `json:"modified_at"`
}

func (c *Client) List(ctx context.Context) ([]Model, error) {
	var res struct {
		Models []Model `json:"models"`
	}
	err := c.do(ctx, http.MethodGet, "/api/tags", nil, &res)
	return res.Models, err
}

func (c *Client) Delete(ctx context.Context, model string) error {
	return c.do(ctx, http.MethodDelete, "/api/delete", map[string]string{"model": model}, nil)
}

// Pull downloads (or updates) a model; progress gets bytes done of total
// for the current layer and a status text.
func (c *Client) Pull(ctx context.Context, model string, progress func(status string, done, total int64)) error {
	if IsCloud(model) {
		return errors.New("cloudové modely Ollamy běží mimo počítač – nepoužívají se")
	}
	b, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/pull", bytes.NewReader(b))
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return apiError(res)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var p struct {
			Status    string `json:"status"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &p) != nil {
			continue
		}
		if p.Error != "" {
			return fmt.Errorf("Ollama: %s", p.Error)
		}
		if progress != nil {
			progress(p.Status, p.Completed, p.Total)
		}
	}
	return sc.Err()
}

// IsCloud reports model tags that Ollama runs on its servers.
func IsCloud(model string) bool {
	return strings.Contains(strings.ToLower(model), "cloud")
}

// ChatRequest is one single-turn chat completion.
type ChatRequest struct {
	Model  string
	System string
	User   string
	Schema map[string]any // JSON schema for structured output (nil = text)
	NumCtx int
	// KeepAlive is how long the model stays loaded afterwards ("30m");
	// "" keeps the server default.
	KeepAlive string
}

type Stats struct {
	PromptTokens, OutputTokens int
	Duration                   time.Duration
}

func (c *Client) Chat(ctx context.Context, r ChatRequest) (string, Stats, error) {
	if IsCloud(r.Model) {
		return "", Stats{}, errors.New("cloudové modely Ollamy se nepoužívají")
	}
	body := map[string]any{
		"model": r.Model,
		"messages": []map[string]string{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
		"stream": false,
		"think":  false,
		"options": map[string]any{
			"num_ctx":     r.NumCtx,
			"temperature": 0.2,
		},
	}
	if r.Schema != nil {
		body["format"] = r.Schema
	}
	if r.KeepAlive != "" {
		body["keep_alive"] = r.KeepAlive
	}
	var res struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int   `json:"prompt_eval_count"`
		EvalCount       int   `json:"eval_count"`
		TotalDuration   int64 `json:"total_duration"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/chat", body, &res); err != nil {
		return "", Stats{}, err
	}
	return res.Message.Content, Stats{res.PromptEvalCount, res.EvalCount, time.Duration(res.TotalDuration)}, nil
}
