package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envFunc turns a map into an Options.Getenv, so a test never touches the real
// environment.
func envFunc(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// writeConfig puts body at path at mode 0600 and returns the path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("cannot seed the config file: %v", err)
	}
	return path
}

func TestLoadTokenPrecedence(t *testing.T) {
	const (
		flagToken = "tok_from_flag_0001"
		envToken  = "tok_from_env_0002"
		fileToken = "tok_from_file_0003"
	)

	tests := []struct {
		name       string
		fileBody   string // empty: no config file at all
		env        map[string]string
		tokenFlag  string
		wantToken  string
		wantSource Source
	}{
		{
			name:       "flag beats environment and file",
			fileBody:   "token = \"" + fileToken + "\"\n",
			env:        map[string]string{EnvToken: envToken},
			tokenFlag:  flagToken,
			wantToken:  flagToken,
			wantSource: SourceFlag,
		},
		{
			name:       "environment beats file",
			fileBody:   "token = \"" + fileToken + "\"\n",
			env:        map[string]string{EnvToken: envToken},
			wantToken:  envToken,
			wantSource: SourceEnv,
		},
		{
			name:       "file is used when nothing else is set",
			fileBody:   "token = \"" + fileToken + "\"\n",
			wantToken:  fileToken,
			wantSource: SourceFile,
		},
		{
			name:       "flag beats file with no environment",
			fileBody:   "token = \"" + fileToken + "\"\n",
			tokenFlag:  flagToken,
			wantToken:  flagToken,
			wantSource: SourceFlag,
		},
		{
			name:       "environment is used with no file",
			env:        map[string]string{EnvToken: envToken},
			wantToken:  envToken,
			wantSource: SourceEnv,
		},
		{
			name:       "nothing configured",
			wantToken:  "",
			wantSource: SourceNone,
		},
		{
			name:       "a blank flag does not beat the environment",
			env:        map[string]string{EnvToken: envToken},
			tokenFlag:  "   ",
			wantToken:  envToken,
			wantSource: SourceEnv,
		},
		{
			name:       "a blank environment value does not beat the file",
			fileBody:   "token = \"" + fileToken + "\"\n",
			env:        map[string]string{EnvToken: ""},
			wantToken:  fileToken,
			wantSource: SourceFile,
		},
		{
			name:       "an empty token in the file reads as unset",
			fileBody:   "token = \"\"\n",
			wantToken:  "",
			wantSource: SourceNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if tc.fileBody != "" {
				path = writeConfig(t, dir, tc.fileBody)
			}

			cfg, err := Load(Options{TokenFlag: tc.tokenFlag, Path: path, Getenv: envFunc(tc.env)})
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			if cfg.Token != tc.wantToken {
				t.Errorf("token = %q, want %q", cfg.Token, tc.wantToken)
			}
			if cfg.TokenSource != tc.wantSource {
				t.Errorf("token source = %q, want %q", cfg.TokenSource, tc.wantSource)
			}
		})
	}
}

func TestLoadAPIURLPrecedence(t *testing.T) {
	const (
		envURL  = "http://localhost:3000/public/v1"
		fileURL = "http://127.0.0.1:4000/public/v1"
	)

	tests := []struct {
		name       string
		fileBody   string
		env        map[string]string
		wantURL    string
		wantSource Source
	}{
		{
			name:       "environment beats file and default",
			fileBody:   "api_url = \"" + fileURL + "\"\n",
			env:        map[string]string{EnvAPIURL: envURL},
			wantURL:    envURL,
			wantSource: SourceEnv,
		},
		{
			name:       "file beats default",
			fileBody:   "api_url = \"" + fileURL + "\"\n",
			wantURL:    fileURL,
			wantSource: SourceFile,
		},
		{
			name:       "production is the default",
			wantURL:    DefaultAPIURL,
			wantSource: SourceDefault,
		},
		{
			name:       "a blank override does not win",
			env:        map[string]string{EnvAPIURL: "  "},
			wantURL:    DefaultAPIURL,
			wantSource: SourceDefault,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if tc.fileBody != "" {
				path = writeConfig(t, dir, tc.fileBody)
			}

			cfg, err := Load(Options{Path: path, Getenv: envFunc(tc.env)})
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			if cfg.APIURL != tc.wantURL {
				t.Errorf("api url = %q, want %q", cfg.APIURL, tc.wantURL)
			}
			if cfg.APIURLSource != tc.wantSource {
				t.Errorf("api url source = %q, want %q", cfg.APIURLSource, tc.wantSource)
			}
		})
	}
}

