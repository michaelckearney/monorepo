package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"
)

// driver runs generate's async generator to its end, giving each value to
// emit, then calls finish, with the error if it failed.
var driver = goja.MustCompile("driver.js", `(function (iterator, emit, finish) {
	function step() {
		iterator.next().then(function (r) {
			if (r.done) {
				finish();
				return;
			}
			emit(r.value);
			step();
		}).catch(finish);
	}
	step();
})`, true)

// Generate runs the provider's generate for one request, in a new runtime,
// and gives emit each thing it yields. Cancelling ctx stops the code and its
// requests.
func (m Model) Generate(ctx context.Context, req Request, emit func(Output)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}
	r := &runtime{ctx: ctx, vm: goja.New(), jobs: make(chan func()), name: m.Provider.Name, client: client}
	defer r.closeBodies()
	stop := context.AfterFunc(ctx, func() { r.vm.Interrupt(ctx.Err()) })
	defer stop()
	r.install()

	err := r.start(m, req, emit)
	for err == nil && !r.finished {
		select {
		case job := <-r.jobs:
			job()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return r.err
}

func (r *runtime) start(m Model, req Request, emit func(Output)) error {
	vm := r.vm
	if _, err := vm.RunProgram(m.Provider.program); err != nil {
		return err
	}
	provider := exported(vm).(*goja.Object)
	generate, _ := goja.AssertFunction(provider.Get("generate"))

	settings := map[string]string{}
	for _, s := range m.Provider.Settings {
		settings[s.Key] = s.Default
		if v := m.Settings[s.Key]; v != "" {
			settings[s.Key] = v
		}
	}
	// JSON arrays, never null, so the provider can always map them.
	if req.Messages == nil {
		req.Messages = []Message{}
	}
	if req.Tools == nil {
		req.Tools = []Tool{}
	}
	request, err := r.fromJSON(req)
	if err != nil {
		return err
	}
	info := vm.NewObject()
	info.Set("settings", settings)
	iterator, err := generate(provider, request, info)
	if err != nil {
		return jsError(err)
	}
	if it, ok := iterator.(*goja.Object); !ok || it.Get("next") == nil {
		return errors.New("generate must be an async generator function: async *generate(request, context) { ... }")
	}

	run, err := vm.RunProgram(driver)
	if err != nil {
		return err
	}
	drive, _ := goja.AssertFunction(run)
	_, err = drive(goja.Undefined(), iterator,
		vm.ToValue(func(v goja.Value) {
			out, err := r.output(v)
			if err != nil {
				panic(vm.NewTypeError(err.Error()))
			}
			emit(out)
		}),
		vm.ToValue(func(failure goja.Value) {
			r.finished = true
			if !absent(failure) {
				r.err = errors.New(message(failure))
			}
		}))
	return err
}

// runtime is one run of a provider: a goja runtime, and the event loop that
// answers its promises. Only the loop's goroutine touches the runtime; work
// in other goroutines, such as a request, posts a job to it.
type runtime struct {
	ctx      context.Context
	vm       *goja.Runtime
	jobs     chan func()
	name     string
	client   *http.Client
	bodies   []io.Closer
	finished bool
	err      error
}

// post runs job on the loop, unless the run has ended.
func (r *runtime) post(job func()) {
	select {
	case r.jobs <- job:
	case <-r.ctx.Done():
	}
}

func (r *runtime) closeBodies() {
	for _, b := range r.bodies {
		b.Close()
	}
}

// async runs work in another goroutine, and settles the promise it returns
// on the loop with what work returned.
func (r *runtime) async(work func() (func() goja.Value, error)) goja.Value {
	promise, resolve, reject := r.vm.NewPromise()
	go func() {
		value, err := work()
		r.post(func() {
			if err != nil {
				reject(r.vm.NewTypeError(err.Error()))
				return
			}
			resolve(value())
		})
	}()
	return r.vm.ToValue(promise)
}

// install gives the provider what it can use: fetch, TextDecoder, and
// console. It has no other way out of its runtime.
func (r *runtime) install() {
	r.vm.Set("fetch", r.fetch)
	r.vm.Set("TextDecoder", r.textDecoder)
	console := r.vm.NewObject()
	logf := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		log.Printf("provider %s: %s", r.name, strings.Join(parts, " "))
		return goja.Undefined()
	}
	for _, name := range []string{"log", "info", "warn", "error"} {
		console.Set(name, logf)
	}
	r.vm.Set("console", console)
}

// fetch is the part of the web's fetch that providers need: a method,
// headers, and a string body; and a response with status, headers, text(),
// json(), and a body to read as it streams in.
func (r *runtime) fetch(call goja.FunctionCall) goja.Value {
	address := call.Argument(0).String()
	method, body := "GET", ""
	headers := http.Header{}
	if init, ok := call.Argument(1).(*goja.Object); ok {
		if v := init.Get("method"); !absent(v) {
			method = strings.ToUpper(v.String())
		}
		if v := init.Get("body"); !absent(v) {
			body = v.String()
		}
		if h, ok := init.Get("headers").(*goja.Object); ok {
			for _, k := range h.Keys() {
				headers.Set(k, h.Get(k).String())
			}
		}
	}
	return r.async(func() (func() goja.Value, error) {
		req, err := http.NewRequestWithContext(r.ctx, method, address, strings.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("fetch: %v", err)
		}
		req.Header = headers
		resp, err := r.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %v", address, err)
		}
		return func() goja.Value {
			r.bodies = append(r.bodies, resp.Body)
			return r.response(resp)
		}, nil
	})
}

