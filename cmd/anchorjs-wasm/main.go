//go:build js && wasm

// Command anchorjs-wasm exposes the Go AnchorJS implementation to the browser
// as a global `AnchorJS` constructor plus a default `anchors` instance,
// mirroring the browser-globals branch of the original UMD wrapper:
//
//	root.AnchorJS = factory();
//	root.anchors  = new root.AnchorJS();
//
// This lets the exact same behavioral boundary (a real browser DOM) be
// exercised by the migrated test suite, just like the original Karma/Jasmine
// tests ran anchor.js in ChromeHeadless.
package main

import (
	"errors"
	"syscall/js"

	"github.com/bryanbraun/anchorjs/anchorjs"
)

func main() {
	registerAnchorJS()
	// Keep the Go runtime alive so the exported functions remain callable.
	select {}
}

// throwSentinelKey is the property name on a sentinel object returned by a Go
// callback to signal that the JS-side wrapper must `throw` the carried value.
//
// This mechanism is required because, in Go/WASM, an unrecovered panic inside a
// js.FuncOf callback tears down the entire Go program (unwinding main's
// select{}), after which every exported function reports "Go program has
// already exited". Re-invoking a throwing JS function from a Go recover also
// double-panics. So instead of panicking, a callback returns
// {__anchorjs_throw__: <JS Error>} and a thin JS wrapper performs the actual
// `throw`, reproducing the original library's throw behavior while keeping the
// runtime alive.
const throwSentinelKey = "__anchorjs_throw__"

// newThrowSentinel builds a sentinel object carrying a real JS TypeError with
// the exact original message, so Jasmine's toThrowError(Error, message) matches
// (TypeError is an instanceof Error).
func newThrowSentinel(message string) js.Value {
	sentinel := js.Global().Get("Object").New()
	sentinel.Set(throwSentinelKey, js.Global().Get("TypeError").New(message))
	return sentinel
}

// wrap adapts an *anchorjs.AnchorJS into a JS object with the same public
// surface as the original AnchorJS instance: methods add/remove/removeAll/
// urlify/hasAnchorJSLink and properties options/elements.
//
// The add/remove methods are wrapped by a JS shim (see wrapThrowingMethods)
// that turns a returned throw-sentinel into an actual JS `throw` and preserves
// chainability.
func wrap(a *anchorjs.AnchorJS) js.Value {
	obj := js.Global().Get("Object").New()

	// options: expose as a live accessor pair so reads and writes hit the
	// underlying live JS options object, and reassigning `.options = {...}`
	// replaces it (mirroring `anchors.options = { ... }`).
	defineAccessor(obj, "options",
		js.FuncOf(func(this js.Value, args []js.Value) any {
			return a.Options
		}),
		js.FuncOf(func(this js.Value, args []js.Value) any {
			if len(args) > 0 {
				a.SetOptions(args[0])
			}
			return js.Undefined()
		}),
	)

	// elements: getter returns a fresh JS array reflecting current elements.
	defineGetter(obj, "elements",
		js.FuncOf(func(this js.Value, args []js.Value) any {
			arr := js.Global().Get("Array").New()
			for _, el := range a.Elements() {
				arr.Call("push", el)
			}
			return arr
		}),
	)

	// add: returns the wrapper for chaining, or a throw-sentinel on an invalid
	// selector (rethrown by the JS shim).
	obj.Set("add", js.FuncOf(func(this js.Value, args []js.Value) any {
		var sel js.Value
		if len(args) > 0 {
			sel = args[0]
		} else {
			sel = js.Undefined()
		}
		if _, err := a.Add(sel); err != nil {
			return throwSentinelFor(err)
		}
		return obj // chainable: return the JS wrapper
	}))

	// remove: same contract as add.
	obj.Set("remove", js.FuncOf(func(this js.Value, args []js.Value) any {
		var sel js.Value
		if len(args) > 0 {
			sel = args[0]
		} else {
			sel = js.Undefined()
		}
		if _, err := a.Remove(sel); err != nil {
			return throwSentinelFor(err)
		}
		return obj
	}))

	obj.Set("removeAll", js.FuncOf(func(this js.Value, args []js.Value) any {
		a.RemoveAll()
		return js.Undefined()
	}))

	obj.Set("urlify", js.FuncOf(func(this js.Value, args []js.Value) any {
		text := ""
		if len(args) > 0 {
			text = args[0].String()
		}
		return a.Urlify(text)
	}))

	obj.Set("hasAnchorJSLink", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return false
		}
		return a.HasAnchorJSLink(args[0])
	}))

	// Install the JS-side shims that convert throw-sentinels into real throws
	// while preserving chainability.
	wrapThrowingMethods(obj)

	return obj
}

