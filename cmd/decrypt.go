package cmd

import (
	"fmt"
	"os"

	"dbtool/internal/archivecrypt"
	"dbtool/internal/interactivelist"
	"dbtool/internal/secureinput"

	"github.com/spf13/cobra"
)

var decryptPassword string

var decryptCmd = &cobra.Command{
	Use:   "decrypt <input> <output>",
	Short: "Decrypt an archive encrypted by dbtool's S3/Telegram encryption password",
	Long: `Decrypt a file dbtool encrypted before sending it to S3 or Telegram (see
'setting s3 config' and 'setting telegram set' --encryption-password).

S3 restores are decrypted automatically — this command is mainly for a
dump delivered via Telegram, which has no automated ingest path back into
dbtool: after reassembling the parts by hand (cat *.part* > name.tar.enc,
per the delivery message), decrypt the result before extracting it:

  dbtool decrypt name.tar.enc name.tar
  tar -xf name.tar

The password is resolved the same way other dbtool secrets are: --password,
then DBTOOL_DECRYPT_PASSWORD, then an interactive masked prompt.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath, outputPath := args[0], args[1]

		password, err := secureinput.ResolveSecret(decryptPassword, "DBTOOL_DECRYPT_PASSWORD", interactivelist.PromptLabel("Encryption password", ""))
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}

		in, err := os.Open(inputPath) // #nosec G304 -- inputPath is an explicit CLI argument the operator chose
		if err != nil {
			return fmt.Errorf("open %s: %w", inputPath, err)
		}
		defer in.Close()

		out, err := os.Create(outputPath) // #nosec G304 -- outputPath is an explicit CLI argument the operator chose
		if err != nil {
			return fmt.Errorf("create %s: %w", outputPath, err)
		}

		if err := archivecrypt.DecryptStream(out, in, password); err != nil {
			out.Close()
			os.Remove(outputPath) // don't leave a partial/garbage file behind on failure
			return err
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %s: %w", outputPath, err)
		}

		fmt.Println("Decrypted:", outputPath)
		return nil
	},
}

func init() {
	decryptCmd.Flags().StringVar(&decryptPassword, "password", "", "Encryption password (non-interactive; falls back to DBTOOL_DECRYPT_PASSWORD env var, then a masked prompt)")
	if installedInPath {
		rootCmd.AddCommand(decryptCmd)
	}
}
