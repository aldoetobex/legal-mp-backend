package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Supabase struct {
	URL    string
	Bucket string
	Key    string // Secret API key (server-only)
}

func NewSupabase() *Supabase {
	return &Supabase{
		URL:    strings.TrimRight(os.Getenv("SUPABASE_URL"), "/"),
		Bucket: os.Getenv("SUPABASE_BUCKET"),
		Key:    os.Getenv("SUPABASE_SECRET_KEY"),
	}
}

func (s *Supabase) Upload(key string, r io.Reader, contentType string, size int64) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.URL, s.Bucket, key)
	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase upload failed: %s - %s", resp.Status, string(b))
	}
	return nil
}

func (s *Supabase) SignedURL(key string, expSeconds int) (string, error) {
	url := fmt.Sprintf("%s/storage/v1/object/sign/%s/%s", s.URL, s.Bucket, key)
	body := fmt.Sprintf(`{"expiresIn": %d}`, expSeconds)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sign url failed: %s - %s", resp.Status, string(b))
	}

	var sr struct {
		SignedURL string `json:"signedURL"`
		SignedUrl string `json:"signedUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", err
	}
	path := sr.SignedURL
	if path == "" {
		path = sr.SignedUrl
	}
	return s.URL + path, nil
}

func (s *Supabase) MakeObjectKey(caseID, original string) string {
	ext := filepath.Ext(original)
	if mime.TypeByExtension(ext) == "" {
		ext = ".bin"
	}
	ts := time.Now().UnixNano()
	base := filepath.Base(original)
	return fmt.Sprintf("cases/%s/%d_%s", caseID, ts, base)
}
