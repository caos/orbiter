package mntr

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

type OnMessage func(string, map[string]string)
type OnError func(error, map[string]string)
type OnRecoverPanic func(interface{}, map[string]string)

type Monitor struct {
	Fields         map[string]interface{}
	OnInfo         OnMessage
	OnChange       OnMessage
	OnError        OnError
	OnRecoverPanic OnRecoverPanic
	verbose        bool
}

func (m Monitor) WithField(key string, value interface{}) Monitor {
	return m.WithFields(map[string]interface{}{key: value})
}

func (m Monitor) WithFields(add map[string]interface{}) Monitor {
	m.Fields = merge(m.Fields, add)
	return m
}

func (m Monitor) Info(msg string) {
	if m.OnInfo == nil {
		return
	}

	m.Fields = merge(map[string]interface{}{
		"msg": msg,
		"ts":  now(),
	}, m.Fields)

	if m.verbose {
		m.addDebugContext()
	}
	m.OnInfo(msg, normalize(m.Fields))
}

func (m Monitor) Changed(evt string) {
	if m.OnChange == nil {
		return
	}

	m.Fields = merge(map[string]interface{}{
		"evt": evt,
		"ts":  now(),
	}, m.Fields)

	if m.verbose {
		m.addDebugContext()
	}
	m.OnChange(evt, normalize(m.Fields))
}

func (m Monitor) Error(err error) {

	if err == nil {
		return
	}

	m.captureWithFields(func(client *sentry.Client, scope sentry.EventModifier) {
		client.CaptureException(err, nil, scope)
	})

	if m.OnError == nil {
		return
	}

	m.Fields = merge(map[string]interface{}{
		"err": err.Error(),
		"ts":  now(),
	}, m.Fields)

	m.addDebugContext()
	m.OnError(err, normalize(m.Fields))
}

func (m Monitor) CaptureMessage(msg string) {
	m.captureWithFields(func(client *sentry.Client, scope sentry.EventModifier) {
		client.CaptureMessage(msg, nil, scope)
	})
}

func (m Monitor) RecoverPanic() {
	if m.OnRecoverPanic == nil {
		return
	}
	r := recover()
	if r != nil {
		m.Fields = merge(map[string]interface{}{
			"ts":    now(),
			"panic": r,
			"msg":   "An internal error occured. Please file an issue at https://github.com/caos/orbos/issues containing the following stack trace",
		}, m.Fields)

		m.addDebugContext()
		m.OnRecoverPanic(r, normalize(m.Fields))
	}

	if sentryClient != nil {
		if r != nil {
			sentryClient.Recover(r, nil, nil)
		}
		// Also send errors if it wasn't a panic
		sentryClient.Flush(time.Second * 2)
	}
	if r != nil {
		panic(r)
	}
}

func (m Monitor) Debug(dbg string) {
	if !m.verbose {
		return
	}

	m.Fields = merge(map[string]interface{}{
		"dbg": dbg,
		"ts":  now(),
	}, m.Fields)
	m.addDebugContext()
	LogMessage(dbg, normalize(m.Fields))
}

func (m Monitor) Verbose() Monitor {
	m.verbose = true
	return m
}

func (m Monitor) IsVerbose() bool {
	return m.verbose
}

func now() string {
	return time.Now().Format(time.RFC3339)
}

func merge(fields map[string]interface{}, add map[string]interface{}) map[string]interface{} {
	newFields := make(map[string]interface{})
	for k, v := range fields {
		newFields[k] = v
	}
	for k, v := range add {
		panicOnReserved(k)
		newFields[k] = v
	}
	return newFields
}

func panicOnReserved(key string) {
	switch key {
	case "ts":
		fallthrough
	case "msg":
		fallthrough
	case "dbg":
		fallthrough
	case "evt":
		fallthrough
	case "src":
		fallthrough
	case "err":
		panic(fmt.Errorf("Key \"%s\" is reserved", key))
	}
}

func (m *Monitor) addDebugContext() {

	pc := make([]uintptr, 15)
	n := runtime.Callers(1, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, more := frames.Next()

	seenCaller := false
framesLoop:
	for more && frame.Func != nil {
		receiverStart := strings.LastIndex(frame.Function, "/")
		receiverEnd := strings.LastIndex(frame.Function, ".")
		receiver := ""
		if receiverStart != -1 && receiverEnd != -1 {
			receiver = frame.Function[receiverStart+1 : receiverEnd]
		}

		if !strings.Contains(receiver, ".Monitor") {
			if seenCaller {
				m.Fields["src"] = fmt.Sprintf("%s:%d", frame.File, frame.Line)
				return
			}

			frame, more = frames.Next()
			continue framesLoop
		}
		seenCaller = true
		frame, more = frames.Next()
	}
}
