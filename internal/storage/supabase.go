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

type Supabase struct {
	baseURL string // https://<project>.supabase.co
	apiKey  string // service_role (legacy JWT) atau secret API key
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

// MakeObjectKey: simpan dengan prefix case/<caseID> agar rapi
func (s *Supabase) MakeObjectKey(caseID, filename string) string {
	return path.Join("case", caseID, filename)
}

// Upload: POST /storage/v1/object/{bucket}/{objectName}
func (s *Supabase) Upload(key string, r io.Reader, contentType string, size int64) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.baseURL, s.bucket, key)

	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return err
	}
	// Wajib:
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("apikey", s.apiKey)

	// Jika s.apiKey adalah **legacy service_role (JWT)** → kirim Authorization.
	// Jika Anda pakai Secret API Key (sb_secret_...) yang BUKAN JWT, HAPUS baris Authorization di bawah.
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

// SignedURL: POST /storage/v1/object/sign/{bucket}/{objectName}  body: {"expiresIn": N}
func (s *Supabase) SignedURL(key string, expiresInSeconds int) (string, error) {
	url := fmt.Sprintf("%s/storage/v1/object/sign/%s/%s", s.baseURL, s.bucket, key)

	body, _ := json.Marshal(map[string]int{"expiresIn": expiresInSeconds})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	// Lihat catatan di atas soal Authorization:
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
	// API mengembalikan path relatif; jadikan absolute URL
	return s.baseURL + "/storage/v1" + out.SignedURL, nil
}
