//go:build js && wasm

// Package anchorjs is a faithful Go/WebAssembly migration of AnchorJS
// (https://github.com/bryanbraun/anchorjs), a client-side library that adds
// deep anchor links to existing page content.
//
// The library is browser/DOM oriented. This Go package manipulates the real
// DOM through syscall/js, preserving 100% of the original JavaScript's
// observable behavior (generated anchors, IDs/slugs, options, ARIA
// attributes, styles, event/URL/hash behavior, and edge cases).
//
// The original source is anchor.js (v5.0.0). Every method and option is
// reproduced here with equivalent semantics.
package anchorjs

import (
	"math"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/bryanbraun/anchorjs/anchorjs/internal/slug"
)

// defaultSelector matches the JS default used by add() when no selector given.
const defaultSelector = "h2, h3, h4, h5, h6"

// defaultIcon is the AnchorJS default icon glyph, U+E9CB, matching '\uE9CB'.
const defaultIcon = "\uE9CB"

// InvalidSelectorMessage matches the exact error text thrown by the original
// implementation when an inappropriate selector is provided.
const InvalidSelectorMessage = "The selector provided to AnchorJS was invalid."

// baseline CSS rules inserted by _addBaselineStyles, byte-for-byte identical to
// the original anchor.js source (order preserved on insertion).
const (
	linkRule = ".anchorjs-link{" +
		"opacity:0;" +
		"text-decoration:none;" +
		"-webkit-font-smoothing:antialiased;" +
		"-moz-osx-font-smoothing:grayscale" +
		"}"

	hoverRule = ":hover>.anchorjs-link," +
		".anchorjs-link:focus{" +
		"opacity:1" +
		"}"

	pseudoElContent = "[data-anchorjs-icon]::after{" +
		"content:attr(data-anchorjs-icon)" +
		"}"

	anchorjsLinkFontFace = "@font-face{" +
		"font-family:anchorjs-icons;" +
		"src:url(data:n/a;base64,AAEAAAALAIAAAwAwT1MvMg8yG2cAAAE4AAAAYGNtYXDp3gC3AAABpAAAAExnYXNwAAAAEAAAA9wAAAAIZ2x5ZlQCcfwAAAH4AAABCGhlYWQHFvHyAAAAvAAAADZoaGVhBnACFwAAAPQAAAAkaG10eASAADEAAAGYAAAADGxvY2EACACEAAAB8AAAAAhtYXhwAAYAVwAAARgAAAAgbmFtZQGOH9cAAAMAAAAAunBvc3QAAwAAAAADvAAAACAAAQAAAAEAAHzE2p9fDzz1AAkEAAAAAADRecUWAAAAANQA6R8AAAAAAoACwAAAAAgAAgAAAAAAAAABAAADwP/AAAACgAAA/9MCrQABAAAAAAAAAAAAAAAAAAAAAwABAAAAAwBVAAIAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAMCQAGQAAUAAAKZAswAAACPApkCzAAAAesAMwEJAAAAAAAAAAAAAAAAAAAAARAAAAAAAAAAAAAAAAAAAAAAQAAg//0DwP/AAEADwABAAAAAAQAAAAAAAAAAAAAAIAAAAAAAAAIAAAACgAAxAAAAAwAAAAMAAAAcAAEAAwAAABwAAwABAAAAHAAEADAAAAAIAAgAAgAAACDpy//9//8AAAAg6cv//f///+EWNwADAAEAAAAAAAAAAAAAAAAACACEAAEAAAAAAAAAAAAAAAAxAAACAAQARAKAAsAAKwBUAAABIiYnJjQ3NzY2MzIWFxYUBwcGIicmNDc3NjQnJiYjIgYHBwYUFxYUBwYGIwciJicmNDc3NjIXFhQHBwYUFxYWMzI2Nzc2NCcmNDc2MhcWFAcHBgYjARQGDAUtLXoWOR8fORYtLTgKGwoKCjgaGg0gEhIgDXoaGgkJBQwHdR85Fi0tOAobCgoKOBoaDSASEiANehoaCQkKGwotLXoWOR8BMwUFLYEuehYXFxYugC44CQkKGwo4GkoaDQ0NDXoaShoKGwoFBe8XFi6ALjgJCQobCjgaShoNDQ0NehpKGgobCgoKLYEuehYXAAAADACWAAEAAAAAAAEACAAAAAEAAAAAAAIAAwAIAAEAAAAAAAMACAAAAAEAAAAAAAQACAAAAAEAAAAAAAUAAQALAAEAAAAAAAYACAAAAAMAAQQJAAEAEAAMAAMAAQQJAAIABgAcAAMAAQQJAAMAEAAMAAMAAQQJAAQAEAAMAAMAAQQJAAUAAgAiAAMAAQQJAAYAEAAMYW5jaG9yanM0MDBAAGEAbgBjAGgAbwByAGoAcwA0ADAAMABAAAAAAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAH//wAP) format(\"truetype\")" +
		"}"
)

