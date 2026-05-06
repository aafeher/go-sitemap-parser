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

func mustGetBody(t *testing.T, url string) (int, []byte) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	return res.StatusCode, body
}

func mustUnzip(t *testing.T, data []byte) []byte {
	t.Helper()
	result, err := unzip(data)
	if err != nil {
		t.Fatalf("Failed to unzip response body: %v", err)
	}
	return result
}

func TestTestServer(t *testing.T) {
	ts := testServer()
	defer ts.Close()

	t.Run("index should be not found", func(t *testing.T) {
		status, _ := mustGetBody(t, ts.URL+"/")
		mustEqual(t, "status", status, http.StatusNotFound)
	})

	t.Run("example should return ok", func(t *testing.T) {
		_, body := mustGetBody(t, ts.URL+"/example")
		mustEqual(t, "body", string(body), "example content\n")
	})

	t.Run("non-existent file should be not found", func(t *testing.T) {
		status, _ := mustGetBody(t, ts.URL+"/nonexistent.txt")
		mustEqual(t, "status", status, http.StatusNotFound)
	})

	t.Run("serve plain text file", func(t *testing.T) {
		_, body := mustGetBody(t, ts.URL+"/test.txt")
		host := strings.TrimPrefix(ts.URL, "http://")
		mustEqual(t, "body", string(body), "Hello "+host+"\n")
	})

	t.Run("serve gzipped file", func(t *testing.T) {
		_, body := mustGetBody(t, ts.URL+"/test.gz")
		unzippedBody := mustUnzip(t, body)
		host := strings.TrimPrefix(ts.URL, "http://")
		mustEqual(t, "body", string(unzippedBody), "Gzipped "+host)
	})

	t.Run("unzip error should be handled", func(t *testing.T) {
		_, body := mustGetBody(t, ts.URL+"/corrupted.gz")
		mustEqual(t, "body", string(body), "error: gzip: invalid header\n")
	})

	t.Run("handle zip error", func(t *testing.T) {
		originalZipFunc := zipFunc
		zipFunc = func(_ []byte, _ io.Writer) ([]byte, error) {
			return nil, fmt.Errorf("simulated zip error")
		}
		defer func() { zipFunc = originalZipFunc }()

		_, body := mustGetBody(t, ts.URL+"/test.gz")
		mustEqual(t, "body", string(body), "error: simulated zip error\n")
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
