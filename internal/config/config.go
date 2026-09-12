// Package config resolves the token and base URL the investviews CLI runs with.
//
// A token is taken from, highest precedence first:
//
//  1. the --token flag
//  2. the INVESTVIEWS_TOKEN environment variable
//  3. the config file, ~/.config/investviews/config.toml
//
// The base URL comes from INVESTVIEWS_API_URL, else the config file, else
// DefaultAPIURL.
//
// The config file holds a credential, so it is written at mode 0600 and Save
// refuses to write when the file on disk is readable by the group or by
// everyone.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultAPIURL is the production base URL of the InvestViews public API.
	//
	// EnvAPIURL overrides it and accepts any base URL — for example
	// http://localhost:3000/public/v1 when working against a local server.
	// It is a plain base-URL override and nothing more: there is no staging
	// environment, so never describe it as one.
	DefaultAPIURL = "https://api.investviews.ai/public/v1"

	// EnvToken holds a bearer token. It outranks the config file.
	EnvToken = "INVESTVIEWS_TOKEN"
	// EnvAPIURL overrides the base URL. See DefaultAPIURL.
	EnvAPIURL = "INVESTVIEWS_API_URL"

	// FileMode is the only mode the config file is ever written with.
	FileMode fs.FileMode = 0o600
	// DirMode is the mode the config directory is created with.
	DirMode fs.FileMode = 0o700

	// sharedBits are the permission bits that would expose the token to the
	// group or to everyone.
	sharedBits fs.FileMode = 0o077
)

// ErrInsecurePermissions is returned when the config file is, or would end up,
// readable beyond its owner.
var ErrInsecurePermissions = errors.New("config file permissions are too open")

// Source names where a resolved value came from, so a command can tell the
// user which layer won.
type Source string

const (
	SourceFlag    Source = "the --token flag"
	SourceEnv     Source = "the environment"
	SourceFile    Source = "the config file"
	SourceDefault Source = "the built-in default"
	SourceNone    Source = "nowhere"
)

// File is the on-disk shape of the config file.
type File struct {
	Token  string `toml:"token,omitempty"`
	APIURL string `toml:"api_url,omitempty"`
}

// Config is the resolved configuration for one command run.
type Config struct {
	Token        string
	TokenSource  Source
	APIURL       string
	APIURLSource Source

	// Path is the config file that was consulted, whether or not it exists.
	Path string
	// FileExists reports whether Path was present.
	FileExists bool
	// FileMode is Path's permission bits, zero when the file is absent.
	// A command should warn when it has any of the group or other bits set.
	FileMode fs.FileMode
}

// InsecureFile reports whether the config file exists and is readable beyond
// its owner.
func (c *Config) InsecureFile() bool {
	return c.FileExists && c.FileMode&sharedBits != 0
}

// Options steers Load. The zero value reads the real environment and the
// default config path.
type Options struct {
	// TokenFlag is the value of --token, empty when the flag was not given.
	TokenFlag string
	// Path overrides the config file location. Tests set it; the CLI does not.
	Path string
	// Getenv overrides environment lookups. Tests set it; the CLI does not.
	Getenv func(string) string
}

func (o Options) getenv(key string) string {
	if o.Getenv != nil {
		return o.Getenv(key)
	}
	return os.Getenv(key)
}

func (o Options) path() (string, error) {
	if o.Path != "" {
		return o.Path, nil
	}
	return DefaultPath(o.Getenv)
}

// DefaultPath returns ~/.config/investviews/config.toml, honouring
// XDG_CONFIG_HOME. getenv may be nil, in which case the real environment is
// read.
//
// This deliberately does not use os.UserConfigDir: on macOS that answers
// ~/Library/Application Support, and the documented path for this CLI is
// ~/.config on every platform.
func DefaultPath(getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "investviews", "config.toml"), nil
	}
	home := getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("cannot locate the home directory: %w", err)
		}
	}
	return filepath.Join(home, ".config", "investviews", "config.toml"), nil
}

