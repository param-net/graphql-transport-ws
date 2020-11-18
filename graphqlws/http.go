package graphqlws

import (
	"context"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/graph-gophers/graphql-transport-ws/graphqlws/internal/connection"
)

const protocolGraphQLWS = "graphql-ws"

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	Subprotocols: []string{protocolGraphQLWS},
}

// The ContextGenerator takes a context and the http request it can be used
// to take values out of the request context and assign them to a new context
// that will be supplied to the websocket connection go routine and be accessible
// in the resolver.
// The http request context should not be modified as any changes made will
// not be accessible in the resolver.
type ContextGeneratorFunc func(context.Context, *http.Request) (context.Context, error)

// BuildContext calls f(ctx, r) and returns a context and error
func (f ContextGeneratorFunc) BuildContext(ctx context.Context, r *http.Request) (context.Context, error) {
	return f(ctx, r)
}

// A ContextGenerator handles any changes made to the the connection context prior
// to creating the websocket connection routine.
type ContextGenerator interface {
	BuildContext(context.Context, *http.Request) (context.Context, error)
}

// Option applies configuration when a graphql websocket connection is handled
type Option interface {
	apply(*options)
}

type options struct {
	contextGenerators []ContextGenerator
}

type optionFunc func(*options)

func (f optionFunc) apply(o *options) {
	f(o)
}

// WithContextGenerator specifies that the background context of the websocket connection go routine
// should be built upon by executing provided context generators
func WithContextGenerator(f ContextGenerator) Option {
	return optionFunc(func(o *options) {
		o.contextGenerators = append(o.contextGenerators, f)
	})
}

func applyOptions(opts ...Option) *options {
	var o options

	for _, op := range opts {
		op.apply(&o)
	}

	return &o
}

func processHTTPRequst(ctx *context.Context, options *options, svc connection.GraphQLService, httpHandler http.Handler, w http.ResponseWriter, r *http.Request) {
	if ctx == nil && options == nil {
		_ctx := context.Background()
		ctx = &_ctx
	}
	for _, subprotocol := range websocket.Subprotocols(r) {
		if subprotocol == "graphql-ws" {
			if options != nil {
				_ctx, err := buildContext(r, options.contextGenerators)
				if err != nil {
					return
				}
				ctx = &_ctx
			}
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}

			if ws.Subprotocol() != protocolGraphQLWS {
				ws.Close()
				return
			}

			go connection.Connect(*ctx, ws, svc)
			return
		}
	}
	// Fallback to HTTP
	httpHandler.ServeHTTP(w, r)
}

// NewHandler returns an http.HandlerFunc that supports GraphQL over websockets
func NewHandler(ctx context.Context, svc connection.GraphQLService, httpHandler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		processHTTPRequst(&ctx, nil, svc, httpHandler, w, r)
	}
}

// NewHandlerFunc returns an http.HandlerFunc that supports GraphQL over websockets
func NewHandlerFunc(svc connection.GraphQLService, httpHandler http.Handler, options ...Option) http.HandlerFunc {
	o := applyOptions(options...)

	return func(w http.ResponseWriter, r *http.Request) {
		processHTTPRequst(nil, o, svc, httpHandler, w, r)
	}
}

func buildContext(r *http.Request, generators []ContextGenerator) (context.Context, error) {
	ctx := context.Background()
	for _, g := range generators {
		var err error
		ctx, err = g.BuildContext(ctx, r)
		if err != nil {
			return nil, err
		}
	}

	return ctx, nil
}