// AnchorJS is the Go analog of the AnchorJS constructor object. It holds the
// mutable options map (mirroring this.options) and the ordered list of DOM
// elements that currently have anchors (mirroring this.elements).
type AnchorJS struct {
	// Options mirrors this.options: a live JS object. It is stored as a
	// js.Value object so that reads/writes and hasOwnProperty checks behave
	// exactly like the original (including a user replacing the whole object
	// via SetOptions).
	Options js.Value

	// elements mirrors this.elements: an ordered slice of DOM element
	// js.Values.
	elements []js.Value
}

// New constructs a new AnchorJS. options may be js.Null()/js.Undefined() to use
// an empty options object (mirroring `options || {}`), or a JS object.
func New(options js.Value) *AnchorJS {
	a := &AnchorJS{}
	if options.Type() == js.TypeObject {
		a.Options = options
	} else {
		a.Options = newObject()
	}
	a.elements = []js.Value{}
	a.applyRemainingDefaultOptions(a.Options)
	return a
}

// Elements returns the current ordered list of elements with anchors
// (mirroring the public this.elements property).
func (a *AnchorJS) Elements() []js.Value {
	return a.elements
}

// hasOwn reports whether obj has its own property named key (Object.prototype.hasOwnProperty).
func hasOwn(obj js.Value, key string) bool {
	return js.Global().Get("Object").Get("prototype").Get("hasOwnProperty").
		Call("call", obj, key).Bool()
}

func newObject() js.Value {
	return js.Global().Get("Object").New()
}

func document() js.Value {
	return js.Global().Get("document")
}

func window() js.Value {
	return js.Global().Get("window")
}

// applyRemainingDefaultOptions mirrors _applyRemainingDefaultOptions. It fills
// only missing keys, using hasOwnProperty exactly like the original.
func (a *AnchorJS) applyRemainingDefaultOptions(opts js.Value) {
	if !hasOwn(opts, "icon") {
		opts.Set("icon", defaultIcon)
	}
	if !hasOwn(opts, "visible") {
		opts.Set("visible", "hover")
	}
	if !hasOwn(opts, "placement") {
		opts.Set("placement", "right")
	}
	if !hasOwn(opts, "ariaLabel") {
		opts.Set("ariaLabel", "Anchor")
	}
	if !hasOwn(opts, "class") {
		opts.Set("class", "")
	}
	if !hasOwn(opts, "base") {
		opts.Set("base", "")
	}
	if hasOwn(opts, "truncate") {
		// Math.floor(Number(opts.truncate)); Number-cast + integer floor.
		opts.Set("truncate", mathFloorNumber(opts.Get("truncate")))
	} else {
		opts.Set("truncate", 64)
	}
	if !hasOwn(opts, "titleText") {
		opts.Set("titleText", "")
	}
}

// mathFloorNumber reproduces JS Math.floor(value) with the same Number coercion
// semantics for the values AnchorJS supports (numbers and numeric strings).
func mathFloorNumber(v js.Value) int {
	// Use the JS runtime to match coercion exactly, including strings like "87"
	// and floats like 87.9999999999, plus NaN behavior.
	return js.Global().Get("Math").Call("floor", v).Int()
}

