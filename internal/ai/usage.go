package ai

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
)

// Usage is what one request consumed, as the provider reports it. In does
// not include Cached (input served from the provider's cache, cheaper).
type Usage struct {
	In, Cached, Out int64
	// The model that answered (a fallback or a dated version can differ
	// from the one asked for); "" when the provider does not say.
	Model string
}

// Record is one request for the usage statistics: no content, only who
// answered what kind of task and how many tokens it took.
type Record struct {
	Time            time.Time
	Provider, Model string
	Task            string
	Usage
}

// Tasks, for the statistics.
const (
	TaskSpam     = "spam"
	TaskSummary  = "summary"
	TaskReply    = "reply"
	TaskImprove  = "improve"
	TaskLabel    = "label"
	TaskSearch   = "search"
	TaskAnswer   = "answer"
	TaskDigest   = "digest"
	TaskTest     = "test"
	TaskAssist   = "assistant"
	taskFallback = TaskAssist
)

var usageHook atomic.Pointer[func(Record)]

// SetUsageHook sets the function that receives every request's usage
// (called on the request's goroutine).
func SetUsageHook(f func(Record)) { usageHook.Store(&f) }

func report(r Record) {
	if f := usageHook.Load(); f != nil && *f != nil {
		(*f)(r)
	}
}

type taskKey struct{}

// withTask tags the requests made with ctx for the statistics.
func withTask(ctx context.Context, task string) context.Context {
	return context.WithValue(ctx, taskKey{}, task)
}

func taskOf(ctx context.Context) string {
	if t, ok := ctx.Value(taskKey{}).(string); ok {
		return t
	}
	return taskFallback
}

// Prices per million tokens in USD from the providers' price lists
// (standard tier, prompts under the long-context limits), checked
// 2026-10-14. Keys are model names or prefixes; the longest match wins.
var builtinPrices = map[string]config.ModelPrice{
	// Anthropic – platform.claude.com/docs/en/about-claude/pricing
	"claude-fable-5-1":  {In: 10, Cached: 0.25, Out: 50},
	"claude-fable-5":    {In: 10, Cached: 1, Out: 50},
	"claude-opus-5-5":   {In: 4, Cached: 0.20, Out: 20},
	"claude-opus-5":     {In: 5, Cached: 0.50, Out: 25},
	"claude-opus-4":     {In: 5, Cached: 0.50, Out: 25},
	"claude-sonnet-5-5": {In: 2, Cached: 0.10, Out: 10},
	"claude-sonnet-5":   {In: 2, Cached: 0.20, Out: 10},
	"claude-sonnet-4":   {In: 3, Cached: 0.30, Out: 15},
	"claude-haiku-5-5":  {In: 0.10, Cached: 0.01, Out: 0.50},
	"claude-haiku-4":    {In: 1, Cached: 0.10, Out: 5},
	// OpenAI – developers.openai.com/api/docs/pricing
	"gpt-6-luna":   {In: 0.10, Cached: 0.01, Out: 0.50},
	"gpt-6-sol":    {In: 2, Cached: 0.20, Out: 10},
	"gpt-6.1-sol":  {In: 2, Cached: 0.10, Out: 10},
	"gpt-6-astra":  {In: 10, Cached: 1, Out: 50},
	"gpt-5.5":      {In: 5, Cached: 0.50, Out: 30},
	"gpt-5.4":      {In: 2.50, Cached: 0.25, Out: 15},
	"gpt-5.4-mini": {In: 0.75, Cached: 0.075, Out: 4.50},
	"gpt-5.4-nano": {In: 0.20, Cached: 0.02, Out: 1.25},
	"gpt-5-mini":   {In: 0.25, Cached: 0.025, Out: 2},
	"gpt-5-nano":   {In: 0.05, Cached: 0.005, Out: 0.40},
	// Google – ai.google.dev/gemini-api/docs/pricing
	"gemini-3.1-pro":        {In: 2, Cached: 0.20, Out: 12},
	"gemini-3.1-flash-lite": {In: 0.25, Cached: 0.025, Out: 1.50},
	"gemini-3-flash":        {In: 0.50, Cached: 0.05, Out: 3},
	"gemini-3.6-flash":      {In: 0.75, Cached: 0.075, Out: 3.75},
	"gemini-3.8-flash":      {In: 0.75, Cached: 0.075, Out: 3.75},
	// xAI – docs.x.ai/docs/models
	"grok-4.7": {In: 2, Cached: 0.20, Out: 6},
	"grok-4.6": {In: 2, Cached: 0.20, Out: 6},
	"grok-4.5": {In: 2, Cached: 0.20, Out: 6},
	"grok-4":   {In: 3, Cached: 0.30, Out: 15},
	"grok-3":   {In: 3, Cached: 0.30, Out: 15},
	"grok-2":   {In: 2, Cached: 0.20, Out: 10},
}

// PriceFor finds the price of a model: the user's own price first, then
// the built-in list. The local models cost nothing.
func PriceFor(provider, model string, own map[string]config.ModelPrice) (config.ModelPrice, bool) {
	if IsLocal(provider) {
		return config.ModelPrice{}, true
	}
	if p, ok := own[model]; ok {
		return p, true
	}
	best := ""
	for k := range builtinPrices {
		if strings.HasPrefix(model, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return config.ModelPrice{}, false
	}
	return builtinPrices[best], true
}

// Cost of a usage at a price, in USD.
func Cost(u Usage, p config.ModelPrice) float64 {
	return (float64(u.In)*p.In + float64(u.Cached)*p.Cached + float64(u.Out)*p.Out) / 1e6
}
