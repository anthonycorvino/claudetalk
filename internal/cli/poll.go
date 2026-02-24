package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const seqFileName = ".claudetalk-seq"

type seqState struct {
	Seq int64 `json:"seq"`
}

func newPollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Check for new messages (designed to run at the start of every Claude turn)",
		Long: `Checks for new messages since the last poll. Prints them if found,
stays silent if there's nothing new. On first run, fetches the latest 5
messages to give context.

This command is meant to be called automatically by Claude Code at the
start of every response, as instructed in CLAUDE.md.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}
			return runPoll()
		},
	}
	return cmd
}

func runPoll() error {
	seqPath := findSeqFile()

	if seqPath == "" {
		// First run: bootstrap with latest 5 messages.
		return pollBootstrap()
	}

	// Read the saved sequence number.
	state, err := readSeqFile(seqPath)
	if err != nil {
		// Corrupted file — re-bootstrap.
		return pollBootstrap()
	}

	// Fetch messages after the saved sequence number.
	list, err := getMessages(flagServer, flagRoom, state.Seq, 100)
	if err != nil {
		return err
	}

	if list.Count == 0 {
		// Nothing new — stay silent.
		return nil
	}

	// Print new messages.
	for _, env := range list.Messages {
		fmt.Println(formatPlain(env))
	}

	// Update seq file with the highest sequence number seen.
	maxSeq := state.Seq
	for _, env := range list.Messages {
		if env.SeqNum > maxSeq {
			maxSeq = env.SeqNum
		}
	}

	return writeSeqFile(seqPath, seqState{Seq: maxSeq})
}

// pollBootstrap runs on first poll (no seq file exists).
// Fetches latest 5 messages for context, then creates the seq file.
func pollBootstrap() error {
	list, err := getLatestMessages(flagServer, flagRoom, 5)
	if err != nil {
		return err
	}

	if list.Count > 0 {
		for _, env := range list.Messages {
			fmt.Println(formatPlain(env))
		}
	}

	// Determine the max sequence number.
	var maxSeq int64
	for _, env := range list.Messages {
		if env.SeqNum > maxSeq {
			maxSeq = env.SeqNum
		}
	}

	// Write the seq file in the current directory.
	seqPath := seqFileName
	return writeSeqFile(seqPath, seqState{Seq: maxSeq})
}

// findSeqFile walks up from the current directory looking for .claudetalk-seq,
// using the same logic as loadConfig for .claudetalk.
func findSeqFile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		path := filepath.Join(dir, seqFileName)
		if _, err := os.Stat(path); err == nil {
			return path
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func readSeqFile(path string) (*seqState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state seqState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeSeqFile(path string, state seqState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
