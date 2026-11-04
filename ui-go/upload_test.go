package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func multipartUploadRequest(t *testing.T, field, filename, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func uploadResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func testSSHKeyPair(t *testing.T) (string, string) {
	t.Helper()
	if !toolInstalled("ssh-keygen") {
		t.Skip("ssh-keygen is required")
	}
	path := filepath.Join(t.TempDir(), "key")
	if output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("generate test key: %v %s", err, output)
	}
	private, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	public, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return string(public), string(private)
}

func TestSSHUploadUsesUniqueManagedFiles(t *testing.T) {
	previousDataDir := dataDir
	t.Cleanup(func() { dataDir = previousDataDir })
	dataDir = t.TempDir()
	public, private := testSSHKeyPair(t)

	publicRequest := multipartUploadRequest(t, "ssh_key_file", "workstation.pub", public)
	publicRequest.SetPathValue("kind", "public")
	publicRecorder := httptest.NewRecorder()
	apiUploadSettingsSSHKeyHandler(publicRecorder, publicRequest)
	publicResponse := uploadResponse(t, publicRecorder)
	if publicResponse["filename"] != "workstation.pub" {
		t.Fatalf("display filename = %q", publicResponse["filename"])
	}

	privateRequest := multipartUploadRequest(t, "ssh_key_file", "workstation", private)
	privateRequest.SetPathValue("kind", "private")
	privateRecorder := httptest.NewRecorder()
	apiUploadSettingsSSHKeyHandler(privateRecorder, privateRequest)
	privateResponse := uploadResponse(t, privateRecorder)
	if privateResponse["filename"] != "workstation" {
		t.Fatalf("display filename = %q", privateResponse["filename"])
	}

	for _, response := range []map[string]string{publicResponse, privateResponse} {
		path := response["path"]
		if !filepath.IsAbs(path) || filepath.Dir(path) != sshSecretsDir() || !strings.HasPrefix(filepath.Base(path), "upload-") {
			t.Fatalf("not a managed path: %q", path)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("key permissions: %v %v", info, err)
		}
	}
	if filepath.Ext(publicResponse["path"]) != ".pub" || filepath.Ext(privateResponse["path"]) != ".key" {
		t.Fatal("key role extensions are missing")
	}
}

func TestPlatformSSHUploadCannotOverwriteTerraform(t *testing.T) {
	oldData, oldTF := dataDir, terraformDir
	dataDir, terraformDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { dataDir, terraformDir = oldData, oldTF })
	public, _ := testSSHKeyPair(t)
	dir := filepath.Join(terraformDir, "aws")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.tf")
	if err := os.WriteFile(main, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, filename := range []string{"main.tf", "main.tf", "../../main.tf"} {
		req := multipartUploadRequest(t, "ssh_key_file", filename, public)
		req.SetPathValue("platform", "aws")
		rec := httptest.NewRecorder()
		apiUploadSSHKeyHandler(rec, req)
		response := uploadResponse(t, rec)
		path := response["path"]
		if filepath.Dir(path) != sshSecretsDir() || paths[path] {
			t.Fatalf("unsafe or reused path: %q", path)
		}
		paths[path] = true
	}
	content, err := os.ReadFile(main)
	if err != nil || string(content) != "keep" {
		t.Fatal("Terraform source was changed")
	}
}

func TestSSHUploadAcceptsEncryptedOpenSSHKey(t *testing.T) {
	oldData := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldData })
	if !toolInstalled("ssh-keygen") {
		t.Skip("ssh-keygen is required")
	}
	path := filepath.Join(t.TempDir(), "encrypted")
	if output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "test-only-passphrase", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("generate test key: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	req := multipartUploadRequest(t, "ssh_key_file", "encrypted", string(data))
	req.SetPathValue("kind", "private")
	rec := httptest.NewRecorder()
	apiUploadSettingsSSHKeyHandler(rec, req)
	uploadResponse(t, rec)
}

func TestSSHUploadRejectsInvalidAndOversizedKeys(t *testing.T) {
	oldData := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldData })
	public, private := testSSHKeyPair(t)
	for _, tc := range []struct {
		kind, content string
		status        int
	}{
		{"public", "", 400}, {"public", "resource terraform {}", 400},
		{"public", "ssh-ed25519 invalid-base64", 400},
		{"public", private, 400}, {"private", public, 400},
		{"private", "-----BEGIN OPENSSH PRIVATE KEY-----\nYQ==\n-----END OPENSSH PRIVATE KEY-----", 400},
		{"public", public + public, 400},
		{"public", strings.Repeat("x", maxSSHUploadBytes), 413},
		{"private", strings.Repeat("x", maxSSHUploadBytes), 413},
	} {
		req := multipartUploadRequest(t, "ssh_key_file", "main.tf", tc.content)
		req.SetPathValue("kind", tc.kind)
		rec := httptest.NewRecorder()
		apiUploadSettingsSSHKeyHandler(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s upload: expected %d, got %d: %s", tc.kind, tc.status, rec.Code, rec.Body.String())
		}
	}
	entries, err := os.ReadDir(sshSecretsDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("invalid uploads left files behind")
	}
	// The platform endpoint must enforce the same whole-request limit.
	req := multipartUploadRequest(t, "ssh_key_file", "main.tf", strings.Repeat("x", maxSSHUploadBytes))
	req.SetPathValue("platform", "aws")
	rec := httptest.NewRecorder()
	apiUploadSSHKeyHandler(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("platform upload size limit: %d", rec.Code)
	}
}

func TestChaosTokenUploadUsesUploadedFilename(t *testing.T) {
	previousDataDir := dataDir
	t.Cleanup(func() { dataDir = previousDataDir })
	dataDir = t.TempDir()
	request := multipartUploadRequest(t, "chaos_token_file", "sandbox-token.txt", "token-value")
	recorder := httptest.NewRecorder()
	apiUploadChaosTokenHandler(recorder, request)
	response := uploadResponse(t, recorder)

	if filepath.Base(response["path"]) != "sandbox-token.txt" {
		t.Fatalf("expected token filename sandbox-token.txt, got %q", response["path"])
	}
	data, err := os.ReadFile(filepath.Join(chaosTokenSecretsDir(), "sandbox-token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "token-value\n" {
		t.Fatalf("unexpected token content %q", data)
	}
}

func TestManagedUploadFilenamePreventsPathTraversal(t *testing.T) {
	if got := managedUploadFilename("../../token.txt", "fallback.token"); got != "token.txt" {
		t.Fatalf("expected basename to be retained safely, got %q", got)
	}
	if got := managedUploadFilename("", "fallback.token"); got != "fallback.token" {
		t.Fatalf("expected fallback filename, got %q", got)
	}
	if got := managedUploadFilename("..", "fallback.token"); got != "fallback.token" {
		t.Fatalf("expected dot-dot filename to use fallback, got %q", got)
	}
}
