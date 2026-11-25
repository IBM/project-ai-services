package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckROCmRHELVersion(t *testing.T) {
	tests := []struct {
		name    string
		osInfo  string
		wantErr string
	}{
		{"RHEL 10.3", "NAME=\"Red Hat Enterprise Linux\"\nID=\"rhel\"\nVERSION_ID=\"10.3\"\n", ""},
		{"RHEL 10.4", "NAME=\"Red Hat Enterprise Linux\"\nID=\"rhel\"\nVERSION_ID=\"10.4\"\n", ""},
		{"RHEL 11.0", "NAME=\"Red Hat Enterprise Linux\"\nID=\"rhel\"\nVERSION_ID=\"11.0\"\n", ""},
		{"RHEL 10.2 is too old", "NAME=\"Red Hat Enterprise Linux\"\nID=\"rhel\"\nVERSION_ID=\"10.2\"\n", "Minimum required version for ROCm is 10.3"},
		{"RHEL 10 with no minor is too old", "ID=rhel\nVERSION_ID=\"10\"\n", "Minimum required version for ROCm is 10.3"},
		{"RHEL 9.6 is too old", "ID=\"rhel\"\nVERSION_ID=\"9.6\"\n", "Minimum required version for ROCm is 10.3"},
		{"not RHEL", "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n", "only RHEL is supported"},
		{"missing VERSION_ID", "ID=rhel\n", "unable to determine OS version"},
		{"unparsable version", "ID=rhel\nVERSION_ID=\"abc\"\n", "unable to parse OS version"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkROCmRHELVersion(tc.osInfo)
			if tc.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
