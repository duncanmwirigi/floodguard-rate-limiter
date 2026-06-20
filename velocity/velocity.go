// Package velocity detects suspicious request patterns using a composable rule engine.
//
// Built-in rules include [RateOverWindow] and [MinInterval]; register custom rules
// with [Engine.AddRule].
package velocity

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrKeyRequired is returned when Check is called with an empty key.
var ErrKeyRequired = errors.New("velocity: key is required")

// ErrInvalidRule is returned when a rule is misconfigured.
var ErrInvalidRule = errors.New("velocity: invalid rule configuration")

// Store records action timestamps for velocity rules.
type Store interface {
	Record(ctx context.Context, key string, at time.Time) error
	Count(ctx context.Context, key string, window time.Duration) (int, error)
	Last(ctx context.Context, key string) (at time.Time, found bool, err error)
}

// Rule flags suspicious activity for a key using shared store state.
type Rule interface {
	Check(ctx context.Context, key string, store Store) (flagged bool, reason string, err error)
}

// Engine evaluates registered rules and records successful checks.
type Engine struct {
	store Store
	rules []Rule
}

// Config configures the velocity engine.
type Config struct {
	// Store persists action timestamps. Defaults to an in-memory store.
	Store Store
	// Rules are evaluated in order. Defaults to 20 actions per minute.
	Rules []Rule
}

// New creates an Engine. With no rules, a default [RateOverWindow] of 20/minute is used.
func New(cfg Config) *Engine {
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}

	rules := cfg.Rules
	if len(rules) == 0 {
		rules = []Rule{RateOverWindow{N: 20, Window: time.Minute}}
	}

	return &Engine{store: store, rules: rules}
}

// AddRule appends a custom rule to the engine.
func (e *Engine) AddRule(rule Rule) {
	e.rules = append(e.rules, rule)
}

// Check runs all rules against key. On success, records the action timestamp once.
func (e *Engine) Check(ctx context.Context, key string) (allowed bool, reason string, err error) {
	if key == "" {
		return false, "", ErrKeyRequired
	}

	for _, rule := range e.rules {
		flagged, why, err := rule.Check(ctx, key, e.store)
		if err != nil {
			return false, "", fmt.Errorf("velocity: rule check: %w", err)
		}
		if flagged {
			return false, why, nil
		}
	}

	if err := e.store.Record(ctx, key, time.Now()); err != nil {
		return false, "", fmt.Errorf("velocity: record: %w", err)
	}
	return true, "", nil
}

// StoreFor exposes the underlying store for custom rule implementations.
func (e *Engine) StoreFor() Store {
	return e.store
}

// Rules returns a copy of registered rules.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// Detector is an alias for [Engine] kept for backward compatibility.
type Detector = Engine
