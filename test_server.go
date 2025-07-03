package sitemap

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// testServer creates a test server with a custom request handler that serves static files and dynamically replaces
// the "HOST" string in the response with the value of the request's Host header. The server handles the following routes:
//   - "/" returns a 404 Not Found response.
//   - "/example" returns a 200 OK response with the content "example content".
//   - other routes serve static files located in the "./test" directory. If a file is gzip-encoded, it will be decompressed,
//     and if it contains the "HOST" string, it will be replaced with the request's Host value. The modified response will be
//     sent back to the client.
//
// It returns an httptest.Server instance, which can be used to make HTTP requests to the test server.
func testServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "/" {
			// index page is always not found
			http.NotFound(w, r)
			return
		}
		if r.RequestURI == "/example" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "example content")
			return
		}

		res, err := os.ReadFile("./test" + r.RequestURI)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		strRes := string(res)
		if strings.Contains(strRes, "\x1f\x8b\x08") {
			resUncompressed, err := unzip(res)
			if err != nil {
				_, _ = fmt.Fprintf(w, "error: %v\n", err)
				return
			}
			strRes = strings.Replace(string(resUncompressed), "HOST", r.Host, -1)

			resCompressed, err := zip([]byte(strRes), nil)
			if err != nil {
				_, _ = fmt.Fprintf(w, "error: %v\n", err)
				return
			}
			strRes = string(resCompressed)
		} else {
			strRes = strings.Replace(strRes, "HOST", r.Host, -1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, strRes)
	}))
}

// zip compresses the given content using gzip compression.
// It returns the compressed content as a byte array.
// If an error occurs during compression, it returns the original content and the error.
// The optional 'w' parameter allows injecting a custom io.Writer for testing purposes.
func zip(content []byte, w io.Writer) ([]byte, error) {
	if w == nil {
		w = bytes.NewBuffer(nil)
	}
	gzipWriter := gzip.NewWriter(w)
	_, err := gzipWriter.Write(content)
	if err != nil {
		return content, err
	}
	err = gzipWriter.Close()
	if err != nil {
		return content, err
	}
	// Type assertion to get bytes.Buffer if the writer is one.
	// This assumes that if w is nil, it will be a bytes.Buffer.
	// If a custom writer is provided, it must be able to return its bytes.
	// For testing, we know our mockWriter has a bytes.Buffer.
	if buf, ok := w.(*bytes.Buffer); ok {
		return buf.Bytes(), nil
	}
	// If not a bytes.Buffer, we can't get the bytes this way.
	// This case should ideally not be hit in this specific context where we control `w`.
	return nil, fmt.Errorf("cannot retrieve compressed bytes from provided writer type")
}
