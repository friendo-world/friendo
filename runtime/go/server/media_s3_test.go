package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// TestMediaS3RoundTrip exercises the S3/R2 media path end-to-end through the real
// HTTP handlers: an uploaded image lands in object storage (not on disk), is
// served from there, and is removed on delete. Gated on FRIENDO_S3_* — skips
// unless an S3-compatible endpoint (e.g. a local MinIO) is configured.
//
//	docker run -d --name friendo-minio -p 9000:9000 minio/minio server /data
//	FRIENDO_S3_ENDPOINT=http://127.0.0.1:9000 FRIENDO_S3_BUCKET=friendo \
//	  FRIENDO_S3_ACCESS_KEY=minioadmin FRIENDO_S3_SECRET_KEY=minioadmin \
//	  go test ./runtime/go/server/ -run TestMediaS3RoundTrip -v
func TestMediaS3RoundTrip(t *testing.T) {
	endpoint, bucket, access, secret := setupS3(t) // a real endpoint if given, else an in-process fake
	ensureBucket(t, endpoint, bucket, access, secret)

	// A minimal site (BuildSiteHandler needs a pages/ dir) with S3 media (env set).
	siteDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(siteDir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("data.Open: %v", err)
	}
	defer db.Conn.Close()

	handler, err := BuildSiteHandler(siteDir, db, true /* openAdmin: no auth needed */)
	if err != nil {
		t.Fatalf("BuildSiteHandler: %v", err)
	}

	imageBytes := []byte("\x89PNG\r\n\x1a\n-fake-image-bytes-for-test-")

	// Upload via the media API.
	body, contentType := multipartImage(t, "gallery", "photo.png", imageBytes)
	up := httptest.NewRequest(http.MethodPost, "/_/api/files", body)
	up.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, up)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		File struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"file"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !strings.HasPrefix(out.File.URL, "/assets/uploads/") {
		t.Fatalf("unexpected upload url %q", out.File.URL)
	}

	// The bytes must NOT be on local disk — they live in object storage.
	if _, err := os.Stat(filepath.Join(siteDir, filepath.FromSlash(strings.TrimPrefix(out.File.URL, "/")))); !os.IsNotExist(err) {
		t.Errorf("uploaded media unexpectedly present on disk (should be in S3)")
	}

	// Serve it — streamed from object storage.
	get := httptest.NewRequest(http.MethodGet, out.File.URL, nil)
	grec := httptest.NewRecorder()
	handler.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("serve = %d, want 200", grec.Code)
	}
	if !bytes.Equal(grec.Body.Bytes(), imageBytes) {
		t.Errorf("served bytes differ from uploaded bytes")
	}

	// Delete it, then it should 404.
	del := httptest.NewRequest(http.MethodDelete, "/_/api/files/"+out.File.ID, nil)
	drec := httptest.NewRecorder()
	handler.ServeHTTP(drec, del)
	if drec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204; body: %s", drec.Code, drec.Body.String())
	}
	get2 := httptest.NewRequest(http.MethodGet, out.File.URL, nil)
	grec2 := httptest.NewRecorder()
	handler.ServeHTTP(grec2, get2)
	if grec2.Code != http.StatusNotFound {
		t.Errorf("serve after delete = %d, want 404", grec2.Code)
	}
}

// setupS3 returns S3 connection details and ensures the FRIENDO_S3_* env is set so
// the runtime picks up the backend. It uses a real endpoint when FRIENDO_S3_ENDPOINT
// is provided, otherwise an in-process S3 fake so the test is self-contained.
func setupS3(t *testing.T) (endpoint, bucket, access, secret string) {
	if ep := os.Getenv("FRIENDO_S3_ENDPOINT"); ep != "" {
		return ep, os.Getenv("FRIENDO_S3_BUCKET"), os.Getenv("FRIENDO_S3_ACCESS_KEY"), os.Getenv("FRIENDO_S3_SECRET_KEY")
	}
	ts := httptest.NewServer(gofakes3.New(s3mem.New()).Server())
	t.Cleanup(ts.Close)
	endpoint, bucket, access, secret = ts.URL, "friendo", "test", "test"
	t.Setenv("FRIENDO_S3_ENDPOINT", endpoint)
	t.Setenv("FRIENDO_S3_BUCKET", bucket)
	t.Setenv("FRIENDO_S3_ACCESS_KEY", access)
	t.Setenv("FRIENDO_S3_SECRET_KEY", secret)
	t.Setenv("FRIENDO_S3_REGION", "us-east-1")
	return endpoint, bucket, access, secret
}

func multipartImage(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("record_type", "post")
	_ = mw.WriteField("record_id", "p1")
	_ = mw.WriteField("field", field)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func ensureBucket(t *testing.T, endpoint, bucket, access, secret string) {
	t.Helper()
	host, secure := endpoint, false
	if strings.HasPrefix(host, "https://") {
		host, secure = strings.TrimPrefix(host, "https://"), true
	} else {
		host = strings.TrimPrefix(host, "http://")
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Secure: secure,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatalf("bucket check (is MinIO running?): %v", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("make bucket: %v", err)
		}
	}
}