// SetOptions replaces the whole options object (mirrors `anchors.options = {...}`).
func (a *AnchorJS) SetOptions(options js.Value) {
	if options.Type() == js.TypeObject {
		a.Options = options
	} else {
		a.Options = newObject()
	}
}

// Add adds anchor links to page elements. selector may be a string (CSS
// selector), or a JS array / NodeList of elements. Returns the receiver for
// chaining. Mirrors this.add.
//
// It returns ErrInvalidSelector (instead of panicking) when the selector is
// neither a string, array, nor NodeList, mirroring the original's thrown
// TypeError while keeping the WASM runtime alive (a panic inside a js.FuncOf
// callback crashes the whole Go program).
func (a *AnchorJS) Add(selector js.Value) (*AnchorJS, error) {
	var indexesToDrop []int

	// Reapply options here because somebody may have overwritten the default
	// options object when setting options.
	a.applyRemainingDefaultOptions(a.Options)

	// Provide a sensible default selector, if none is given (falsy selector).
	if isFalsy(selector) {
		selector = js.ValueOf(defaultSelector)
	}

	elements, err := a.getElements(selector)
	if err != nil {
		return a, err
	}

	if len(elements) == 0 {
		return a, nil
	}

	a.addBaselineStyles()

	// Produce a list of existing IDs so we don't generate a duplicate.
	elsWithIds := document().Call("querySelectorAll", "[id]")
	idList := []string{}
	n := elsWithIds.Get("length").Int()
	for i := 0; i < n; i++ {
		idList = append(idList, elsWithIds.Index(i).Get("id").String())
	}

	for i := 0; i < len(elements); i++ {
		el := elements[i]
		if a.HasAnchorJSLink(el) {
			indexesToDrop = append(indexesToDrop, i)
			continue
		}

		var elementID string
		if el.Call("hasAttribute", "id").Bool() {
			elementID = el.Call("getAttribute", "id").String()
		} else if el.Call("hasAttribute", "data-anchor-id").Bool() {
			elementID = el.Call("getAttribute", "data-anchor-id").String()
		} else {
			tidyText := a.Urlify(el.Get("textContent").String())

			// Compare our generated ID to existing IDs (and increment it if
			// needed) before we add it to the page.
			//
			// Faithful port of the do/while with a sentinel "undefined" index:
			// the first iteration uses tidyText itself (no "-0" suffix), and
			// only on a collision does it start appending "-count".
			newTidyText := tidyText
			count := 0
			indexUndefined := true
			var index int
			for {
				if !indexUndefined {
					newTidyText = tidyText + "-" + strconv.Itoa(count)
				}
				index = indexOf(idList, newTidyText)
				indexUndefined = false
				count++
				if index == -1 {
					break
				}
			}

			idList = append(idList, newTidyText)
			el.Call("setAttribute", "id", newTidyText)
			elementID = newTidyText
		}

		anchor := document().Call("createElement", "a")
		anchor.Set("className", "anchorjs-link "+a.Options.Get("class").String())
		anchor.Call("setAttribute", "aria-label", a.Options.Get("ariaLabel"))
		anchor.Call("setAttribute", "data-anchorjs-icon", a.Options.Get("icon"))
		if isTruthy(a.Options.Get("titleText")) {
			anchor.Set("title", a.Options.Get("titleText"))
		}

		// Adjust the href if there's a <base> tag.
		var hrefBase string
		if !document().Call("querySelector", "base").IsNull() {
			loc := window().Get("location")
			hrefBase = loc.Get("pathname").String() + loc.Get("search").String()
		} else {
			hrefBase = ""
		}
		if base := a.Options.Get("base"); isTruthy(base) {
			hrefBase = base.String()
		}
		anchor.Set("href", hrefBase+"#"+elementID)

		if a.Options.Get("visible").String() == "always" {
			anchor.Get("style").Set("opacity", "1")
		}

		if a.Options.Get("icon").String() == defaultIcon {
			anchor.Get("style").Set("font", "1em/1 anchorjs-icons")

			if a.Options.Get("placement").String() == "left" {
				anchor.Get("style").Set("lineHeight", "inherit")
			}
		}

		if a.Options.Get("placement").String() == "left" {
			style := anchor.Get("style")
			style.Set("position", "absolute")
			style.Set("marginLeft", "-1.25em")
			style.Set("paddingRight", ".25em")
			style.Set("paddingLeft", ".25em")
			el.Call("insertBefore", anchor, el.Get("firstChild"))
		} else { // right (or anything else)
			style := anchor.Get("style")
			style.Set("marginLeft", ".1875em")
			style.Set("paddingRight", ".1875em")
			style.Set("paddingLeft", ".1875em")
			el.Call("appendChild", anchor)
		}
	}

	// Remove dropped indexes (elements that already had anchors).
	for i := 0; i < len(indexesToDrop); i++ {
		removeAt(&elements, indexesToDrop[i]-i)
	}

	a.elements = append(a.elements, elements...)

	return a, nil
}