func TestLoadMissingConfigFile(t *testing.T) {
	tests := []struct {
		name string
		path func(dir string) string
	}{
		{
			name: "no file in an existing directory",
			path: func(dir string) string { return filepath.Join(dir, "config.toml") },
		},
		{
			name: "no directory either",
			path: func(dir string) string { return filepath.Join(dir, "nested", "deeper", "config.toml") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path(t.TempDir())

			cfg, err := Load(Options{Path: path, Getenv: envFunc(nil)})
			if err != nil {
				t.Fatalf("a missing config file must not be an error, got: %v", err)
			}
			if cfg.FileExists {
				t.Error("FileExists = true for a file that is not there")
			}
			if cfg.Token != "" || cfg.TokenSource != SourceNone {
				t.Errorf("token = %q from %q, want no token", cfg.Token, cfg.TokenSource)
			}
			if cfg.APIURL != DefaultAPIURL {
				t.Errorf("api url = %q, want the default %q", cfg.APIURL, DefaultAPIURL)
			}
			if cfg.Path != path {
				t.Errorf("Path = %q, want %q", cfg.Path, path)
			}
		})
	}
}

func TestLoadMalformedConfigFile(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unterminated string", body: "token = \"missing the closing quote\n"},
		{name: "not toml at all", body: "{\"token\": \"json, not toml\"}\n"},
		{name: "wrong type for token", body: "token = 12345\n"},
		{name: "unterminated table header", body: "[section\ntoken = \"x\"\n"},
		{name: "duplicate key", body: "token = \"a\"\ntoken = \"b\"\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), tc.body)

			_, err := Load(Options{Path: path, Getenv: envFunc(nil)})
			if err == nil {
				t.Fatal("a malformed config file must be an error, got nil")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error does not name the file: %v", err)
			}
		})
	}
}

func TestSaveTokenPermissions(t *testing.T) {
	tests := []struct {
		name string
		// seedMode is the mode of a pre-existing config file; 0 means none.
		seedMode  fs.FileMode
		wantErr   bool
		wantToken string
	}{
		{name: "fresh file is written 0600", seedMode: 0, wantToken: "tok_fresh_000001"},
		{name: "an existing 0600 file is rewritten", seedMode: 0o600, wantToken: "tok_rewrite_0001"},
		{name: "a group-readable file is refused", seedMode: 0o640, wantErr: true},
		{name: "a world-readable file is refused", seedMode: 0o644, wantErr: true},
		{name: "a world-writable file is refused", seedMode: 0o666, wantErr: true},
		{name: "a group-writable file is refused", seedMode: 0o620, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			const seeded = "tok_already_there"
			if tc.seedMode != 0 {
				if err := os.WriteFile(path, []byte("token = \""+seeded+"\"\n"), 0o600); err != nil {
					t.Fatalf("cannot seed: %v", err)
				}
				if err := os.Chmod(path, tc.seedMode); err != nil {
					t.Fatalf("cannot chmod the seed: %v", err)
				}
			}

			token := tc.wantToken
			if token == "" {
				token = "tok_should_not_land"
			}
			err := SaveToken(path, token)

			if tc.wantErr {
				if err == nil {
					t.Fatal("SaveToken must refuse a file readable beyond its owner, got nil")
				}
				if !errors.Is(err, ErrInsecurePermissions) {
					t.Errorf("error = %v, want ErrInsecurePermissions", err)
				}
				// The refusal must not have touched the file.
				body, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatalf("the refused file is gone: %v", readErr)
				}
				if !strings.Contains(string(body), seeded) {
					t.Errorf("a refused write still changed the file: %q", body)
				}
				info, _ := os.Stat(path)
				if info.Mode().Perm() != tc.seedMode {
					t.Errorf("a refused write changed the mode to %#o", info.Mode().Perm())
				}
				return
			}

			if err != nil {
				t.Fatalf("SaveToken: unexpected error: %v", err)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("stat: %v", statErr)
			}
			if perm := info.Mode().Perm(); perm != FileMode {
				t.Errorf("mode = %#o, want %#o", perm, FileMode)
			}

			cfg, loadErr := Load(Options{Path: path, Getenv: envFunc(nil)})
			if loadErr != nil {
				t.Fatalf("Load after SaveToken: %v", loadErr)
			}
			if cfg.Token != tc.wantToken {
				t.Errorf("stored token = %q, want %q", cfg.Token, tc.wantToken)
			}
		})
	}
}

func TestSaveTokenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "investviews")
	path := filepath.Join(dir, "config.toml")

	if err := SaveToken(path, "tok_makes_the_dir"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the config directory was not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != DirMode {
		t.Errorf("directory mode = %#o, want %#o", perm, DirMode)
	}
}