// Load resolves the configuration. A missing config file is not an error; a
// malformed one is.
func Load(opts Options) (*Config, error) {
	path, err := opts.path()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Path:         path,
		TokenSource:  SourceNone,
		APIURL:       DefaultAPIURL,
		APIURLSource: SourceDefault,
	}

	file, info, err := readFile(path)
	if err != nil {
		return nil, err
	}
	if info != nil {
		cfg.FileExists = true
		cfg.FileMode = info.Mode().Perm()
		if v := strings.TrimSpace(file.Token); v != "" {
			cfg.Token, cfg.TokenSource = v, SourceFile
		}
		if v := strings.TrimSpace(file.APIURL); v != "" {
			cfg.APIURL, cfg.APIURLSource = v, SourceFile
		}
	}

	// Each layer below overwrites the one above it, so the last write wins.
	if v := strings.TrimSpace(opts.getenv(EnvAPIURL)); v != "" {
		cfg.APIURL, cfg.APIURLSource = v, SourceEnv
	}
	if v := strings.TrimSpace(opts.getenv(EnvToken)); v != "" {
		cfg.Token, cfg.TokenSource = v, SourceEnv
	}
	if v := strings.TrimSpace(opts.TokenFlag); v != "" {
		cfg.Token, cfg.TokenSource = v, SourceFlag
	}
	return cfg, nil
}

// SaveToken stores token in the config file at path, keeping any api_url the
// file already carries. The file ends up at mode 0600 or is not written.
func SaveToken(path, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("refusing to store an empty token")
	}
	file, _, err := readFile(path)
	if err != nil {
		return err
	}
	file.Token = token
	return writeFile(path, file)
}

// ClearToken removes the stored token. It reports whether there was one to
// remove; removing nothing is not an error. The file is deleted outright when
// the token was all it held.
func ClearToken(path string) (bool, error) {
	file, info, err := readFile(path)
	if err != nil {
		return false, err
	}
	if info == nil || strings.TrimSpace(file.Token) == "" {
		return false, nil
	}
	file.Token = ""
	if strings.TrimSpace(file.APIURL) == "" {
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("cannot remove %s: %w", path, err)
		}
		return true, nil
	}
	if err := writeFile(path, file); err != nil {
		return false, err
	}
	return true, nil
}

// Mask renders a token for display. A full token is never printed.
func Mask(token string) string {
	token = strings.TrimSpace(token)
	runes := []rune(token)
	if len(runes) < 12 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + strings.Repeat("*", 6) + string(runes[len(runes)-4:])
}

func readFile(path string) (File, fs.FileInfo, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil, nil
	}
	if err != nil {
		return File{}, nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var file File
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return File{}, nil, fmt.Errorf("%s is not a valid config file: %w", path, err)
	}
	return file, info, nil
}

const fileHeader = "# investviews CLI configuration. Holds an API token — keep it at mode 0600.\n"

func writeFile(path string, file File) error {
	if err := os.MkdirAll(filepath.Dir(path), DirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := checkExistingMode(path); err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	if err := toml.NewEncoder(&buf).Encode(file); err != nil {
		return fmt.Errorf("cannot encode the config file: %w", err)
	}

	// O_TRUNC keeps an existing file's mode, which is what checkExistingMode
	// guards and why the mode is enforced again below.
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, FileMode)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if _, err := fh.Write(buf.Bytes()); err != nil {
		fh.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := fh.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		return fmt.Errorf("cannot set the mode of %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm&sharedBits != 0 {
		// Never leave a token behind in a file others can read.
		os.Remove(path)
		return fmt.Errorf("%w: %s ended up %#o after writing, so nothing was stored",
			ErrInsecurePermissions, path, perm)
	}
	return nil
}

func checkExistingMode(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm&sharedBits != 0 {
		return fmt.Errorf("%w: %s is %#o and would stay readable beyond its owner; run: chmod 600 %s",
			ErrInsecurePermissions, path, perm, path)
	}
	return nil
}
