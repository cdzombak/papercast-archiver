package instapaper

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	want := &Credentials{Token: "tok", TokenSecret: "sec"}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	}

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.Token != want.Token || got.TokenSecret != want.TokenSecret {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadCredentialsMissing(t *testing.T) {
	_, err := LoadCredentials(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("want fs.ErrNotExist, got %v", err)
	}
}

func TestLoadCredentialsBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredentials(path); err == nil {
		t.Error("want error for invalid JSON")
	}
}