func (r *runtime) response(resp *http.Response) goja.Value {
	vm := r.vm
	obj := vm.NewObject()
	obj.Set("status", resp.StatusCode)
	obj.Set("ok", resp.StatusCode >= 200 && resp.StatusCode < 300)
	obj.Set("statusText", http.StatusText(resp.StatusCode))
	obj.Set("url", resp.Request.URL.String())
	headers := vm.NewObject()
	headers.Set("get", func(name string) goja.Value {
		if v := resp.Header.Values(name); len(v) > 0 {
			return vm.ToValue(strings.Join(v, ", "))
		}
		return goja.Null()
	})
	obj.Set("headers", headers)

	readAll := func() (string, error) {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("reading the response failed: %v", err)
		}
		return string(data), nil
	}
	obj.Set("text", func() goja.Value {
		return r.async(func() (func() goja.Value, error) {
			text, err := readAll()
			return func() goja.Value { return vm.ToValue(text) }, err
		})
	})
	obj.Set("json", func() goja.Value {
		promise, resolve, reject := vm.NewPromise()
		go func() {
			text, err := readAll()
			r.post(func() {
				if err != nil {
					reject(vm.NewTypeError(err.Error()))
					return
				}
				v, err := r.parse(text)
				if err != nil {
					reject(vm.NewGoError(err))
					return
				}
				resolve(v)
			})
		}()
		return vm.ToValue(promise)
	})

	stream := vm.NewObject()
	stream.Set("getReader", func() goja.Value {
		reader := vm.NewObject()
		reader.Set("read", func() goja.Value {
			return r.async(func() (func() goja.Value, error) {
				buf := make([]byte, 32*1024)
				n, err := resp.Body.Read(buf)
				for n == 0 && err == nil {
					n, err = resp.Body.Read(buf)
				}
				if err != nil && !errors.Is(err, io.EOF) {
					return nil, fmt.Errorf("reading the response failed: %v", err)
				}
				return func() goja.Value {
					result := vm.NewObject()
					if n == 0 {
						result.Set("done", true)
						result.Set("value", goja.Undefined())
						return result
					}
					bytes, _ := vm.New(vm.Get("Uint8Array"), vm.ToValue(vm.NewArrayBuffer(buf[:n])))
					result.Set("done", false)
					result.Set("value", bytes)
					return result
				}, nil
			})
		})
		reader.Set("cancel", func() { resp.Body.Close() })
		reader.Set("releaseLock", func() {})
		return reader
	})
	obj.Set("body", stream)
	return obj
}

// textDecoder decodes UTF-8. With { stream: true }, a character split
// between two chunks is kept until the next one.
func (r *runtime) textDecoder(call goja.ConstructorCall) *goja.Object {
	var rest []byte
	call.This.Set("decode", func(c goja.FunctionCall) goja.Value {
		data := rest
		rest = nil
		if v := c.Argument(0); !absent(v) {
			b, ok := v.Export().([]byte)
			if !ok {
				panic(r.vm.NewTypeError("TextDecoder.decode takes a Uint8Array"))
			}
			data = append(data, b...)
		}
		if opts, ok := c.Argument(1).(*goja.Object); ok && opts.Get("stream").ToBoolean() {
			cut := len(data)
			for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
				if utf8.RuneStart(data[i]) {
					if !utf8.FullRune(data[i:]) {
						cut = i
					}
					break
				}
			}
			rest = bytes.Clone(data[cut:])
			data = data[:cut]
		}
		return r.vm.ToValue(strings.ToValidUTF8(string(data), "�"))
	})
	return nil
}

func (r *runtime) parse(text string) (goja.Value, error) {
	parse, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("parse"))
	v, err := parse(goja.Undefined(), r.vm.ToValue(text))
	if err != nil {
		return nil, errors.New(jsError(err).Error())
	}
	return v, nil
}

// fromJSON gives the provider v as plain JavaScript objects.
func (r *runtime) fromJSON(v any) (goja.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return r.parse(string(data))
}

// output checks a value generate yielded.
func (r *runtime) output(v goja.Value) (Output, error) {
	stringify, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("stringify"))
	data, err := stringify(goja.Undefined(), v)
	if err != nil || absent(data) {
		return Output{}, fmt.Errorf("generate yielded %s; yield { text }, { toolCall }, or { done: true }", v)
	}
	dec := json.NewDecoder(strings.NewReader(data.String()))
	dec.DisallowUnknownFields()
	var out Output
	if err := dec.Decode(&out); err != nil {
		return Output{}, fmt.Errorf("generate yielded %s: %v; yield { text }, { toolCall }, or { done: true }", data, err)
	}
	if c := out.ToolCall; c != nil {
		if c.ID == "" || c.Name == "" {
			return Output{}, fmt.Errorf("generate yielded a tool call without an id or a name: %s", data)
		}
		if c.Input == nil {
			c.Input = map[string]any{}
		}
	}
	return out, nil
}

// jsError gives the message of an error thrown in JavaScript.
func jsError(err error) error {
	var exc *goja.Exception
	if errors.As(err, &exc) {
		return errors.New(message(exc.Value()))
	}
	return err
}

func message(v goja.Value) string {
	if obj, ok := v.(*goja.Object); ok {
		if m := obj.Get("message"); !absent(m) {
			return m.String()
		}
	}
	return v.String()
}
