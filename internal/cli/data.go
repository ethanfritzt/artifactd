package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

const maxRuntimeData = 5 << 20

func dataCommand(app *Application) *cobra.Command {
	command := &cobra.Command{
		Use:   "data",
		Short: "Manage live artifact data",
	}
	command.AddCommand(dataPushCommand(app))
	return command
}

func dataPushCommand(app *Application) *cobra.Command {
	var filePath string
	command := &cobra.Command{
		Use:   "push <artifact-id> <source>",
		Short: "Push JSON data to an artifact runtime source",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readData(filePath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			client, err := app.client()
			if err != nil {
				return err
			}
			response, err := client.PushData(cmd.Context(), args[0], args[1], data)
			if err != nil {
				return err
			}
			return writeOutput(cmd, app.output, response, func(out io.Writer) error {
				if _, err := fmt.Fprintf(out, "%s/%s updated at %s\n", response.ArtifactID, response.Source, response.UpdatedAt.Format("2006-01-02 15:04:05")); err != nil {
					return fmt.Errorf("writing data result: %w", err)
				}
				return nil
			})
		},
	}
	command.Flags().StringVar(&filePath, "file", "-", "JSON file to push, or - for stdin")
	return command
}

func readData(path string, input io.Reader) ([]byte, error) {
	reader := input
	var file *os.File
	if path != "" && path != "-" {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening data file: %w", err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxRuntimeData+1))
	if err != nil {
		return nil, fmt.Errorf("reading runtime data: %w", err)
	}
	if len(data) > maxRuntimeData {
		return nil, fmt.Errorf("runtime data is too large")
	}
	return data, nil
}