// Remove removes all anchorjs-links from elements targeted by the selector.
// Mirrors this.remove. It returns ErrInvalidSelector on an invalid selector
// type instead of panicking, for the same WASM-runtime-safety reason as Add.
func (a *AnchorJS) Remove(selector js.Value) (*AnchorJS, error) {
	elements, err := a.getElements(selector)
	if err != nil {
		return a, err
	}

	for i := 0; i < len(elements); i++ {
		domAnchor := elements[i].Call("querySelector", ".anchorjs-link")
		if isTruthy(domAnchor) {
			// Drop the element from our main list, if it's in there.
			index := indexOfValue(a.elements, elements[i])
			if index != -1 {
				a.elements = append(a.elements[:index], a.elements[index+1:]...)
			}
			// Remove the anchor from the DOM.
			elements[i].Call("removeChild", domAnchor)
		}
	}

	return a, nil
}

// RemoveAll removes all anchorjs links. Mirrors this.removeAll.
func (a *AnchorJS) RemoveAll() {
	arr := js.Global().Get("Array").New()
	for _, el := range a.elements {
		arr.Call("push", el)
	}
	a.Remove(arr)
}

// Urlify refines text so it makes a good ID. Mirrors this.urlify exactly,
// including HTML-entity decoding via a textarea and the ordered transform
// pipeline.
func (a *AnchorJS) Urlify(text string) string {
	// Decode HTML characters such as '&nbsp;' first, using a textarea (the
	// browser's HTML parser) exactly like the original.
	textarea := document().Call("createElement", "textarea")
	textarea.Set("innerHTML", text)
	text = textarea.Get("value").String()

	// The reason we include this applyRemainingDefaultOptions is so urlify can
	// be called independently, even after setting options.
	if !isTruthy(a.Options.Get("truncate")) {
		a.applyRemainingDefaultOptions(a.Options)
	}

	// truncate may be a number OR a numeric string (e.g. options.truncate='87'
	// set directly before a standalone urlify call). JS .substring(0, truncate)
	// coerces via ToInteger, so we mirror Math.floor(Number(truncate)) here
	// rather than calling .Int() (which panics on a string js.Value).
	truncate := mathFloorNumber(a.Options.Get("truncate"))

	// Pipeline (order matters):
	//   trim -> remove apostrophes -> replace non-safe chars with '-' ->
	//   collapse repeated hyphens -> substring(0, truncate) ->
	//   trim leading/trailing hyphens -> toLowerCase.
	return slug.Pipeline(text, truncate)
}

// HasAnchorJSLink determines if this element already has an AnchorJS link on it.
// Mirrors this.hasAnchorJSLink.
func (a *AnchorJS) HasAnchorJSLink(el js.Value) bool {
	first := el.Get("firstChild")
	last := el.Get("lastChild")

	hasLeftAnchor := isTruthy(first) &&
		strings.Index(" "+classNameOf(first)+" ", " anchorjs-link ") > -1
	hasRightAnchor := isTruthy(last) &&
		strings.Index(" "+classNameOf(last)+" ", " anchorjs-link ") > -1

	return hasLeftAnchor || hasRightAnchor
}