// throwSentinelFor maps a Go error to a JS throw-sentinel. Every error the
// public API can produce is an invalid-selector error, matching the original
// TypeError message.
func throwSentinelFor(err error) js.Value {
	var invalid anchorjs.ErrInvalidSelector
	if errors.As(err, &invalid) {
		return newThrowSentinel(anchorjs.InvalidSelectorMessage)
	}
	// Any other (unexpected) error is surfaced with its own message, still as a
	// JS throw rather than a runtime-killing panic.
	return newThrowSentinel(err.Error())
}

// wrapThrowMethodShim is a JS function that wraps a Go-exported method so that a
// returned throw-sentinel becomes a real `throw`, and any other return value is
// passed through unchanged. Created once and reused.
var wrapThrowMethodShim = js.Global().Call("eval", `(function(impl, sentinelKey){
  return function() {
    var result = impl.apply(this, arguments);
    if (result && typeof result === 'object' && result[sentinelKey] !== undefined) {
      throw result[sentinelKey];
    }
    return result;
  };
})`)

// wrapThrowingMethods replaces obj.add / obj.remove with JS shims that rethrow a
// throw-sentinel. The shims call the underlying Go implementation via
// Function.prototype.apply, so chainability (returning the wrapper) is
// preserved.
func wrapThrowingMethods(obj js.Value) {
	for _, name := range []string{"add", "remove"} {
		impl := obj.Get(name)
		shim := wrapThrowMethodShim.Invoke(impl, throwSentinelKey)
		obj.Set(name, shim)
	}
}

func registerAnchorJS() {
	// AnchorJS constructor function. Callable as `new AnchorJS(options)`.
	// When invoked with `new`, syscall/js passes the freshly created `this`
	// object; we populate it with the same surface as wrap() by copying.
	ctor := js.FuncOf(func(this js.Value, args []js.Value) any {
		var opts js.Value
		if len(args) > 0 {
			opts = args[0]
		} else {
			opts = js.Undefined()
		}
		a := anchorjs.New(opts)
		wrapper := wrap(a)

		// If called with `new`, copy the wrapper's own properties/accessors
		// onto `this` so `instanceof`-style usage and direct property access
		// both work. Object.defineProperties copies accessor descriptors.
		if this.Type() == js.TypeObject {
			descriptors := js.Global().Get("Object").Call("getOwnPropertyDescriptors", wrapper)
			js.Global().Get("Object").Call("defineProperties", this, descriptors)
			return js.Undefined()
		}
		return wrapper
	})

	js.Global().Set("AnchorJS", ctor)
	// Default instance, mirroring `root.anchors = new root.AnchorJS();`.
	defaultInstance := wrap(anchorjs.New(js.Undefined()))
	js.Global().Set("anchors", defaultInstance)
}

func defineAccessor(obj js.Value, name string, getter, setter js.Func) {
	desc := js.Global().Get("Object").New()
	desc.Set("get", getter)
	desc.Set("set", setter)
	desc.Set("enumerable", true)
	desc.Set("configurable", true)
	js.Global().Get("Object").Call("defineProperty", obj, name, desc)
}

func defineGetter(obj js.Value, name string, getter js.Func) {
	desc := js.Global().Get("Object").New()
	desc.Set("get", getter)
	desc.Set("enumerable", true)
	desc.Set("configurable", true)
	js.Global().Get("Object").Call("defineProperty", obj, name, desc)
}
