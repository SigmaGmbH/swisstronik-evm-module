package root_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/SigmaGmbH/evm-module/app"
	daemon "github.com/SigmaGmbH/evm-module/cmd/daemon"
)

func TestInitCmd(t *testing.T) {
	rootCmd, _ := daemon.NewRootCmd()
	rootCmd.SetArgs([]string{
		"init",          // Test the init cmd
		"daemon",        // Moniker
		fmt.Sprintf("--%s=%s", cli.FlagOverwrite, "true"), // Overwrite genesis.json, in case it already exists
		fmt.Sprintf("--%s=%s", flags.FlagChainID, "daemon_1000-1"),
	})

	err := svrcmd.Execute(rootCmd, "", app.DefaultNodeHome)
	require.NoError(t, err)
}
