package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Limit the whole multipart request, not just ParseMultipartForm's RAM usage.
const maxSSHUploadBytes = 1 << 20

func uploadSSHKey(w http.ResponseWriter, r *http.Request, kind string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSSHUploadBytes)
	if err := r.ParseMultipartForm(maxSSHUploadBytes); err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		jsonError(w, status, "SSH key upload must be a multipart request of at most 1 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("ssh_key_file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ssh_key_file field missing")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "cannot read SSH key upload")
		return
	}
	if err := validateSSHKeyEnvelope(data, kind); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, "SSH key validation requires ssh-keygen; run scripts/install-prerequisites.sh")
		return
	}
	if err := os.MkdirAll(sshSecretsDir(), 0700); err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot create SSH secrets directory")
		return
	}
	ext := ".pub"
	if kind == "private" {
		ext = ".key"
	}
	// CreateTemp uses exclusive creation and mode 0600. Never use the client's
	// filename as a storage path, even for repeated uploads of the same key.
	stored, err := os.CreateTemp(sshSecretsDir(), "upload-*"+ext)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot store SSH key")
		return
	}
	keep := false
	defer func() {
		stored.Close()
		if !keep {
			os.Remove(stored.Name())
		}
	}()
	if _, err := stored.Write(data); err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot write SSH key")
		return
	}
	if err := stored.Close(); err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot close SSH key file")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []string{"-l", "-f", stored.Name()}
	if kind == "private" {
		encrypted, err := openSSHKeyEncrypted(data)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid OpenSSH private-key payload")
			return
		}
		// Unencrypted private keys must actually decode, not just contain a
		// fingerprintable public section. Encrypted keys cannot be decrypted
		// without a passphrase; validate their envelope and public section.
		if !encrypted {
			args = []string{"-y", "-P", "", "-f", stored.Name()}
		}
	}
	cmd := exec.CommandContext(ctx, keygen, args...)
	if err := cmd.Run(); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid or unsupported SSH key; use an OpenSSH public/private key or an unencrypted PEM private key")
		return
	}
	path, err := filepath.Abs(stored.Name())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot resolve SSH key path")
		return
	}
	keep = true
	writeJSON(w, http.StatusOK, map[string]string{
		"path": path, "filename": managedUploadFilename(header.Filename, kind+ext),
	})
}

// Inspect the OpenSSH container without prompting for or retaining a passphrase.
// PEM private keys are validated by ssh-keygen -y instead.
func openSSHKeyEncrypted(data []byte) (bool, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		return false, nil
	}
	invalid := fmt.Errorf("invalid OpenSSH private-key container")
	const magic = "openssh-key-v1\x00"
	if !bytes.HasPrefix(block.Bytes, []byte(magic)) {
		return false, invalid
	}
	rest := block.Bytes[len(magic):]
	readString := func() ([]byte, bool) {
		if len(rest) < 4 {
			return nil, false
		}
		n := binary.BigEndian.Uint32(rest)
		rest = rest[4:]
		if uint64(n) > uint64(len(rest)) {
			return nil, false
		}
		value := rest[:n]
		rest = rest[n:]
		return value, true
	}
	cipher, ok := readString()
	if !ok || len(cipher) == 0 {
		return false, invalid
	}
	kdf, ok := readString()
	if !ok {
		return false, invalid
	}
	options, ok := readString()
	if !ok || len(rest) < 4 || binary.BigEndian.Uint32(rest) != 1 {
		return false, invalid
	}
	rest = rest[4:]
	public, ok := readString()
	if !ok || len(public) == 0 {
		return false, invalid
	}
	private, ok := readString()
	if !ok || len(rest) != 0 || len(private) < 8 || len(private)%8 != 0 {
		return false, invalid
	}
	if string(cipher) == "none" {
		if string(kdf) != "none" || len(options) != 0 {
			return false, invalid
		}
		return false, nil
	}
	if string(kdf) != "bcrypt" || len(options) < 8 {
		return false, invalid
	}
	saltSize := uint64(binary.BigEndian.Uint32(options))
	if saltSize == 0 || saltSize+8 != uint64(len(options)) || binary.BigEndian.Uint32(options[len(options)-4:]) == 0 {
		return false, invalid
	}
	return true, nil
}

func validateSSHKeyEnvelope(data []byte, kind string) error {
	data = bytes.TrimSpace(data)
	invalid := fmt.Errorf("upload must contain one valid SSH %s key", kind)
	if len(data) == 0 {
		return invalid
	}
	if kind == "private" {
		block, rest := pem.Decode(data)
		if block == nil || !bytes.HasPrefix(data, []byte("-----BEGIN ")) || len(bytes.TrimSpace(rest)) != 0 {
			return invalid
		}
		switch block.Type {
		case "OPENSSH PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY", "PRIVATE KEY":
			return nil
		default:
			return invalid
		}
	}
	fields := strings.Fields(string(data))
	if bytes.ContainsAny(data, "\r\n") || len(fields) < 2 {
		return invalid
	}
	if !strings.HasPrefix(fields[0], "ssh-") && !strings.HasPrefix(fields[0], "ecdsa-") && !strings.HasPrefix(fields[0], "sk-") {
		return invalid
	}
	return nil
}
