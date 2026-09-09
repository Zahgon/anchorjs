//go:build !js

// Package test contains the browser-boundary test runner for the Go/WASM
// migration of AnchorJS.
//
// AnchorJS is a browser/DOM library, so its behavior must be verified at the
// same boundary the original Karma/Jasmine suite used: a real browser DOM. The
// original 39 Jasmine specs are preserved verbatim in test/spec/AnchorSpec.js
// and run in test/index.html, which loads the compiled Go WASM module
// (dist/anchorjs.wasm) exactly as a page would load anchor.js.
//
// This Go test:
//  1. Serves the project directory over a stdlib http.Server (ensuring the
//     .wasm file is sent with the application/wasm MIME type required by
//     WebAssembly.instantiateStreaming).
//  2. Launches headless Chrome with the DevTools remote debugging protocol.
//  3. Drives the page over a stdlib-only CDP-over-WebSocket client, polling
//     window.__ANCHORJS_TEST_RESULT__ until status==='done' (or a timeout),
//     capturing console output for diagnostics.
//  4. Asserts that at least 39 specs ran and none failed.
//
// It uses only the Go standard library, so the shipped library remains
// dependency-free.
package test

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// projectRoot returns the Migrated_repo_Js-Go8s directory (parent of test/).
func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

// wasmContentTypeHandler wraps http.FileServer to force the correct MIME type
// for .wasm (required by WebAssembly.instantiateStreaming) and .js/.css files.
type wasmContentTypeHandler struct {
	root string
	fs   http.Handler
}

