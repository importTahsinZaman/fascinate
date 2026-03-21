package toolauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fascinate/internal/config"
)

var ErrProfileNotFound = errors.New("tool auth profile not found")

type Store struct {
	rootDir string
	keyPath string
	key     []byte
	now     func() time.Time
}

func NewStore(cfg config.Config) (*Store, error) {
	rootDir := strings.TrimSpace(cfg.ToolAuthDir)
	if rootDir == "" {
		return nil, fmt.Errorf("tool auth directory is required")
	}
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return nil, err
	}

	keyPath := strings.TrimSpace(cfg.ToolAuthKeyPath)
	if keyPath == "" {
		return nil, fmt.Errorf("tool auth key path is required")
	}
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}

	return &Store{
		rootDir: rootDir,
		keyPath: keyPath,
		key:     key,
		now:     time.Now,
	}, nil
}

func (s *Store) HasProfile(ctx context.Context, key ProfileKey) (bool, error) {
	_, _, err := s.LoadSessionState(ctx, key)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrProfileNotFound) {
		return false, nil
	}
	return false, err
}

func (s *Store) LoadSessionState(_ context.Context, key ProfileKey) (Profile, []byte, error) {
	profilePath := filepath.Join(s.profileDir(key), "profile.json")
	profileBody, err := os.ReadFile(profilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Profile{}, nil, ErrProfileNotFound
		}
		return Profile{}, nil, err
	}

	var profile Profile
	if err := json.Unmarshal(profileBody, &profile); err != nil {
		return Profile{}, nil, err
	}

	ciphertext, err := os.ReadFile(filepath.Join(s.profileDir(key), "state.enc"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Profile{}, nil, ErrProfileNotFound
		}
		return Profile{}, nil, err
	}

	plaintext, err := decryptState(s.key, ciphertext)
	if err != nil {
		return Profile{}, nil, err
	}

	return profile, plaintext, nil
}

func (s *Store) SaveSessionState(_ context.Context, profile Profile, archive []byte) error {
	dir := s.profileDir(profile.Key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	profile.BundleSHA256 = archiveSHA256(archive)
	profile.UpdatedAt = s.now().UTC().Format(time.RFC3339)

	encrypted, err := encryptState(s.key, archive)
	if err != nil {
		return err
	}

	if err := s.backupIfExists(filepath.Join(dir, "state.enc"), filepath.Join(dir, "state.prev.enc")); err != nil {
		return err
	}
	if err := s.backupIfExists(filepath.Join(dir, "profile.json"), filepath.Join(dir, "profile.prev.json")); err != nil {
		return err
	}

	if err := writeFileAtomically(filepath.Join(dir, "state.enc"), encrypted, 0o600); err != nil {
		return err
	}

	body, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(dir, "profile.json"), body, 0o600)
}

func (s *Store) MarkCaptureResult(_ context.Context, key ProfileKey, captureErr error) error {
	return s.updateProfileMetadata(key, func(profile *Profile) {
		now := s.now().UTC().Format(time.RFC3339)
		profile.LastCaptureAt = &now
		if captureErr == nil {
			profile.LastCaptureError = nil
			return
		}
		message := captureErr.Error()
		profile.LastCaptureError = &message
	})
}

func (s *Store) MarkRestoreResult(_ context.Context, key ProfileKey, restoreErr error) error {
	return s.updateProfileMetadata(key, func(profile *Profile) {
		now := s.now().UTC().Format(time.RFC3339)
		profile.LastRestoreAt = &now
		if restoreErr == nil {
			profile.LastRestoreError = nil
			return
		}
		message := restoreErr.Error()
		profile.LastRestoreError = &message
	})
}

func (s *Store) ListProfiles(_ context.Context, userID string) ([]Profile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}

	root := filepath.Join(s.rootDir, sanitizePathPart(userID))
	toolEntries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []Profile
	for _, toolEntry := range toolEntries {
		if !toolEntry.IsDir() {
			continue
		}
		authEntries, err := os.ReadDir(filepath.Join(root, toolEntry.Name()))
		if err != nil {
			return nil, err
		}
		for _, authEntry := range authEntries {
			if !authEntry.IsDir() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(root, toolEntry.Name(), authEntry.Name(), "profile.json"))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			var profile Profile
			if err := json.Unmarshal(body, &profile); err != nil {
				return nil, err
			}
			profiles = append(profiles, profile)
		}
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Key.ToolID == profiles[j].Key.ToolID {
			return profiles[i].Key.AuthMethodID < profiles[j].Key.AuthMethodID
		}
		return profiles[i].Key.ToolID < profiles[j].Key.ToolID
	})

	return profiles, nil
}

func (s *Store) profileDir(key ProfileKey) string {
	return filepath.Join(s.rootDir, sanitizePathPart(key.UserID), sanitizePathPart(key.ToolID), sanitizePathPart(key.AuthMethodID))
}

func (s *Store) updateProfileMetadata(key ProfileKey, mutate func(*Profile)) error {
	profilePath := filepath.Join(s.profileDir(key), "profile.json")
	body, err := os.ReadFile(profilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProfileNotFound
		}
		return err
	}

	var profile Profile
	if err := json.Unmarshal(body, &profile); err != nil {
		return err
	}
	mutate(&profile)

	updated, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(profilePath, updated, 0o600)
}

func (s *Store) backupIfExists(sourcePath, backupPath string) error {
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeFileAtomically(backupPath, body, 0o600)
}

func loadOrCreateKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	body, err := os.ReadFile(path)
	if err == nil {
		if len(body) != 32 {
			return nil, fmt.Errorf("tool auth key at %s must be 32 bytes", path)
		}
		return body, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := writeFileAtomically(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func encryptState(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptState(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	body := ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}

func writeFileAtomically(path string, body []byte, mode os.FileMode) error {
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, body, mode); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func sanitizePathPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_")
	return replacer.Replace(strings.TrimSpace(value))
}
