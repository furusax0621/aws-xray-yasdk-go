package xrayaws

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/shogo82148/aws-xray-yasdk-go/xray"
	"github.com/shogo82148/aws-xray-yasdk-go/xray/schema"
	"github.com/shogo82148/aws-xray-yasdk-go/xrayaws-v2/whitelist"
	"github.com/shogo82148/aws-xray-yasdk-go/xrayhttp"
)

//go:generate go run codegen.go

type subsegments struct {
	mu            sync.Mutex
	ctx           context.Context
	awsCtx        context.Context
	awsSeg        *xray.Segment
	marshalCtx    context.Context
	marshalSeg    *xray.Segment
	attemptCtx    context.Context
	attemptSeg    *xray.Segment
	attemptCancel context.CancelFunc
	unmarshalCtx  context.Context
	unmarshalSeg  *xray.Segment
}

func contextSubsegments(ctx context.Context) *subsegments {
	segs := ctx.Value(segmentsContextKey)
	if segs == nil {
		return nil
	}
	return segs.(*subsegments)
}

func (segs *subsegments) beforeValidate(r *aws.Request) {
	ctx := context.WithValue(r.Context(), segmentsContextKey, segs)
	segs.awsCtx, segs.awsSeg = xray.BeginSubsegment(ctx, r.Metadata.EndpointsID)
	r.SetContext(ctx)
	segs.awsSeg.SetNamespace("aws")
	r.HTTPRequest.Header.Set(xray.TraceIDHeaderKey, xray.DownstreamHeader(segs.awsCtx).String())

	segs.marshalCtx, segs.marshalSeg = xray.BeginSubsegment(segs.awsCtx, "marshal")
}

var beforeValidate = aws.NamedHandler{
	Name: "XRayBeforeValidateHandler",
	Fn: func(r *aws.Request) {
		segs := &subsegments{
			ctx: r.Context(),
		}
		segs.beforeValidate(r)
	},
}

func (segs *subsegments) afterBuild(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.marshalSeg != nil {
		segs.marshalSeg.Close()
		segs.marshalCtx, segs.marshalSeg = nil, nil
	}
}

var afterBuild = aws.NamedHandler{
	Name: "XRayAfterBuildHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.Context()); segs != nil {
			segs.afterBuild(r)
		}
	},
}

func (segs *subsegments) beforeSign(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.attemptCtx, segs.attemptSeg = xray.BeginSubsegment(segs.awsCtx, "attempt")
	ctx, cancel := xrayhttp.WithClientTrace(segs.attemptCtx)
	segs.attemptCancel = cancel
	r.SetContext(ctx)
}

var beforeSign = aws.NamedHandler{
	Name: "XRayBeforeSignHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.Context()); segs != nil {
			segs.beforeSign(r)
		}
	},
}

func (segs *subsegments) afterSend(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.attemptCtx != nil {
		if r.Error != nil {
			// r.Error will be stored into segs.awsSeg,
			// so we just set fault here.
			segs.attemptSeg.SetFault()
		}
		segs.attemptCancel()
		segs.attemptSeg.Close()
		segs.attemptCtx, segs.attemptSeg = nil, nil
	}
}

var afterSend = aws.NamedHandler{
	Name: "XRayAfterSendHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.Context()); segs != nil {
			segs.afterSend(r)
		}
	},
}

func (segs *subsegments) beforeUnmarshalMeta(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	segs.unmarshalCtx, segs.unmarshalSeg = xray.BeginSubsegment(segs.awsCtx, "unmarshal")
}

var beforeUnmarshalMeta = aws.NamedHandler{
	Name: "XRayBeforeUnmarshalMetaHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.Context()); segs != nil {
			segs.beforeUnmarshalMeta(r)
		}
	},
}

func (segs *subsegments) afterUnmarshalError(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.unmarshalCtx == nil {
		return
	}
	segs.unmarshalSeg.AddError(r.Error)
	segs.unmarshalSeg.Close()
	segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
}

var afterUnmarshalError = aws.NamedHandler{
	Name: "XRayAfterUnmarshalErrorHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.Context()); segs != nil {
			segs.afterUnmarshalError(r)
		}
	},
}

func (segs *subsegments) afterUnmarshal(r *aws.Request) {
	segs.mu.Lock()
	defer segs.mu.Unlock()
	if segs.unmarshalCtx == nil {
		return
	}
	segs.unmarshalSeg.AddError(r.Error)
	segs.unmarshalSeg.Close()
	segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
}

var afterUnmarshal = aws.NamedHandler{
	Name: "XRayAfterUnmarshalHandler",
	Fn: func(r *aws.Request) {
		if segs := contextSubsegments(r.HTTPRequest.Context()); segs != nil {
			segs.afterUnmarshal(r)
		}
	},
}

