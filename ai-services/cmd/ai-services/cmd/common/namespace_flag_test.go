package common

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/catalogns"
)

func newNamespaceCmd(t *testing.T) *cobra.Command {
	t.Helper()
	t.Cleanup(func() { _ = catalogns.Set(catalogns.Default) })

	cmd := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	ConfigureNamespaceFlag(cmd)

	return cmd
}

func TestConfigureNamespaceFlag_Default(t *testing.T) {
	cmd := newNamespaceCmd(t)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := catalogns.Get(); got != catalogns.Default {
		t.Errorf("namespace = %q, want %q", got, catalogns.Default)
	}
	if got := cmd.PersistentFlags().Lookup("namespace").DefValue; got != catalogns.Default {
		t.Errorf("DefValue = %q, want %q", got, catalogns.Default)
	}
}

func TestConfigureNamespaceFlag_Override(t *testing.T) {
	cmd := newNamespaceCmd(t)
	cmd.SetArgs([]string{"--namespace", "ai-services-dev"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := catalogns.Get(); got != "ai-services-dev" {
		t.Errorf("namespace = %q, want ai-services-dev", got)
	}
}

func TestConfigureNamespaceFlag_Hidden(t *testing.T) {
	cmd := newNamespaceCmd(t)
	f := cmd.PersistentFlags().Lookup("namespace")
	if f == nil {
		t.Fatal("namespace flag not registered")
	}
	if !f.Hidden {
		t.Error("namespace flag must be hidden")
	}
}

func TestConfigureNamespaceFlag_Validation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{"valid", "ai-services-dev", true},
		{"too long", strings.Repeat("a", catalogns.MaxLen+1), false},
		{"uppercase", "AI-Services", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newNamespaceCmd(t)
			cmd.SetArgs([]string{"--namespace=" + tt.value})
			err := cmd.Execute()
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if got := catalogns.Get(); got != catalogns.Default {
					t.Errorf("namespace changed to %q on invalid input", got)
				}
			}
		})
	}
}

func TestValidateResetFlag_AllowsNamespace(t *testing.T) {
	t.Cleanup(func() { _ = catalogns.Set(catalogns.Default) })

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("reset-password", false, "")
	cmd.Flags().String("other", "", "")
	ConfigureNamespaceFlag(cmd)
	cmd.Flags().AddFlagSet(cmd.PersistentFlags())

	if err := cmd.Flags().Parse([]string{"--reset-password", "--namespace=ai-services-dev"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidateResetFlag(cmd, "reset-password"); err != nil {
		t.Errorf("--namespace must be allowed with a reset flag, got: %v", err)
	}

	cmd2 := &cobra.Command{Use: "test"}
	cmd2.Flags().Bool("reset-password", false, "")
	cmd2.Flags().String("other", "", "")
	if err := cmd2.Flags().Parse([]string{"--reset-password", "--other=x"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidateResetFlag(cmd2, "reset-password"); err == nil {
		t.Error("other flags must still be rejected with a reset flag")
	}
}
