// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package cli

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/build"
	"github.com/cockroachdb/cockroach/pkg/settings"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sqlmigrations"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var manPath string

var genManCmd = &cobra.Command{
	Use:   "man",
	Short: "generate man pages for CockroachDB",
	Long: `This command generates man pages for CockroachDB.

By default, this places man pages into the "man/man1" directory under the
current directory. Use "--path=PATH" to override the output directory. For
example, to install man pages globally on many Unix-like systems,
use "--path=/usr/local/share/man/man1".
`,
	Args: cobra.NoArgs,
	RunE: MaybeDecorateGRPCError(runGenManCmd),
}

func runGenManCmd(cmd *cobra.Command, args []string) error {
	info := build.GetInfo()
	header := &doc.GenManHeader{
		Section: "1",
		Manual:  "CockroachDB Manual",
		Source:  fmt.Sprintf("CockroachDB %s", info.Tag),
	}

	if !strings.HasSuffix(manPath, string(os.PathSeparator)) {
		manPath += string(os.PathSeparator)
	}

	if _, err := os.Stat(manPath); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(manPath, 0755); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if err := doc.GenManTree(cmd.Root(), header, manPath); err != nil {
		return err
	}

	// TODO(cdo): The man page generated by the cobra package doesn't include a list of commands, so
	// one has to notice the "See Also" section at the bottom of the page to know which commands
	// are supported. I'd like to make this better somehow.

	fmt.Println("Generated CockroachDB man pages in", manPath)
	return nil
}

var autoCompletePath string

var genAutocompleteCmd = &cobra.Command{
	Use:   "autocomplete [shell]",
	Short: "generate autocompletion script for CockroachDB",
	Long: `Generate autocompletion script for CockroachDB.

If no arguments are passed, or if 'bash' is passed, a bash completion file is
written to ./cockroach.bash. If 'zsh' is passed, a zsh completion file is written
to ./_cockroach. Use "--out=/path/to/file" to override the output file location.

Note that for the generated file to work on OS X with bash, you'll need to install
Homebrew's bash-completion package (or an equivalent) and follow the post-install
instructions.
`,
	Args:      cobra.OnlyValidArgs,
	ValidArgs: []string{"bash", "zsh"},
	RunE:      MaybeDecorateGRPCError(runGenAutocompleteCmd),
}

func runGenAutocompleteCmd(cmd *cobra.Command, args []string) error {
	var shell string
	if len(args) > 0 {
		shell = args[0]
	} else {
		shell = "bash"
	}

	var err error
	switch shell {
	case "bash":
		if autoCompletePath == "" {
			autoCompletePath = "cockroach.bash"
		}
		err = cmd.Root().GenBashCompletionFile(autoCompletePath)
	case "zsh":
		if autoCompletePath == "" {
			autoCompletePath = "_cockroach"
		}
		err = cmd.Root().GenZshCompletionFile(autoCompletePath)
	}
	if err != nil {
		return nil
	}

	fmt.Printf("Generated %s completion file: %s\n", shell, autoCompletePath)
	return nil
}

var aesSize int

var genEncryptionKeyCmd = &cobra.Command{
	Use:   "encryption-key <key-file>",
	Short: "generate store key for encryption at rest",
	Long: `Generate store key for encryption at rest.

If no AES key size is specified through "-s=256", the key size used for AES
algorithm will be 128 by default. AES key size should only be 128, 192, or 256.

Users are required to provide a filename for the key to be stored.
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		encryptionKeyPath := args[0]

		// Check encryptionKeySize is suitable for the encryption algorithm.
		if aesSize != 128 && aesSize != 192 && aesSize != 256 {
			return fmt.Errorf("store key size should be 128, 192, or 256 bits, got %d", aesSize)
		}

		// 32 bytes are reserved for key ID.
		keySize := aesSize/8 + 32
		b := make([]byte, keySize)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("failed to create key with size %d bytes", keySize)
		}

		// Write key to the file with owner read/write permission.
		if err := ioutil.WriteFile(encryptionKeyPath, b, 0600); err != nil {
			return err
		}

		fmt.Printf("successfully created AES-%d key: %s\n", aesSize, encryptionKeyPath)
		return nil
	},
}

var genSettingsListCmd = &cobra.Command{
	Use:   "settings-list <output-dir>",
	Short: "output a list of available cluster settings",
	Long: `
Output the list of cluster settings known to this binary.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wrapCode := func(s string) string {
			if cliCtx.tableDisplayFormat == tableDisplayHTML {
				return fmt.Sprintf("<code>%s</code>", s)
			}
			return s
		}

		// Fill a Values struct with the defaults.
		s := cluster.MakeTestingClusterSettings()
		settings.NewUpdater(&s.SV).ResetRemaining()

		var rows [][]string
		for _, name := range settings.Keys() {
			setting, ok := settings.Lookup(name)
			if !ok {
				panic(fmt.Sprintf("could not find setting %q", name))
			}
			typ, ok := settings.ReadableTypes[setting.Typ()]
			if !ok {
				panic(fmt.Sprintf("unknown setting type %q", setting.Typ()))
			}
			defaultVal := setting.String(&s.SV)
			if override, ok := sqlmigrations.SettingsDefaultOverrides[name]; ok {
				defaultVal = override
			}
			row := []string{wrapCode(name), typ, wrapCode(defaultVal), setting.Description()}
			rows = append(rows, row)
		}

		reporter, err := makeReporter()
		if err != nil {
			return err
		}
		if hr, ok := reporter.(*htmlReporter); ok {
			hr.escape = false
			hr.rowStats = false
		}
		cols := []string{"Setting", "Type", "Default", "Description"}
		return render(reporter, os.Stdout, cols, newRowSliceIter(rows, "dddd"), nil /* noRowsHook*/)
	},
}

var genCmd = &cobra.Command{
	Use:   "gen [command]",
	Short: "generate auxiliary files",
	Long:  "Generate manpages, example shell settings, example databases, etc.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Usage()
	},
}

var genCmds = []*cobra.Command{
	genManCmd,
	genAutocompleteCmd,
	genExamplesCmd,
	genHAProxyCmd,
	genSettingsListCmd,
	genEncryptionKeyCmd,
}

func init() {
	genManCmd.PersistentFlags().StringVar(&manPath, "path", "man/man1",
		"path where man pages will be outputted")
	genAutocompleteCmd.PersistentFlags().StringVar(&autoCompletePath, "out", "",
		"path to generated autocomplete file")
	genHAProxyCmd.PersistentFlags().StringVar(&haProxyPath, "out", "haproxy.cfg",
		"path to generated haproxy configuration file")
	genEncryptionKeyCmd.PersistentFlags().IntVarP(&aesSize, "size", "s", 128,
		"AES key size for encryption at rest")

	genCmd.AddCommand(genCmds...)
}
