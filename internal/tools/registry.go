package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Spec struct {
	Name, Version  string
	Timeout        time.Duration
	MaxOutputBytes int64
	InputSchema    json.RawMessage
}
type Handler[T any] func(context.Context, Input) (T, error)
type entry[T any] struct {
	spec Spec
	run  Handler[T]
}
type Registry[T any] struct{ entries map[string]entry[T] }

func NewRegistry[T any]() *Registry[T] { return &Registry[T]{entries: map[string]entry[T]{}} }
func (r *Registry[T]) Register(spec Spec, run Handler[T]) error {
	if spec.Name == "" || spec.Version == "" || spec.Timeout <= 0 || run == nil || !json.Valid(spec.InputSchema) {
		return errors.New("invalid_tool_spec")
	}
	if _, exists := r.entries[spec.Name]; exists {
		return errors.New("duplicate_tool")
	}
	r.entries[spec.Name] = entry[T]{spec, run}
	return nil
}
func (r *Registry[T]) Has(name string) bool {
	_, exists := r.entries[name]
	return exists
}
func (r *Registry[T]) Execute(ctx context.Context, input Input) (T, error) {
	var zero T
	tool, ok := r.entries[input.Tool]
	if !ok {
		return zero, &Error{Code: "unknown_tool", Correctable: true}
	}
	if err := input.Validate(); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, tool.spec.Timeout)
	defer cancel()
	return tool.run(ctx, input)
}