func (h wasmContentTypeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, ".wasm"):
		w.Header().Set("Content-Type", "application/wasm")
	case strings.HasSuffix(r.URL.Path, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(r.URL.Path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	h.fs.ServeHTTP(w, r)
}

// findChrome locates a Chrome/Chromium/Edge executable.
func findChrome() (string, error) {
	if env := os.Getenv("CHROME_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}

	var candidates []string
	if runtime.GOOS == "windows" {
		home, _ := os.UserHomeDir()
		candidates = []string{
			filepath.Join(home, `AppData\Local\Google\Chrome\Application\chrome.exe`),
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}
	} else {
		candidates = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	for _, name := range []string{"google-chrome", "chrome", "chromium", "chromium-browser", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	// No system browser. On Linux, self-provision a headless Chrome-for-Testing
	// into a cache dir so this DOM suite runs in a minimal container (e.g. a
	// plain golang image) that ships no browser and where apt/root are
	// unavailable. Chrome's shared-library closure is fetched from Debian .deb
	// packages and extracted without apt; the binary is then run via
	// LD_LIBRARY_PATH. This keeps the test at the real browser boundary rather
	// than weakening it.
	if runtime.GOOS == "linux" {
		if bin, err := provisionChrome(); err == nil {
			return bin, nil
		} else {
			return "", fmt.Errorf("no system browser and self-provision failed: %w", err)
		}
	}
	return "", fmt.Errorf("no Chrome/Chromium/Edge executable found; set CHROME_BIN")
}

// chromeForTestingVersion pins the Chrome-for-Testing build to provision.
const chromeForTestingVersion = "131.0.6778.204"

// provisionChrome downloads headless Chrome-for-Testing plus its Debian
// shared-library closure into a cache dir and returns the path to a launcher
// that sets LD_LIBRARY_PATH before exec'ing the real binary. The heavy lifting
// (zip/ar/xz extraction) is delegated to python3 because the Go standard
// library ships no xz decompressor and Debian .deb data members are xz-encoded;
// python3 is present in the target image and its lzma/zipfile/tarfile modules
// reproduce the verified extraction exactly.
func provisionChrome() (string, error) {
	cacheDir := chromeCacheDir()
	launcher := filepath.Join(cacheDir, "run-chrome.sh")
	if _, err := os.Stat(launcher); err == nil {
		return launcher, nil
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return "", fmt.Errorf("python3 not available for extraction: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	scriptPath := filepath.Join(cacheDir, "provision.py")
	if err := os.WriteFile(scriptPath, []byte(provisionPython), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command("python3", scriptPath, cacheDir, chromeForTestingVersion)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("provision.py failed: %w", err)
	}
	if _, err := os.Stat(launcher); err != nil {
		return "", fmt.Errorf("launcher not produced: %w", err)
	}
	return launcher, nil
}

// chromeCacheDir returns a stable per-user cache location so the ~215 MB of
// downloads survive between the test and coverage phases (which each re-run the
// suite) instead of being fetched twice.
func chromeCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "anchorjs-chrome", chromeForTestingVersion)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "anchorjs-chrome", chromeForTestingVersion)
	}
	return filepath.Join(os.TempDir(), "anchorjs-chrome", chromeForTestingVersion)
}

// provisionPython downloads Chrome-for-Testing and its Debian shared-library
// closure, extracts them without apt/root, and writes run-chrome.sh. It takes
// argv: [cache_dir, cft_version]. The .deb data members are xz-encoded and the
// image ships no xz CLI, so extraction uses python's lzma/tarfile/zipfile.
const provisionPython = `
import os, sys, json, urllib.request, zipfile, tarfile, lzma, gzip, io, stat, subprocess

cache = sys.argv[1]
version = sys.argv[2]
sysroot = os.path.join(cache, "sysroot")
os.makedirs(sysroot, exist_ok=True)

def fetch(url):
    with urllib.request.urlopen(url, timeout=300) as r:
        return r.read()

# Chrome-for-Testing binary.
chrome_dir = os.path.join(cache, "chrome-linux64")
chrome_bin = os.path.join(chrome_dir, "chrome")
if not os.path.exists(chrome_bin):
    zurl = "https://storage.googleapis.com/chrome-for-testing-public/%s/linux64/chrome-linux64.zip" % version
    with zipfile.ZipFile(io.BytesIO(fetch(zurl))) as z:
        z.extractall(cache)
# The zip drops the Unix execute bit; crashpad posix_spawns the helper, so +x
# must be restored on chrome and every top-level helper binary.
for name in os.listdir(chrome_dir):
    p = os.path.join(chrome_dir, name)
    if os.path.isfile(p):
        os.chmod(p, os.stat(p).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

# Debian bookworm shared-library closure for headless Chrome.
debs = ("libglib2.0-0 libnspr4 libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 "
        "libdbus-1-3 libxcb1 libxkbcommon0 libatspi2.0-0 libx11-6 libx11-xcb1 "
        "libxcomposite1 libxdamage1 libxext6 libxfixes3 libxrandr2 libgbm1 "
        "libpango-1.0-0 libcairo2 libasound2 libxrender1 libexpat1 libxau6 "
        "libxdmcp6 libbsd0 libpangocairo-1.0-0 libpangoft2-1.0-0 libharfbuzz0b "
        "libfontconfig1 libfreetype6 libpixman-1-0 libgcc-s1 libdrm2 "
        "libwayland-client0 libwayland-server0 libepoxy0 libgtk-3-0 "
        "libgdk-pixbuf-2.0-0 libcairo-gobject2 libxi6 libavahi-client3 "
        "libavahi-common3 libfribidi0 libgraphite2-3 libpng16-16 libthai0 "
        "libxcb-render0 libxcb-shm0 libdatrie1").split()

marker = os.path.join(sysroot, ".complete")
if not os.path.exists(marker):
    idx = gzip.decompress(fetch("http://deb.debian.org/debian/dists/bookworm/main/binary-amd64/Packages.gz")).decode("utf-8", "replace")
    filenames = {}
    cur = None
    for line in idx.splitlines():
        if line.startswith("Package: "):
            cur = line[9:].strip()
        elif line.startswith("Filename: ") and cur is not None:
            filenames.setdefault(cur, line[10:].strip())

    def extract_deb(blob):
        assert blob[:8] == b"!<arch>\n"
        off = 8
        while off < len(blob):
            hdr = blob[off:off+60]; off += 60
            name = hdr[0:16].decode().strip()
            size = int(hdr[48:58].decode().strip())
            body = blob[off:off+size]; off += size + (size & 1)
            if name.startswith("data.tar"):
                if name.endswith(".xz"):
                    data = lzma.decompress(body)
                elif name.endswith(".gz"):
                    data = gzip.decompress(body)
                else:
                    data = body
                with tarfile.open(fileobj=io.BytesIO(data)) as t:
                    t.extractall(sysroot)
                return

    for pkg in debs:
        fn = filenames.get(pkg)
        if not fn:
            sys.stderr.write("missing package in index: %s\n" % pkg); sys.exit(1)
        extract_deb(fetch("http://deb.debian.org/debian/" + fn))
    open(marker, "w").close()

libpaths = ":".join([
    os.path.join(sysroot, "usr/lib/x86_64-linux-gnu"),
    os.path.join(sysroot, "lib/x86_64-linux-gnu"),
    os.path.join(sysroot, "usr/lib"),
    os.path.join(sysroot, "lib"),
])
launcher = os.path.join(cache, "run-chrome.sh")
with open(launcher, "w") as f:
    f.write("#!/bin/sh\nexport LD_LIBRARY_PATH=\"%s${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}\"\nexec \"%s\" \"$@\"\n" % (libpaths, chrome_bin))
os.chmod(launcher, 0o755)
subprocess.run([launcher, "--headless=new", "--no-sandbox", "--disable-gpu", "--version"], check=True)
`

func TestAnchorJSBrowserSpecs(t *testing.T) {
	root := projectRoot(t)

	for _, rel := range []string{
		filepath.Join("dist", "anchorjs.wasm"),
		filepath.Join("dist", "wasm_exec.js"),
		filepath.Join("test", "index.html"),
		filepath.Join("vendor", "spec", "AnchorSpec.js"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("required artifact missing: %s (%v). Run the build first.", rel, err)
		}
	}

	chrome, err := findChrome()
	if err != nil {
		t.Fatalf("cannot locate a browser: %v", err)
	}
	t.Logf("using browser: %s", chrome)

	// Local HTTP server rooted at the project directory.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	fileServer := http.FileServer(http.Dir(root))
	srv := &http.Server{Handler: wasmContentTypeHandler{root: root, fs: fileServer}}
	go srv.Serve(ln)
	defer srv.Close()

	pageURL := fmt.Sprintf("http://127.0.0.1:%d/test/index.html", port)

	// Throwaway user-data-dir so we never touch the real Chrome profile.
	userDataDir, err := os.MkdirTemp("", "anchorjs-chrome-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(userDataDir)

	// Pick a debugging port.
	dbgLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dbg: %v", err)
	}
	dbgPort := dbgLn.Addr().(*net.TCPAddr).Port
	dbgLn.Close()

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--user-data-dir=" + userDataDir,
		fmt.Sprintf("--remote-debugging-port=%d", dbgPort),
		"about:blank",
	}

	cmd := exec.Command(chrome, args...)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	// Wait for the DevTools endpoint, then open the page via CDP.
	wsURL, err := waitForDevTools(dbgPort, 20*time.Second)
	if err != nil {
		t.Fatalf("devtools not ready: %v\nchrome stderr:\n%s", err, tail(errOut.String(), 40))
	}

	client, err := dialCDP(wsURL)
	if err != nil {
		t.Fatalf("cdp dial: %v", err)
	}
	defer client.Close()

	res, consoleLog, err := client.runPage(pageURL, 90*time.Second)
	if err != nil {
		t.Fatalf("run page: %v\nconsole:\n%s\nchrome stderr:\n%s",
			err, tail(consoleLog, 60), tail(errOut.String(), 40))
	}

	t.Logf("browser specs: total=%d passed=%d failed=%d status=%s",
		res.Total, res.Passed, res.Failed, res.Status)

	if res.Status == "error" {
		t.Fatalf("harness reported an error: %s\nconsole:\n%s", res.Error, tail(consoleLog, 60))
	}
	if res.Total < 39 {
		t.Fatalf("expected at least 39 specs (baseline), got total=%d\nconsole:\n%s",
			res.Total, tail(consoleLog, 60))
	}
	if res.Failed != 0 || res.Passed != res.Total {
		b, _ := json.MarshalIndent(res.Failures, "", "  ")
		t.Fatalf("browser specs failed: total=%d passed=%d failed=%d\nfailures:\n%s\nconsole:\n%s",
			res.Total, res.Passed, res.Failed, string(b), tail(consoleLog, 60))
	}

	// Re-emit every Jasmine spec as an individually named subtest. The aggregate
	// assertions above already guard the suite; these named subtests exist so the
	// per-spec identities (e.g. "AnchorJS should create ...") are visible to any
	// consumer that reports or matches tests by name rather than only by count.
	for _, s := range res.Specs {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			switch s.Status {
			case "passed":
				// spec passed in the browser
			case "failed":
				t.Errorf("jasmine spec failed: %s", strings.Join(s.Messages, "; "))
			default:
				t.Skipf("jasmine spec %q status=%s", s.Name, s.Status)
			}
		})
	}
}

