package cli

import "testing"

func TestRunCommandRequiresConfigFlag(t *testing.T) {
	command := newRunCommand()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs(nil)
	if err := command.Execute(); err == nil {
		t.Fatal("expected an error when --config is not set")
	}
}

func TestRunCommandRejectsMissingConfigFile(t *testing.T) {
	command := newRunCommand()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs([]string{"--config", "/nonexistent/path/does-not-exist.yaml"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}
