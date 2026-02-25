package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// serveHTML starts an HTTP server that serves the HTML file and auto-reloads
// the browser when the file changes on disk. It blocks forever.
func serveHTML(htmlFile string, port int) {
	absPath, err := filepath.Abs(htmlFile)
	if err != nil {
		fatal("Failed to resolve path: %v", err)
	}

	watcher := &fileWatcher{path: absPath}
	go watcher.watch()

	mux := http.NewServeMux()

	// Serve the HTML file at /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			http.Error(w, "Failed to read file", 500)
			return
		}
		// Inject live-reload script before </body>
		reload := []byte(`<script>
const es = new EventSource("/__reload");
es.onmessage = () => location.reload();
es.onerror = () => setTimeout(() => location.reload(), 2000);
</script></body>`)
		injected := replaceBytes(content, []byte("</body>"), reload)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(injected)
	})

	// SSE endpoint for live reload
	mux.HandleFunc("/__reload", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher.Flush()

		ch := watcher.subscribe()
		defer watcher.unsubscribe(ch)

		for {
			select {
			case <-ch:
				fmt.Fprintf(w, "data: reload\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	addr := fmt.Sprintf(":%d", port)

	// Bind the port first so it's listening before we try to open it in Gitpod
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("Failed to listen on %s: %v", addr, err)
	}

	fmt.Fprintf(os.Stderr, "Serving %s at http://localhost%s\n", htmlFile, addr)

	// Try to open the port in Gitpod and print the public URL
	openGitpodPort(port)

	if err := http.Serve(ln, mux); err != nil {
		fatal("Server error: %v", err)
	}
}

// openGitpodPort attempts to open the port via the Gitpod CLI and prints the
// public URL. Silently does nothing if not in a Gitpod environment.
func openGitpodPort(port int) {
	gitpodBin, err := exec.LookPath("gitpod")
	if err != nil {
		return // not in a Gitpod environment
	}

	portStr := fmt.Sprintf("%d", port)
	cmd := exec.Command(gitpodBin, "environment", "port", "open",
		"--name", "throughput", portStr)
	out, err := cmd.Output()
	if err != nil {
		return // port open failed, fall back to localhost
	}

	url := strings.TrimSpace(string(out))
	if url != "" {
		fmt.Fprintf(os.Stderr, "\n  Open in browser: %s\n\n", url)
	}
}

func replaceBytes(s, old, new []byte) []byte {
	for i := 0; i <= len(s)-len(old); i++ {
		match := true
		for j := range old {
			if s[i+j] != old[j] {
				match = false
				break
			}
		}
		if match {
			result := make([]byte, 0, len(s)-len(old)+len(new))
			result = append(result, s[:i]...)
			result = append(result, new...)
			result = append(result, s[i+len(old):]...)
			return result
		}
	}
	return s
}

// hashFile returns a simple FNV-1a hash of the file contents, or 0 on error.
func hashFile(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	// FNV-1a
	var h uint64 = 14695981039346656037
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

// fileWatcher polls a file for modification time changes and notifies subscribers.
type fileWatcher struct {
	path    string
	mu      sync.Mutex
	clients []chan struct{}
}

func (fw *fileWatcher) subscribe() chan struct{} {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	ch := make(chan struct{}, 1)
	fw.clients = append(fw.clients, ch)
	return ch
}

func (fw *fileWatcher) unsubscribe(ch chan struct{}) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	for i, c := range fw.clients {
		if c == ch {
			fw.clients = append(fw.clients[:i], fw.clients[i+1:]...)
			break
		}
	}
}

func (fw *fileWatcher) notify() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	for _, ch := range fw.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (fw *fileWatcher) watch() {
	var lastMod time.Time
	var lastSize int64
	var lastHash uint64
	for {
		info, err := os.Stat(fw.path)
		if err == nil {
			mod := info.ModTime()
			size := info.Size()
			changed := false
			if !lastMod.IsZero() && (mod.After(lastMod) || size != lastSize) {
				changed = true
			}
			// If modtime and size match, check content hash to catch
			// overwrites within the same filesystem timestamp second.
			if !changed && !lastMod.IsZero() {
				if h := hashFile(fw.path); h != 0 && h != lastHash {
					changed = true
				}
			}
			if changed {
				fmt.Fprintf(os.Stderr, "File changed, reloading browsers...\n")
				fw.notify()
			}
			lastMod = mod
			lastSize = size
			if h := hashFile(fw.path); h != 0 {
				lastHash = h
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}
