package sitemap

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// failingWriter is a mock io.Writer for testing error paths.
type failingWriter struct {
	failOnNthWrite int
	writeCount     int
	buffer         bytes.Buffer
}

func (fw *failingWriter) Write(p []byte) (n int, err error) {
	fw.writeCount++
	if fw.failOnNthWrite > 0 && fw.writeCount == fw.failOnNthWrite {
		return 0, fmt.Errorf("simulated write error on write #%d", fw.writeCount)
	}
	return fw.buffer.Write(p)
}

func TestTestServer(t *testing.T) {

	ts := testServer()
	defer ts.Close()

	t.Run("index should be not found", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("expected status %d; got %d", http.StatusNotFound, res.StatusCode)
		}
	})

	t.Run("example should return ok", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/example")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		expected := "example content\n"
		if string(body) != expected {
			t.Errorf("expected body %q; got %q", expected, string(body))
		}
	})

	t.Run("non-existent file should be not found", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/nonexistent.txt")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("expected status %d; got %d", http.StatusNotFound, res.StatusCode)
		}
	})

	t.Run("serve plain text file", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/test.txt")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		host := strings.TrimPrefix(ts.URL, "http://")
		expected := "Hello " + host + "\n"
		if string(body) != expected {
			t.Errorf("expected body %q; got %q", expected, string(body))
		}
	})

	t.Run("serve gzipped file", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/test.gz")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		unzippedBody, err := unzip(body)
		if err != nil {
			t.Fatalf("Failed to unzip response body: %v", err)
		}
		host := strings.TrimPrefix(ts.URL, "http://")
		expected := "Gzipped " + host
		if string(unzippedBody) != expected {
			t.Errorf("expected body %q; got %q", expected, string(unzippedBody))
		}
	})

	t.Run("unzip error should be handled", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/corrupted.gz")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		// The handler returns the error in "error: %v\n" format
		expected := "error: gzip: invalid header\n"
		if string(body) != expected {
			t.Errorf("expected body %q; got %q", expected, string(body))
		}
	})

	t.Run("handle zip error", func(t *testing.T) {
		// Replace the original zip function with a mock that returns an error
		originalZipFunc := zipFunc
		zipFunc = func(_ []byte, _ io.Writer) ([]byte, error) {
			return nil, fmt.Errorf("simulated zip error")
		}
		// Ensure the original function is restored after the test
		defer func() { zipFunc = originalZipFunc }()

		// Request a gzipped file to trigger the code path that calls zip
		res, err := http.Get(ts.URL + "/test.gz")
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer func() { _ = res.Body.Close() }()

		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		// Check if the handler returned the expected error message
		expected := "error: simulated zip error\n"
		if string(body) != expected {
			t.Errorf("expected body %q; got %q", expected, string(body))
		}
	})
}

func TestZip(t *testing.T) {
	content := []byte("hello world")

	t.Run("successful compression", func(t *testing.T) {
		compressed, err := zip(content, nil)
		if err != nil {
			t.Fatalf("zip failed: %v", err)
		}

		r, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatalf("failed to create gzip reader: %v", err)
		}
		defer func() { _ = r.Close() }()
		uncompressed, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("failed to read uncompressed data: %v", err)
		}
		if !bytes.Equal(content, uncompressed) {
			t.Errorf("content does not match, got: %s, want: %s", uncompressed, content)
		}
	})

	t.Run("writer fails on write", func(t *testing.T) {
		writer := &failingWriter{failOnNthWrite: 1}
		b, err := zip(content, writer)
		if err == nil {
			t.Error("expected an error, but got nil")
		}
		if !bytes.Equal(b, content) {
			t.Errorf("expected original content on error, got %q, want %q", string(b), string(content))
		}
	})

	t.Run("writer fails on close", func(t *testing.T) {
		// The first write in gzip.Writer.Write may succeed, but the flush during gzip.Writer.Close will fail.
		writer := &failingWriter{failOnNthWrite: 2}
		b, err := zip(content, writer)
		if err == nil {
			t.Error("expected an error, but got nil")
		}
		if !bytes.Equal(b, content) {
			t.Errorf("expected original content on error, got %q, want %q", string(b), string(content))
		}
	})

	t.Run("unsupported writer type", func(t *testing.T) {
		writer := &failingWriter{} // This is not a *bytes.Buffer
		_, err := zip(content, writer)
		if err == nil {
			t.Fatal("expected an error, but got nil")
		}
		expectedErr := "cannot retrieve compressed bytes from provided writer type"
		if err.Error() != expectedErr {
			t.Errorf("expected error %q, got %q", expectedErr, err.Error())
		}
	})
}