func (segs *subsegments) afterComplete(r *aws.Request, awsData schema.AWS) {
	segs.mu.Lock()
	defer segs.mu.Unlock()

	// make share all segments closed.
	if segs.attemptCtx != nil {
		segs.attemptCancel()
		segs.attemptSeg.Close()
		segs.attemptCancel = nil
		segs.attemptCtx, segs.attemptSeg = nil, nil
	}
	if segs.marshalCtx != nil {
		segs.marshalSeg.Close()
		segs.marshalCtx, segs.marshalSeg = nil, nil
	}
	if segs.unmarshalCtx != nil {
		segs.unmarshalSeg.Close()
		segs.unmarshalCtx, segs.unmarshalSeg = nil, nil
	}

	if resp := r.HTTPResponse; resp != nil {
		segs.awsSeg.SetHTTPResponse(&schema.HTTPResponse{
			Status:        resp.StatusCode,
			ContentLength: resp.ContentLength,
		})

		// record the s3 extend request id.
		if ext := resp.Header.Get("x-amz-id-2"); ext != "" {
			awsData.Set("id_2", ext)
		}
	}
	segs.awsSeg.SetAWS(awsData)

	var v interface{ StatusCode() int }
	if errors.As(r.Error, &v) {
		if v.StatusCode() == http.StatusTooManyRequests {
			segs.awsSeg.SetThrottle()
		}
	}
	segs.awsSeg.AddError(r.Error)
	segs.awsSeg.Close()
}

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "xrayaws-v2 context value " + k.name }

var segmentsContextKey = &contextKey{"segments"}

func pushHandlers(handlers *aws.Handlers, list *whitelist.Whitelist) {
	handlers.Validate.PushFrontNamed(beforeValidate)
	handlers.Build.PushBackNamed(afterBuild)
	handlers.Sign.PushFrontNamed(beforeSign)
	handlers.Send.PushBackNamed(afterSend)
	handlers.Unmarshal.PushFrontNamed(beforeUnmarshalMeta)
	handlers.UnmarshalError.PushBackNamed(afterUnmarshalError)
	handlers.Unmarshal.PushBackNamed(afterUnmarshal)
	handlers.Complete.PushBackNamed(completeHandler(list))
}

// Client adds X-Ray tracing to an AWS client.
func Client(c *aws.Client) *aws.Client {
	if c == nil {
		panic("Please initialize the provided AWS client before passing to the Client() method.")
	}
	pushHandlers(&c.Handlers, defaultWhitelist)
	return c
}

// ClientWithWhitelist adds X-Ray tracing to an AWS client with custom whitelist.
func ClientWithWhitelist(c *aws.Client, whitelist *whitelist.Whitelist) *aws.Client {
	if c == nil {
		panic("Please initialize the provided AWS client before passing to the Client() method.")
	}
	pushHandlers(&c.Handlers, whitelist)
	return c
}

func completeHandler(list *whitelist.Whitelist) aws.NamedHandler {
	if list == nil {
		list = &whitelist.Whitelist{
			Services: map[string]*whitelist.Service{},
		}
	}
	return aws.NamedHandler{
		Name: "XRayCompleteHandler",
		Fn: func(r *aws.Request) {
			segs := contextSubsegments(r.HTTPRequest.Context())
			if segs == nil {
				return
			}
			awsData := schema.AWS{
				"region":     r.Config.Region,
				"operation":  r.Operation.Name,
				"retries":    r.RetryCount,
				"request_id": r.RequestID,
			}
			insertParameter(awsData, r, list)
			segs.afterComplete(r, awsData)
		},
	}
}

func insertParameter(aws schema.AWS, r *aws.Request, list *whitelist.Whitelist) {
	service, ok := list.Services[r.Metadata.EndpointsID]
	if !ok {
		return
	}
	operation, ok := service.Operations[r.Operation.Name]
	if !ok {
		return
	}
	for _, key := range operation.RequestParameters {
		aws.Set(key, getValue(r.Params, key))
	}
	for key, desc := range operation.RequestDescriptors {
		insertDescriptor(desc, aws, r.Params, key)
	}
	for _, key := range operation.ResponseParameters {
		aws.Set(key, getValue(r.Data, key))
	}
	for key, desc := range operation.ResponseDescriptors {
		insertDescriptor(desc, aws, r.Data, key)
	}
}

func getValue(v interface{}, key string) interface{} {
	v1 := reflect.ValueOf(v)
	if v1.Kind() == reflect.Ptr {
		v1 = v1.Elem()
	}
	if v1.Kind() != reflect.Struct {
		return nil
	}
	typ := v1.Type()

	// i starts 1 because first field is always struct{}
	for i := 1; i < v1.NumField(); i++ {
		if typ.Field(i).Name == key {
			return v1.Field(i).Interface()
		}
	}
	return nil
}

func insertDescriptor(desc *whitelist.Descriptor, aws schema.AWS, v interface{}, key string) {
	renameTo := desc.RenameTo
	if renameTo == "" {
		renameTo = key
	}
	value := getValue(v, key)
	switch {
	case desc.Map:
		if !desc.GetKeys {
			return
		}
		val := reflect.ValueOf(value)
		if val.Kind() != reflect.Map {
			return
		}
		keySlice := make([]interface{}, 0, val.Len())
		for _, key := range val.MapKeys() {
			keySlice = append(keySlice, key.Interface())
		}
		aws.Set(renameTo, keySlice)
	case desc.List:
		if !desc.GetCount {
			return
		}
		val := reflect.ValueOf(value)
		if kind := val.Kind(); kind != reflect.Slice && kind != reflect.Array {
			return
		}
		aws.Set(renameTo, val.Len())
	default:
		aws.Set(renameTo, value)
	}
}
