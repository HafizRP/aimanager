// Package proxy implements the OpenAI/Anthropic-compatible reverse proxy gateway.
package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"9router-gateway/internal/entity"
)

// PipelineContext carries request state throughout the Chain of Responsibility.
type PipelineContext struct {
	Context        context.Context
	Writer         http.ResponseWriter
	Request        *http.Request
	StartTime      time.Time
	ClientIP       string
	RawAPIKey      string
	APIKey         *entity.APIKey
	User           *entity.User
	RequestBody    []byte
	BodyMap        map[string]interface{}
	RequestedModel string
	IsStream       bool
	Routing        *RoutingDecision
	ShortCircuited bool
	StatusCode     int
	ErrorMessage   string
	ErrorType      string
}

// WriteJSONError sends a formatted JSON error response and flags the context as short-circuited.
func (pctx *PipelineContext) WriteJSONError(statusCode int, message, errType string) {
	pctx.ShortCircuited = true
	pctx.StatusCode = statusCode
	pctx.ErrorMessage = message
	pctx.ErrorType = errType
	pctx.Writer.Header().Set("Content-Type", "application/json")
	pctx.Writer.WriteHeader(statusCode)
	_ = json.NewEncoder(pctx.Writer).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
			"code":    statusCode,
		},
	})
}

// NextFunc invokes the next handler in the Chain of Responsibility.
type NextFunc func(ctx *PipelineContext) error

// MiddlewareHandler defines the contract for an individual processing step in the chain.
type MiddlewareHandler interface {
	Name() string
	Handle(ctx *PipelineContext, next NextFunc) error
}

// Pipeline coordinates sequential execution of MiddlewareHandlers.
type Pipeline struct {
	handlers []MiddlewareHandler
}

// NewPipeline creates a new chain of responsibility.
func NewPipeline(handlers ...MiddlewareHandler) *Pipeline {
	return &Pipeline{handlers: handlers}
}

// Add appends a handler to the pipeline.
func (p *Pipeline) Add(handler MiddlewareHandler) {
	p.handlers = append(p.handlers, handler)
}

// Handlers returns the slice of registered handlers in order.
func (p *Pipeline) Handlers() []MiddlewareHandler {
	return p.handlers
}

// Execute runs the pipeline starting from the first handler.
func (p *Pipeline) Execute(ctx *PipelineContext) error {
	var dispatch func(index int) error
	dispatch = func(index int) error {
		if index >= len(p.handlers) || ctx.ShortCircuited {
			return nil
		}
		handler := p.handlers[index]
		return handler.Handle(ctx, func(nextCtx *PipelineContext) error {
			if nextCtx.ShortCircuited {
				return nil
			}
			return dispatch(index + 1)
		})
	}
	return dispatch(0)
}
