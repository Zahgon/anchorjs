# AnchorJS (Go/WebAssembly)

AnchorJS adds deep anchor links (the little clickable link that appears next to
headers when you hover over them) to existing page content.

This repository is a **faithful Go migration** of the original
[AnchorJS](https://www.bryanbraun.com/anchorjs/) v5.0.0 JavaScript library. The
implementation is written in Go and compiled to **WebAssembly (WASM)**, where it
manipulates the real browser DOM through `syscall/js`. It preserves 100% of the
original library's observable browser behavior: the same public API, the same
generated anchor elements, IDs, hrefs, ARIA attributes, CSS classes, baseline
styles, options, and slug/ID generation.

> Historical note: the upstream project was a browser JavaScript library. This
> project is **not** a JavaScript source project — the library is implemented in
> Go. The only JavaScript present is generated WebAssembly runtime glue
> (`wasm_exec.js`, produced by the Go toolchain), a generated license banner,
> and browser-side test tooling.

## Requirements

- **Go 1.23+**
- A **WebAssembly target** (`GOOS=js GOARCH=wasm`) for building the library.
- A modern browser (or headless Chrome) to run the compiled WASM, since the
  library operates on the browser DOM.
- **No runtime dependencies.** The shipped library uses only the Go standard
  library (`syscall/js`).

## Project layout

| Path | Description |
| --- | --- |
| `anchorjs/anchorjs.go` | Core library (`//go:build js && wasm`). The AnchorJS type and DOM logic. |
| `anchorjs/internal/slug/` | Build-tag-neutral, DOM-independent slug/urlify string pipeline (natively unit-tested). |
| `cmd/anchorjs-wasm/` | WASM entry point. Installs the global `AnchorJS` constructor and a default `anchors` instance. |
| `build/` | Go-native build tool (mirrors the original `uglify` → `add-banner` → `copy-to-docs` npm workflow). |
| `dist/` | Generated distribution output: `anchorjs.wasm`, `wasm_exec.js` loader, and `anchorjs.license.js` banner. |
| `docs/` | Published copy of the generated distribution artifacts. |
| `test/` | Browser-boundary test harness + runner, plus the verbatim upstream Jasmine spec suite. |

## Build

The build tool reproduces the original distribution workflow:

```sh
go run ./build
```

This:

1. Compiles the library to `dist/anchorjs.wasm` (`GOOS=js GOARCH=wasm go build ./cmd/anchorjs-wasm`).
2. Publishes the Go toolchain's `wasm_exec.js` loader into `dist/`.
3. Writes the license banner to `dist/anchorjs.license.js`.
4. Copies the generated artifacts into `docs/`.

## Test

The migration is verified at the same behavioral boundaries as the original.

Native (DOM-independent slug/urlify pipeline and build banner):

```sh
go test ./anchorjs/internal/slug/
go test ./build/
```

Browser boundary (the full upstream 39-spec Jasmine suite, run against the
compiled WASM in headless Chrome — the same boundary the original Karma/Jasmine
suite used):

```sh
go test ./test/ -run TestAnchorJSBrowserSpecs
```

The browser runner serves the project, launches headless Chrome, drives it over
the DevTools Protocol, and asserts that all 39 specs pass. Set `CHROME_BIN` to
override Chrome/Edge discovery if needed.

## Usage

Load the WASM runtime glue and instantiate the compiled module in the browser.
Instantiating installs a global `AnchorJS` constructor and a default `anchors`
instance, exactly like the original library's browser build:

```html
<script src="anchorjs/dist/wasm_exec.js"></script>
<script>
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("anchorjs/dist/anchorjs.wasm"), go.importObject)
    .then((result) => {
      go.run(result.instance);
      // The default instance is now available:
      anchors.add();
      // Or create your own:
      // new AnchorJS().add();
    });
</script>
```

## Public API

Mirrors the original AnchorJS API.

### Constructor

`new AnchorJS(options)` — creates an instance, filling in any unspecified
options with the defaults below.

### Methods

- `add(selector)` — adds anchor links to elements matching `selector` (a CSS
  selector string, a `NodeList`, or an array of elements). Defaults to
  `'h2, h3, h4, h5, h6'` when no selector is given. Chainable. Throws a
  `TypeError` (`"The selector provided to AnchorJS was invalid."`) for an
  invalid selector.
- `remove(selector)` — removes anchor links from matching elements. Chainable.
- `removeAll()` — removes all anchor links this instance created.
- `urlify(text)` — converts text into a URL-friendly ID slug.
- `hasAnchorJSLink(element)` — returns whether an element already has an
  AnchorJS link.
- `elements` — the list of elements this instance has added anchors to.

### Options

| Option | Default | Notes |
| --- | --- | --- |
| `icon` | `'\uE9CB'` | The icon character. Custom/unicode icons supported. |
| `visible` | `'hover'` | `'hover'` or `'always'`. Legacy `'touch'` behaves like `'hover'`. |
| `placement` | `'right'` | `'right'` or `'left'`. |
| `ariaLabel` | `'Anchor'` | The anchor's `aria-label`. |
| `class` | `''` | Extra class(es) added to the anchor. |
| `base` | `''` | Custom base for generated hrefs. |
| `truncate` | `64` | Max ID length (in UTF-16 code units). |
| `titleText` | `''` | Optional `title` (tooltip) text. |

## Distribution

`go run ./build` produces the distributable artifacts in `dist/` (and copies
them to `docs/`):

- `anchorjs.wasm` — the compiled library.
- `wasm_exec.js` — the Go WebAssembly runtime loader.
- `anchorjs.license.js` — the license banner.

## License

MIT. Copyright (c) Bryan Braun.
