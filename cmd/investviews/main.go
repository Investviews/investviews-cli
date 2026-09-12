// Command investviews is the command-line client for the InvestViews public
// API (https://docs.investviews.ai).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/investviews/investviews-cli/internal/config"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "investviews: "+err.Error())
		os.Exit(1)
	}
}

// errNoToken is returned by "auth status" when nothing is configured, so a
// script can test for it with the exit code.
var errNoToken = errors.New("no token configured")

// newRootCmd builds the command tree. Subcommands that talk to the API (geo,
// stats, usage, coverage) hang off this root and read their token and base URL
// through loadConfig.
func newRootCmd() *cobra.Command {
	var tokenFlag string

	cmd := &cobra.Command{
		Use:   "investviews",
		Short: "Query the InvestViews public API from the shell",
		Long: `investviews queries the InvestViews public API from the shell.

Token, highest precedence first:
  1. the --token flag
  2. the INVESTVIEWS_TOKEN environment variable
  3. ~/.config/investviews/config.toml, written by "investviews auth login"

INVESTVIEWS_API_URL overrides the base URL the CLI talks to. It defaults to
` + config.DefaultAPIURL + ` and accepts any base URL, for example
http://localhost:3000/public/v1 when working against a local server.

Documentation: https://docs.investviews.ai`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&tokenFlag, "token", "",
		"API token; outranks "+config.EnvToken+" and the config file")
	cmd.AddCommand(newAuthCmd(&tokenFlag))

	return cmd
}

// loadConfig resolves the configuration for one command run.
func loadConfig(tokenFlag string) (*config.Config, error) {
	return config.Load(config.Options{TokenFlag: tokenFlag})
}

func newAuthCmd(tokenFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage the stored API token",
	}
	cmd.AddCommand(
		newAuthLoginCmd(tokenFlag),
		newAuthStatusCmd(tokenFlag),
		newAuthLogoutCmd(),
	)
	return cmd
}

func newAuthLoginCmd(tokenFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store an API token in the config file",
		Long: `Store an API token at ~/.config/investviews/config.toml, mode 0600.

The token is taken from --token, else from ` + config.EnvToken + `, else read
from standard input when it is piped in:

  investviews auth login --token iv_xxx
  echo "$` + config.EnvToken + `" | investviews auth login

Nothing is written if the config file already on disk is readable by the group
or by everyone; fix it with chmod 600 and run the command again.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, source, err := tokenToStore(cmd, *tokenFlag)
			if err != nil {
				return err
			}
			path, err := config.DefaultPath(nil)
			if err != nil {
				return err
			}
			if err := config.SaveToken(path, token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stored token %s from %s in %s (mode 0600).\n",
				config.Mask(token), source, path)
			return nil
		},
	}
}

func newAuthStatusCmd(tokenFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether a token is configured and where it came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*tokenFlag)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "Config file: %s", cfg.Path)
			if !cfg.FileExists {
				fmt.Fprint(out, " (not present)")
			}
			fmt.Fprintln(out)
			if cfg.InsecureFile() {
				fmt.Fprintf(out, "  warning: mode is %#o and the token is readable beyond you; run: chmod 600 %s\n",
					cfg.FileMode, cfg.Path)
			}
			fmt.Fprintf(out, "Base URL:    %s (%s)\n", cfg.APIURL, cfg.APIURLSource)

			if cfg.Token == "" {
				fmt.Fprintln(out, "Token:       none configured")
				fmt.Fprintf(out, "Run \"investviews auth login --token …\" or set %s.\n", config.EnvToken)
				return errNoToken
			}
			// Masked, always. The full token is never printed.
			fmt.Fprintf(out, "Token:       %s (from %s)\n", config.Mask(cfg.Token), cfg.TokenSource)
			return nil
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored API token",
		Long: `Remove the token from ~/.config/investviews/config.toml.

A token set through --token or ` + config.EnvToken + ` is not stored on disk, so
this command cannot remove it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.DefaultPath(nil)
			if err != nil {
				return err
			}
			removed, err := config.ClearToken(path)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed the stored token from %s.\n", path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "No token was stored in %s.\n", path)
			}
			return nil
		},
	}
}

// tokenToStore picks the token "auth login" writes: the flag, else the
// environment, else piped standard input.
func tokenToStore(cmd *cobra.Command, tokenFlag string) (token string, source config.Source, err error) {
	if v := strings.TrimSpace(tokenFlag); v != "" {
		return v, config.SourceFlag, nil
	}
	if v := strings.TrimSpace(os.Getenv(config.EnvToken)); v != "" {
		return v, config.SourceEnv, nil
	}
	if v, ok := tokenFromStdin(cmd.InOrStdin()); ok {
		return v, "standard input", nil
	}
	return "", config.SourceNone, fmt.Errorf(
		"no token given; pass --token, set %s, or pipe the token on standard input", config.EnvToken)
}

// tokenFromStdin reads a piped token. An interactive terminal is left alone so
// the command never appears to hang waiting for input.
func tokenFromStdin(in io.Reader) (string, bool) {
	if f, ok := in.(*os.File); ok {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return "", false
		}
	}
	b, err := io.ReadAll(in)
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(b))
	return token, token != ""
}