func TestSaveTokenKeepsAPIURL(t *testing.T) {
	const url = "http://localhost:3000/public/v1"
	path := writeConfig(t, t.TempDir(), "api_url = \""+url+"\"\n")

	if err := SaveToken(path, "tok_keeps_the_url"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	cfg, err := Load(Options{Path: path, Getenv: envFunc(nil)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIURL != url {
		t.Errorf("api url = %q, want the stored %q", cfg.APIURL, url)
	}
	if cfg.Token != "tok_keeps_the_url" {
		t.Errorf("token = %q, want the one just saved", cfg.Token)
	}
}

func TestSaveTokenRejectsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := SaveToken(path, "   "); err == nil {
		t.Fatal("SaveToken must refuse an empty token, got nil")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused empty token still created %s", path)
	}
}

func TestClearToken(t *testing.T) {
	tests := []struct {
		name        string
		fileBody    string // empty: no config file
		wantRemoved bool
		wantFile    bool // the file survives
		wantAPIURL  string
	}{
		{
			name:        "removes the file when the token was all it held",
			fileBody:    "token = \"tok_to_remove_01\"\n",
			wantRemoved: true,
		},
		{
			name:        "keeps the file when it also holds an api url",
			fileBody:    "token = \"tok_to_remove_02\"\napi_url = \"http://localhost:3000/public/v1\"\n",
			wantRemoved: true,
			wantFile:    true,
			wantAPIURL:  "http://localhost:3000/public/v1",
		},
		{
			name:        "no config file is not an error",
			wantRemoved: false,
		},
		{
			name:        "a file without a token is left alone",
			fileBody:    "api_url = \"http://localhost:3000/public/v1\"\n",
			wantRemoved: false,
			wantFile:    true,
			wantAPIURL:  "http://localhost:3000/public/v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if tc.fileBody != "" {
				path = writeConfig(t, dir, tc.fileBody)
			}

			removed, err := ClearToken(path)
			if err != nil {
				t.Fatalf("ClearToken: %v", err)
			}
			if removed != tc.wantRemoved {
				t.Errorf("removed = %v, want %v", removed, tc.wantRemoved)
			}

			_, statErr := os.Stat(path)
			if tc.wantFile && statErr != nil {
				t.Fatalf("the config file should have survived: %v", statErr)
			}
			if !tc.wantFile && !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("the config file should be gone, stat gave: %v", statErr)
			}
			if !tc.wantFile {
				return
			}

			cfg, loadErr := Load(Options{Path: path, Getenv: envFunc(nil)})
			if loadErr != nil {
				t.Fatalf("Load: %v", loadErr)
			}
			if cfg.Token != "" {
				t.Errorf("token = %q, want it cleared", cfg.Token)
			}
			if cfg.APIURL != tc.wantAPIURL {
				t.Errorf("api url = %q, want %q", cfg.APIURL, tc.wantAPIURL)
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "empty", token: "", want: ""},
		{name: "short tokens are fully hidden", token: "abc", want: "***"},
		{name: "eleven characters are fully hidden", token: "abcdefghijk", want: "***********"},
		{name: "twelve characters keep four either side", token: "abcdefghijkl", want: "abcd******ijkl"},
		{name: "a realistic token", token: "iv_live_9f3a2b7c4d1e", want: "iv_l******4d1e"},
		{name: "surrounding blanks are trimmed", token: "  iv_live_9f3a2b7c4d1e  ", want: "iv_l******4d1e"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Mask(tc.token)
			if got != tc.want {
				t.Errorf("Mask(%q) = %q, want %q", tc.token, got, tc.want)
			}
			trimmed := strings.TrimSpace(tc.token)
			if len(trimmed) >= 12 && strings.Contains(got, trimmed) {
				t.Errorf("Mask leaked the whole token: %q", got)
			}
		})
	}
}

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "under HOME",
			env:  map[string]string{"HOME": "/home/tester"},
			want: filepath.Join("/home/tester", ".config", "investviews", "config.toml"),
		},
		{
			name: "XDG_CONFIG_HOME wins",
			env:  map[string]string{"HOME": "/home/tester", "XDG_CONFIG_HOME": "/elsewhere/cfg"},
			want: filepath.Join("/elsewhere/cfg", "investviews", "config.toml"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefaultPath(envFunc(tc.env))
			if err != nil {
				t.Fatalf("DefaultPath: %v", err)
			}
			if got != tc.want {
				t.Errorf("DefaultPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInsecureFile(t *testing.T) {
	tests := []struct {
		name string
		mode fs.FileMode
		want bool
	}{
		{name: "0600 is fine", mode: 0o600, want: false},
		{name: "0400 is fine", mode: 0o400, want: false},
		{name: "0640 is not", mode: 0o640, want: true},
		{name: "0604 is not", mode: 0o604, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfig(t, dir, "token = \"tok_for_mode_test\"\n")
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}

			cfg, err := Load(Options{Path: path, Getenv: envFunc(nil)})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.InsecureFile(); got != tc.want {
				t.Errorf("InsecureFile() = %v for mode %#o, want %v", got, tc.mode, tc.want)
			}
		})
	}
}
