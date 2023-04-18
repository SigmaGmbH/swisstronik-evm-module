package cli

import (
	"fmt"
	"strconv"

	"github.com/SigmaGmbH/evm-module/x/attestation/types"
	"github.com/SigmaGmbH/librustgo"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the parent command for all x/attestation CLi query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the attestation module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(GetSeedCmd())

	return cmd
}

// GetSeedCmd requests seed from the seed node
func GetSeedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed SEED_SERVER_ADDRESS PORT",
		Short: "Requests seed server to share seed",
		Long:  "Requests seed server to share seed. During the request, this node will pass Remote Attestation, and if it will be successful, seed server sends encrypted seed.", //nolint:lll
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seedAddress := args[0]
			port, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}

			if err := librustgo.RequestSeed(seedAddress, port); err != nil {
				return err
			}

			fmt.Println("Remote Attestation passed. Node is ready for work")
			return nil
		},
	}

	return cmd
}