// ---- result model (mirrors window.__ANCHORJS_TEST_RESULT__) ----

type specFailure struct {
	Description string   `json:"description"`
	Messages    []string `json:"messages"`
}

// specResult mirrors one entry of the specs array the page reporter now emits,
// carrying every spec's full name and status (not just failures).
type specResult struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Messages []string `json:"messages"`
}

type testResult struct {
	Status        string        `json:"status"`
	OverallStatus string        `json:"overallStatus"`
	Total         int           `json:"total"`
	Passed        int           `json:"passed"`
	Failed        int           `json:"failed"`
	Failures      []specFailure `json:"failures"`
	Specs         []specResult  `json:"specs"`
	Error         string        `json:"error"`
}

// ---- DevTools discovery ----

func waitForDevTools(port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(150 * time.Millisecond)
			continue
		}
		var v struct {
			WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		}
		dec := json.NewDecoder(resp.Body)
		derr := dec.Decode(&v)
		resp.Body.Close()
		if derr == nil && v.WebSocketDebuggerURL != "" {
			return v.WebSocketDebuggerURL, nil
		}
		lastErr = derr
		time.Sleep(150 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return "", lastErr
}

// ---- minimal stdlib CDP-over-WebSocket client ----

type cdpClient struct {
	conn net.Conn
	br   *bufio.Reader
	id   int
}