// classNameOf mirrors reading el.className, where a text node yields
// `undefined` in JS -> string "undefined" after concatenation. To match the
// original's string concatenation semantics, missing/undefined className
// becomes the literal "undefined".
func classNameOf(node js.Value) string {
	cn := node.Get("className")
	switch cn.Type() {
	case js.TypeString:
		return cn.String()
	case js.TypeUndefined, js.TypeNull:
		return "undefined"
	default:
		return cn.String()
	}
}

// ErrInvalidSelector is returned for a selector that is not a string, Array, or
// NodeList. It must NOT be a panic: an unrecovered panic in a js.FuncOf callback
// crashes the whole Go/WASM runtime. The boundary layer converts it to a thrown
// JS TypeError carrying InvalidSelectorMessage, matching the original behavior.
type ErrInvalidSelector struct{}

func (ErrInvalidSelector) Error() string { return InvalidSelectorMessage }

// getElements mirrors _getElements; returns ErrInvalidSelector on bad input.
func (a *AnchorJS) getElements(input js.Value) ([]js.Value, error) {
	switch {
	case input.Type() == js.TypeString:
		nodeList := document().Call("querySelectorAll", input.String())
		return valueSlice(nodeList), nil
	case isArray(input) || isNodeList(input):
		return valueSlice(input), nil
	default:
		return nil, ErrInvalidSelector{}
	}
}

// addBaselineStyles adds baseline styles to the page (idempotent). Mirrors
// _addBaselineStyles, including insertion order and placement.
func (a *AnchorJS) addBaselineStyles() {
	head := document().Get("head")
	if !head.Call("querySelector", "style.anchorjs").IsNull() {
		return
	}

	style := document().Call("createElement", "style")
	style.Set("className", "anchorjs")
	style.Call("appendChild", document().Call("createTextNode", "")) // Necessary for Webkit.

	// Insert before the first stylesheet/style if present, else append.
	firstStyleEl := head.Call("querySelector", `[rel="stylesheet"],style`)
	// Original compares `=== undefined`; querySelector returns null when not
	// found, so the JS code actually always takes the insertBefore branch with
	// a null reference node, which the DOM treats as appendChild. We reproduce
	// that exact behavior.
	head.Call("insertBefore", style, firstStyleEl)

	sheet := style.Get("sheet")
	sheet.Call("insertRule", linkRule, sheet.Get("cssRules").Get("length"))
	sheet.Call("insertRule", hoverRule, sheet.Get("cssRules").Get("length"))
	sheet.Call("insertRule", pseudoElContent, sheet.Get("cssRules").Get("length"))
	sheet.Call("insertRule", anchorjsLinkFontFace, sheet.Get("cssRules").Get("length"))
}

// ---- helpers ----

func isFalsy(v js.Value) bool {
	switch v.Type() {
	case js.TypeUndefined, js.TypeNull:
		return true
	case js.TypeBoolean:
		return !v.Bool()
	case js.TypeNumber:
		f := v.Float()
		return f == 0 || math.IsNaN(f)
	case js.TypeString:
		return v.String() == ""
	default:
		return false
	}
}

func isTruthy(v js.Value) bool { return !isFalsy(v) }

func isArray(v js.Value) bool {
	return js.Global().Get("Array").Call("isArray", v).Bool()
}

func isNodeList(v js.Value) bool {
	nl := js.Global().Get("NodeList")
	if nl.IsUndefined() {
		return false
	}
	return v.InstanceOf(nl)
}

// valueSlice mirrors [].slice.call(x): copies the array-like's indexed entries.
func valueSlice(arrayLike js.Value) []js.Value {
	out := []js.Value{}
	n := arrayLike.Get("length").Int()
	for i := 0; i < n; i++ {
		out = append(out, arrayLike.Index(i))
	}
	return out
}

func indexOf(list []string, target string) int {
	for i, s := range list {
		if s == target {
			return i
		}
	}
	return -1
}

func indexOfValue(list []js.Value, target js.Value) int {
	for i, v := range list {
		if v.Equal(target) {
			return i
		}
	}
	return -1
}

func removeAt(list *[]js.Value, i int) {
	if i < 0 || i >= len(*list) {
		return
	}
	*list = append((*list)[:i], (*list)[i+1:]...)
}
