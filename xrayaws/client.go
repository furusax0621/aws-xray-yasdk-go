package xrayaws

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/shogo82148/aws-xray-yasdk-go/xray"
	"github.com/shogo82148/aws-xray-yasdk-go/xrayhttp"
)

type subsegments struct {
	mu           sync.Mutex
	ctx          context.Context
	awsCtx       context.Context
	awsSeg       *xray.Segment
	marshalCtx   context.Context
	marshalSeg   *xray.Segment
	attemptCtx   context.Context
	attemptSeg   *xray.Segment
	unmarshalCtx context.Context
	unmarshalSeg *xray.Segment
}

func contextSubsegments(ctx context.Context) *subsegments {
	segs := ctx.Value(segmentsContextKey)
	if segs == nil {
		return nil
	}
	return segs.(*subsegments)
}

func (segs *subsegments) beforeValidate(r *request.Request) {
	ctx := context.WithValue(r.HTTPRequest.Context(), segmentsContextKey, segs)
	segs.awsCtx, segs.awsSeg = xray.BeginSubsegment(ctx, r.ClientInfo.ServiceName)
	segs.awsSeg.SetNamespace("aws")
	r.HTTPRequest = r.HTTPRequest.WithContext(segs.awsCtx)

	segs.marshalCtx, segs.marshalSeg = xray.BeginSubsegment(segs.awsCtx, "marshal")

	// TODO: set x-amzn-trace-id header
}

var beforeValidate = request.NamedHandler{
	Name: "XRayBeforeValidateHandler",
	Fn: func(r *request.Request) {
		segs := &subsegments{
			ctx: r.HTTPRequest.Context(),
		}
		segs.beforeValidate(r)
	},
}

func (segs *subsegments) afterBuild(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.closeAll()
}

var afterBuild = request.NamedHandler{
	Name: "XRayAfterBuildHandler",
	Fn: func(r *request.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.afterBuild(r)
		}
	},
}

func (segs *subsegments) beforeSign(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.attemptCtx, segs.attemptSeg = xray.BeginSubsegment(segs.awsCtx, "attempt")
	ctx := xrayhttp.WithClientTrace(segs.attemptCtx)
	r.HTTPRequest = r.HTTPRequest.WithContext(ctx)
}

var beforeSign = request.NamedHandler{
	Name: "XRayBeforeSignHandler",
	Fn: func(r *request.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.beforeSign(r)
		}
	},
}

func (segs *subsegments) beforeUnmarshalMeta(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.unmarshalCtx, segs.unmarshalSeg = xray.BeginSubsegment(segs.awsCtx, "unmarshal")
}

var beforeUnmarshalMeta = request.NamedHandler{
	Name: "XRayBeforeUnmarshalMetaHandler",
	Fn: func(r *request.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.beforeUnmarshalMeta(r)
		}
	},
}

func (segs *subsegments) afterUnmarshalError(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.unmarshalSeg == nil {
		return
	}
	segs.unmarshalSeg.AddError(r.Error)
	segs.unmarshalSeg.Close()
	segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
}

var afterUnmarshalError = request.NamedHandler{
	Name: "XRayAfterUnmarshalErrorHandler",
	Fn: func(r *request.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.afterUnmarshalError(r)
		}
	},
}

func (segs *subsegments) afterUnmarshal(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.unmarshalSeg == nil {
		return
	}
	segs.unmarshalSeg.AddError(r.Error)
	segs.unmarshalSeg.Close()
	segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
}

var afterUnmarshal = request.NamedHandler{
	Name: "XRayAfterUnmarshalHandler",
	Fn: func(r *request.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.afterUnmarshal(r)
		}
	},
}

func (segs *subsegments) afterComplete(r *request.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.closeAll()

	if request.IsErrorThrottle(r.Error) {
		segs.awsSeg.SetThrottle()
	}
	segs.awsSeg.AddError(r.Error)
	segs.awsSeg.Close()
}

func (segs *subsegments) closeAll() {
	if segs.attemptSeg != nil {
		segs.attemptSeg.Close()
		segs.attemptCtx, segs.attemptSeg = nil, nil
	}
	if segs.marshalSeg != nil {
		segs.marshalSeg.Close()
		segs.marshalCtx, segs.marshalSeg = nil, nil
	}
	if segs.unmarshalSeg != nil {
		segs.unmarshalSeg.Close()
		segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
	}
}

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "xrayaws context value " + k.name }

var segmentsContextKey = &contextKey{"segments"}

func pushHandlers(handlers *request.Handlers, completionWhitelistFilename string) {
	handlers.Validate.PushFrontNamed(beforeValidate)
	handlers.Build.PushBackNamed(afterBuild)
	handlers.Sign.PushFrontNamed(beforeSign)
	handlers.UnmarshalMeta.PushFrontNamed(beforeUnmarshalMeta)
	handlers.UnmarshalError.PushBackNamed(afterUnmarshalError)
	handlers.Unmarshal.PushBackNamed(afterUnmarshal)
	handlers.Complete.PushBackNamed(completeHandler(completionWhitelistFilename))
}

// Client adds X-Ray tracing to an AWS client.
func Client(c *client.Client) *client.Client {
	if c == nil {
		panic("Please initialize the provided AWS client before passing to the Client() method.")
	}
	pushHandlers(&c.Handlers, "")
	return c
}

func completeHandler(filename string) request.NamedHandler {
	// TODO: parse white list
	return request.NamedHandler{
		Name: "XRayCompleteHandler",
		Fn: func(r *request.Request) {
			if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
				segs.afterComplete(r)
			}
		},
	}
}