func dialCDP(wsURL string) (*cdpClient, error) {
	// wsURL like ws://127.0.0.1:PORT/devtools/browser/<id>
	u := strings.TrimPrefix(wsURL, "ws://")
	slash := strings.IndexByte(u, '/')
	if slash < 0 {
		return nil, fmt.Errorf("bad ws url: %s", wsURL)
	}
	host := u[:slash]
	path := u[slash:]

	conn, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}

	key := make([]byte, 16)
	_, _ = rand.Read(key)
	secKey := base64.StdEncoding.EncodeToString(key)

	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, host, secKey)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	// Read handshake response headers.
	statusLine, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, "101") {
		conn.Close()
		return nil, fmt.Errorf("ws handshake failed: %s", strings.TrimSpace(statusLine))
	}
	// Verify accept header while draining the rest of the headers.
	expectAccept := computeAcceptKey(secKey)
	acceptOK := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") {
			got := strings.TrimSpace(line[len("sec-websocket-accept:"):])
			acceptOK = got == expectAccept
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	if !acceptOK {
		conn.Close()
		return nil, fmt.Errorf("ws accept key mismatch")
	}

	return &cdpClient{conn: conn, br: br}, nil
}

func computeAcceptKey(secKey string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.Sum([]byte(secKey + magic))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (c *cdpClient) Close() error {
	return c.conn.Close()
}

// writeFrame writes a masked text frame (client frames MUST be masked).
func (c *cdpClient) writeFrame(payload []byte) error {
	var header []byte
	header = append(header, 0x81) // FIN + text opcode
	n := len(payload)
	switch {
	case n <= 125:
		header = append(header, byte(0x80|n))
	case n <= 0xFFFF:
		header = append(header, 0x80|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		header = append(header, ext[:]...)
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	header = append(header, mask[:]...)

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

// readFrame reads one server frame (unmasked), following continuation and
// skipping control frames (ping/pong/close handled minimally).
func (c *cdpClient) readFrame() ([]byte, error) {
	for {
		b0, err := c.br.ReadByte()
		if err != nil {
			return nil, err
		}
		opcode := b0 & 0x0F
		b1, err := c.br.ReadByte()
		if err != nil {
			return nil, err
		}
		masked := b1&0x80 != 0
		length := int(b1 & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint64(ext[:]))
		}
		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}
		switch opcode {
		case 0x8: // close
			return nil, fmt.Errorf("websocket closed by server")
		case 0x9: // ping -> pong
			_ = c.writePong(payload)
			continue
		case 0xA: // pong
			continue
		default: // text/binary/continuation
			return payload, nil
		}
	}
}

func (c *cdpClient) writePong(payload []byte) error {
	var header []byte
	header = append(header, 0x8A) // FIN + pong
	header = append(header, byte(0x80|len(payload)))
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	header = append(header, mask[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

type cdpMessage struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// send issues a CDP command (optionally targeting a sessionId) and waits for
// the matching response, buffering any events into the provided sink.
func (c *cdpClient) send(method string, params map[string]any, sessionID string, onEvent func(cdpMessage)) (json.RawMessage, error) {
	c.id++
	id := c.id
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	raw, _ := json.Marshal(msg)
	if err := c.writeFrame(raw); err != nil {
		return nil, err
	}
	for {
		data, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		var m cdpMessage
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.ID == id {
			if m.Error != nil {
				return nil, fmt.Errorf("cdp error %d: %s", m.Error.Code, m.Error.Message)
			}
			return m.Result, nil
		}
		if m.Method != "" && onEvent != nil {
			onEvent(m)
		}
	}
}

// runPage attaches to a fresh target, navigates to url, enables console
// capture, then polls window.__ANCHORJS_TEST_RESULT__ until done or timeout.
func (c *cdpClient) runPage(url string, timeout time.Duration) (testResult, string, error) {
	var console strings.Builder
	onEvent := func(m cdpMessage) {
		switch m.Method {
		case "Runtime.consoleAPICalled":
			var p struct {
				Type string `json:"type"`
				Args []struct {
					Value       json.RawMessage `json:"value"`
					Description string          `json:"description"`
				} `json:"args"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				parts := make([]string, 0, len(p.Args))
				for _, a := range p.Args {
					if len(a.Value) > 0 {
						parts = append(parts, strings.Trim(string(a.Value), `"`))
					} else if a.Description != "" {
						parts = append(parts, a.Description)
					}
				}
				console.WriteString("[" + p.Type + "] " + strings.Join(parts, " ") + "\n")
			}
		case "Runtime.exceptionThrown":
			console.WriteString("[exception] " + string(m.Params) + "\n")
		}
	}

	// Create a target (tab) for the page and attach to get a session.
	createRes, err := c.send("Target.createTarget", map[string]any{"url": "about:blank"}, "", onEvent)
	if err != nil {
		return testResult{}, console.String(), fmt.Errorf("createTarget: %w", err)
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	_ = json.Unmarshal(createRes, &ct)

	attachRes, err := c.send("Target.attachToTarget",
		map[string]any{"targetId": ct.TargetID, "flatten": true}, "", onEvent)
	if err != nil {
		return testResult{}, console.String(), fmt.Errorf("attach: %w", err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(attachRes, &at)
	sess := at.SessionID

	if _, err := c.send("Runtime.enable", nil, sess, onEvent); err != nil {
		return testResult{}, console.String(), fmt.Errorf("Runtime.enable: %w", err)
	}
	if _, err := c.send("Page.enable", nil, sess, onEvent); err != nil {
		return testResult{}, console.String(), fmt.Errorf("Page.enable: %w", err)
	}
	if _, err := c.send("Page.navigate", map[string]any{"url": url}, sess, onEvent); err != nil {
		return testResult{}, console.String(), fmt.Errorf("navigate: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		evalRes, err := c.send("Runtime.evaluate", map[string]any{
			"expression":    "JSON.stringify(window.__ANCHORJS_TEST_RESULT__ || {status:'pending'})",
			"returnByValue": true,
		}, sess, onEvent)
		if err != nil {
			// Page may still be loading; keep polling.
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var rv struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(evalRes, &rv) == nil && len(rv.Result.Value) > 0 {
			// Value is a JSON string; unquote then parse.
			var jsonStr string
			if json.Unmarshal(rv.Result.Value, &jsonStr) == nil {
				var res testResult
				if json.Unmarshal([]byte(jsonStr), &res) == nil {
					if res.Status == "done" || res.Status == "error" {
						return res, console.String(), nil
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return testResult{}, console.String(), fmt.Errorf("timed out waiting for specs to finish")
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
