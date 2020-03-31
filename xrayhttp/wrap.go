// Code generated by codegen.go; DO NOT EDIT

package xrayhttp

import "net/http"

func wrap(rw *serverResponseTracer) http.ResponseWriter {
	var n uint
	if _, ok := rw.rw.(http.Flusher); ok {
		n |= 0x1
	}
	if _, ok := rw.rw.(http.CloseNotifier); ok {
		n |= 0x2
	}
	if _, ok := rw.rw.(http.Hijacker); ok {
		n |= 0x4
	}
	if _, ok := rw.rw.(http.Pusher); ok {
		n |= 0x8
	}
	switch n {
	case 0x0:
		return struct {
			responseWriter
		}{rw}
	case 0x1:
		return struct {
			responseWriter
			http.Flusher
		}{rw, rw}
	case 0x2:
		return struct {
			responseWriter
			http.CloseNotifier
		}{rw, rw}
	case 0x3:
		return struct {
			responseWriter
			http.Flusher
			http.CloseNotifier
		}{rw, rw, rw}
	case 0x4:
		return struct {
			responseWriter
			http.Hijacker
		}{rw, rw}
	case 0x5:
		return struct {
			responseWriter
			http.Flusher
			http.Hijacker
		}{rw, rw, rw}
	case 0x6:
		return struct {
			responseWriter
			http.CloseNotifier
			http.Hijacker
		}{rw, rw, rw}
	case 0x7:
		return struct {
			responseWriter
			http.Flusher
			http.CloseNotifier
			http.Hijacker
		}{rw, rw, rw, rw}
	case 0x8:
		return struct {
			responseWriter
			http.Pusher
		}{rw, rw}
	case 0x9:
		return struct {
			responseWriter
			http.Flusher
			http.Pusher
		}{rw, rw, rw}
	case 0xa:
		return struct {
			responseWriter
			http.CloseNotifier
			http.Pusher
		}{rw, rw, rw}
	case 0xb:
		return struct {
			responseWriter
			http.Flusher
			http.CloseNotifier
			http.Pusher
		}{rw, rw, rw, rw}
	case 0xc:
		return struct {
			responseWriter
			http.Hijacker
			http.Pusher
		}{rw, rw, rw}
	case 0xd:
		return struct {
			responseWriter
			http.Flusher
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw}
	case 0xe:
		return struct {
			responseWriter
			http.CloseNotifier
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw}
	case 0xf:
		return struct {
			responseWriter
			http.Flusher
			http.CloseNotifier
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw, rw}
	}
	panic("unreachable")
}
