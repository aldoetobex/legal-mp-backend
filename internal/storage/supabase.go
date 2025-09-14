package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"
)

/*
Supabase wraps minimal calls to Supabase Storage REST API.

Notes on authorization:
- If you use a legacy service_role JWT, send both `apikey` and `Authorization: Bearer <token>`.
- If you use a Secret API Key (sb_secret_...) that is NOT a JWT, some setups do NOT require the
  Authorization header. In that case, remove the `Authorization` header lines below.
*/

type Supabase struct {
	baseURL string // e.g. https://<project>.supabase.co
	apiKey  string // service_role JWT or secret API key
	bucket  string
	client  *http.Client
}

func NewSupabase() *Supabase {
	return &Supabase{
		baseURL: os.Getenv("SUPABASE_URL"),
		apiKey:  os.Getenv("SUPABASE_SERVICE_KEY"),
		bucket:  os.Getenv("SUPABASE_BUCKET"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// MakeObjectKey builds a tidy, per-case object key: case/<caseID>/<filename>
func (s *Supabase) MakeObjectKey(caseID, filename string) string {
	return path.Join("case", caseID, filename)
}

// Upload sends a new object to: POST /storage/v1/object/{bucket}/{objectName}
func (s *Supabase) Upload(key string, r io.Reader, contentType string, size int64) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.baseURL, s.bucket, key)

	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("apikey", s.apiKey)
	// See header note at the top of the file.
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("supabase upload error: %s | %s", res.Status, string(body))
	}
	return nil
}

// SignedURL creates a short-lived signed URL:
// POST /storage/v1/object/sign/{bucket}/{objectName}  body: {"expiresIn": <seconds>}
func (s *Supabase) SignedURL(key string, expiresInSeconds int) (string, error) {
	url := fmt.Sprintf("%s/storage/v1/object/sign/%s/%s", s.baseURL, s.bucket, key)

	body, _ := json.Marshal(map[string]int{"expiresIn": expiresInSeconds})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	// See header note at the top of the file.
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("supabase sign error: %s | %s", res.Status, string(b))
	}

	var out struct {
		SignedURL string `json:"signedURL"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.SignedURL == "" {
		return "", fmt.Errorf("empty signedURL in response")
	}

	// API returns a relative path; convert to absolute URL.
	return s.baseURL + "/storage/v1" + out.SignedURL, nil
}

// Delete removes an object by key:
// DELETE /storage/v1/object/{bucket}/{objectName}
// This is idempotent: 404 is treated as success (already deleted).
func (s *Supabase) Delete(key string) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.baseURL, s.bucket, key)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", s.apiKey)
	// See header note at the top of the file.
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("supabase delete error: %s | %s", res.Status, string(b))
	}
	return nil
}

// BulkDelete removes multiple objects in one call:
// POST /storage/v1/object/{bucket}/remove
// Body: {"prefixes": ["case/<id>/file1.png", "case/<id>/file2.pdf", ...]}
func (s *Supabase) BulkDelete(keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/storage/v1/object/%s/remove", s.baseURL, s.bucket)

	body, _ := json.Marshal(map[string][]string{"prefixes": keys})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	// See header note at the top of the file.
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("supabase bulk delete error: %s | %s", res.Status, string(b))
	}

	// API usually returns an array of per-prefix results; errors are already handled above.
	return nil
}
